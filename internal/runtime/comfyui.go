package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/modeldock/modeldock/internal/model"
)

// comfyGenerate submits a small, generated workflow. It deliberately checks
// the installed node set first so missing custom nodes are actionable instead
// of surfacing as an opaque ComfyUI prompt error.
func comfyGenerate(ctx context.Context, m model.Manifest, input Input) (string, error) {
	base := strings.TrimRight(os.Getenv("MODELDOCK_COMFYUI_URL"), "/")
	if base == "" {
		base = "http://127.0.0.1:8188"
	}
	client := &http.Client{Timeout: 20 * time.Second}
	info, err := comfyObjectInfo(ctx, client, base)
	if err != nil {
		return "", fmt.Errorf("无法连接 ComfyUI：%w", err)
	}
	ggufNode := firstNode(info, "UnetLoaderGGUF", "UNETLoaderGGUF", "UnetLoader (GGUF)")
	if ggufNode == "" {
		return "", errors.New("该 GGUF 图片模型需要 ComfyUI-GGUF 节点。请在运行时设置中准备 ComfyUI-GGUF，并把模型目录加入 ComfyUI 的模型搜索路径")
	}
	if !hasNode(info, "CLIPLoader") || !hasNode(info, "VAELoader") || !hasNode(info, "TextEncodeQwenImage21") {
		return "", errors.New("当前 ComfyUI 缺少 Qwen-Image 所需节点，请先更新 ComfyUI 或其自定义节点")
	}
	workflow, err := qwenImageWorkflow(m, input, ggufNode, info)
	if err != nil {
		return "", err
	}
	payload, _ := json.Marshal(map[string]any{"prompt": workflow, "client_id": "modeldock"})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/prompt", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("提交 ComfyUI 工作流失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("ComfyUI 拒绝工作流：%s", strings.TrimSpace(string(body)))
	}
	var accepted struct {
		PromptID string `json:"prompt_id"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&accepted); err != nil || accepted.PromptID == "" {
		return "", errors.New("ComfyUI 未返回任务编号")
	}
	return waitComfyOutput(ctx, client, base, accepted.PromptID)
}

type comfyNodeInfo map[string]json.RawMessage

func comfyObjectInfo(ctx context.Context, client *http.Client, base string) (comfyNodeInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/object_info", nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}
	var info comfyNodeInfo
	err = json.NewDecoder(resp.Body).Decode(&info)
	return info, err
}

func hasNode(info comfyNodeInfo, name string) bool { _, ok := info[name]; return ok }
func firstNode(info comfyNodeInfo, names ...string) string {
	for _, name := range names {
		if hasNode(info, name) {
			return name
		}
	}
	return ""
}

func qwenImageWorkflow(m model.Manifest, input Input, ggufNode string, info comfyNodeInfo) (map[string]any, error) {
	modelName := ""
	for _, f := range m.Files {
		if strings.HasSuffix(strings.ToLower(f.Path), ".gguf") {
			modelName = filepath.Base(f.Path)
			break
		}
	}
	if modelName == "" {
		return nil, errors.New("模型清单中没有 GGUF 文件")
	}
	clipName := chooseInputName(info, "CLIPLoader", "clip_name", "qwen")
	if clipName == "" {
		return nil, errors.New("ComfyUI 尚未发现 Qwen-Image 文本编码器。请将文本编码器放入 models/clip 后点击重新扫描")
	}
	vae := chooseInputName(info, "VAELoader", "vae_name", "pixel")
	if vae == "" {
		return nil, errors.New("ComfyUI 尚未发现 Qwen-Image VAE。请将 VAE 放入 models/vae 后点击重新扫描")
	}
	if !hasNode(info, "EmptySD3LatentImage") {
		return nil, errors.New("ComfyUI 缺少基础图像尺寸节点，请先更新 ComfyUI")
	}
	w, h := 1024, 1024
	if parts := strings.Split(input.Options.Size, "x"); len(parts) == 2 {
		if n, e := strconv.Atoi(parts[0]); e == nil && n >= 256 {
			w = n
		}
		if n, e := strconv.Atoi(parts[1]); e == nil && n >= 256 {
			h = n
		}
	}
	steps, cfg := input.Options.Steps, input.Options.Guidance
	if steps <= 0 {
		steps = 20
	}
	if cfg <= 0 {
		cfg = 4
	}
	seed := int64(time.Now().UnixNano() & 0x7fffffffffffffff)
	if input.Options.Seed != nil {
		seed = *input.Options.Seed
	}
	workflow := map[string]any{
		"1": map[string]any{"class_type": ggufNode, "inputs": map[string]any{"unet_name": modelName}},
		"2": map[string]any{"class_type": "CLIPLoader", "inputs": map[string]any{"clip_name": clipName, "type": "qwen_image"}},
		"3": map[string]any{"class_type": "VAELoader", "inputs": map[string]any{"vae_name": vae}},
		"4": map[string]any{"class_type": "TextEncodeQwenImage21", "inputs": map[string]any{"clip": []any{"2", 0}, "prompt": input.Prompt, "negative_prompt": "", "resolution": w}},
		"5": map[string]any{"class_type": "EmptySD3LatentImage", "inputs": map[string]any{"width": w, "height": h, "batch_size": 1}},
		"6": map[string]any{"class_type": "KSampler", "inputs": map[string]any{"model": []any{"1", 0}, "seed": seed, "steps": steps, "cfg": cfg, "sampler_name": "euler", "scheduler": "simple", "positive": []any{"4", 0}, "negative": []any{"4", 1}, "latent_image": []any{"5", 0}, "denoise": 1}},
		"7": map[string]any{"class_type": "VAEDecode", "inputs": map[string]any{"samples": []any{"6", 0}, "vae": []any{"3", 0}}},
		"8": map[string]any{"class_type": "SaveImage", "inputs": map[string]any{"images": []any{"7", 0}, "filename_prefix": "ModelDock"}},
	}
	return workflow, nil
}

// chooseInputName reads ComfyUI's advertised combo choices. It avoids baking
// a particular encoder/VAE filename into ModelDock because model repositories
// use different revisions and quantization suffixes.
func chooseInputName(info comfyNodeInfo, node, input, marker string) string {
	var descriptor struct {
		Input struct {
			Required map[string]json.RawMessage `json:"required"`
		} `json:"input"`
	}
	raw, ok := info[node]
	if !ok || json.Unmarshal(raw, &descriptor) != nil {
		return ""
	}
	var choices []json.RawMessage
	if json.Unmarshal(descriptor.Input.Required[input], &choices) != nil || len(choices) == 0 {
		return ""
	}
	var names []string
	if json.Unmarshal(choices[0], &names) != nil {
		return ""
	}
	for _, name := range names {
		if marker == "" || strings.Contains(strings.ToLower(name), strings.ToLower(marker)) {
			return name
		}
	}
	return ""
}

func waitComfyOutput(ctx context.Context, client *http.Client, base, promptID string) (string, error) {
	ticker := time.NewTicker(800 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/history/"+url.PathEscape(promptID), nil)
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		var history map[string]struct {
			Outputs map[string]struct {
				Images []struct {
					Filename  string `json:"filename"`
					Subfolder string `json:"subfolder"`
					Type      string `json:"type"`
				} `json:"images"`
			} `json:"outputs"`
		}
		err = json.NewDecoder(resp.Body).Decode(&history)
		resp.Body.Close()
		if err != nil {
			continue
		}
		entry, ok := history[promptID]
		if !ok {
			continue
		}
		for _, output := range entry.Outputs {
			for _, image := range output.Images {
				view := base + "/view?filename=" + url.QueryEscape(image.Filename) + "&subfolder=" + url.QueryEscape(image.Subfolder) + "&type=" + url.QueryEscape(image.Type)
				return downloadComfyArtifact(ctx, client, view, image.Filename)
			}
		}
	}
}

func downloadComfyArtifact(ctx context.Context, client *http.Client, source, name string) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("读取 ComfyUI 输出失败：%s", resp.Status)
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cache, "ModelDock", "outputs")
	if err = os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	ext := filepath.Ext(name)
	if ext == "" {
		ext = ".png"
	}
	path := filepath.Join(dir, fmt.Sprintf("modeldock-%d%s", time.Now().UnixNano(), ext))
	out, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer out.Close()
	if _, err = io.Copy(out, resp.Body); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}
