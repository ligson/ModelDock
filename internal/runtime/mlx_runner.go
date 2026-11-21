//go:build !windows

package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/modeldock/modeldock/internal/model"
)

// mlxGenerate delegates MLX-specific model math to the model project's pinned
// worker script. ModelDock owns the process and presents a single Go API.
func mlxGenerate(ctx context.Context, m model.Manifest, prompt string) (string, error) {
	return mlxGenerateInput(ctx, m, Input{Prompt: prompt})
}

func mlxGenerateInput(ctx context.Context, m model.Manifest, input Input) (string, error) {
	modelPath, err := mlxModelPath(m)
	if err != nil {
		return "", err
	}
	root, kind, err := mlxProject(modelPath, m)
	if err != nil {
		return "", err
	}
	if input.Options.Modality == "video" || input.Options.Modality == "image" {
		kind = input.Options.Modality
	}
	if input.Prompt == "" {
		return "", errors.New("请输入提示词")
	}
	outDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	outDir = filepath.Join(outDir, "ModelDock", "outputs")
	if err = os.MkdirAll(outDir, 0755); err != nil {
		return "", err
	}
	ext := ".png"
	script := filepath.Join(root, "generate-image.sh")
	outputPath := filepath.Join(outDir, fmt.Sprintf("modeldock-%d%s", time.Now().UnixNano(), ext))
	args := []string{input.Prompt, outputPath}
	if kind == "video" {
		ext = ".mp4"
		script = filepath.Join(root, "generate-video.sh")
		outputPath = filepath.Join(outDir, fmt.Sprintf("modeldock-%d%s", time.Now().UnixNano(), ext))
		args = []string{input.Prompt, outputPath}
	}
	if input.ImagePath != "" {
		if _, err = os.Stat(input.ImagePath); err != nil {
			return "", fmt.Errorf("输入图片不可用：%w", err)
		}
		if kind == "image" {
			args = []string{"--edit", input.ImagePath, input.Prompt, args[1]}
		} else {
			args = append(args, "--image", input.ImagePath, "--anchor", "first")
		}
	}
	if _, err = os.Stat(script); err != nil {
		return "", fmt.Errorf("没有找到 MLX %s 运行脚本", kind)
	}
	cmd := exec.CommandContext(ctx, "bash", append([]string{script}, args...)...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "H3_IMAGE_MODEL_DIR="+modelPath, "H3_TRANSFORMER_DIR="+modelPath)
	// launchctl does not inherit the interactive shell PATH. Include common
	// package-manager locations so the worker can find ffmpeg and other tools.
	pathValue := os.Getenv("PATH")
	for _, dir := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin"} {
		if !strings.Contains(":"+pathValue+":", ":"+dir+":") {
			pathValue += ":" + dir
		}
	}
	cmd.Env = append(cmd.Env, "PATH="+pathValue)
	if input.Options.Size != "" {
		cmd.Env = append(cmd.Env, "H3_IMAGE_SIZE="+input.Options.Size)
	}
	if input.Options.Steps > 0 {
		cmd.Env = append(cmd.Env, fmt.Sprintf("H3_IMAGE_STEPS=%d", input.Options.Steps))
	}
	if input.Options.Guidance > 0 {
		cmd.Env = append(cmd.Env, fmt.Sprintf("H3_IMAGE_GUIDANCE=%g", input.Options.Guidance))
	}
	if input.Options.Seed != nil {
		cmd.Env = append(cmd.Env, fmt.Sprintf("H3_IMAGE_SEED=%d", *input.Options.Seed))
	}
	if input.Options.PromptExpansion {
		cmd.Env = append(cmd.Env, "H3_IMAGE_UPSAMPLE_PROMPT=1")
	}
	started := time.Now()
	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			detail = err.Error()
		}
		return "", fmt.Errorf("MLX 运行失败：%s", detail)
	}
	if info, statErr := os.Stat(outputPath); statErr == nil && !info.IsDir() && info.Size() > 0 {
		return outputPath, nil
	}
	// Some pinned model scripts ignore the requested filename and write to
	// their own output directory. Recover the newest artifact from ModelDock's
	// output directory before reporting failure.
	if kind == "video" {
		if recovered := newestArtifact(outDir, ".mp4", started); recovered != "" {
			return recovered, nil
		}
	}
	detail := strings.TrimSpace(string(output))
	if detail == "" {
		detail = "脚本未生成输出文件：" + outputPath
	}
	return "", fmt.Errorf("MLX 运行完成但没有生成结果：%s", detail)
}

func newestArtifact(dir, ext string, since time.Time) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var newest string
	var newestTime time.Time
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ext) {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.Size() == 0 || info.ModTime().Before(since.Add(-2*time.Second)) {
			continue
		}
		if newest == "" || info.ModTime().After(newestTime) {
			newest = filepath.Join(dir, entry.Name())
			newestTime = info.ModTime()
		}
	}
	return newest
}

func mlxProject(modelPath string, m model.Manifest) (root, kind string, err error) {
	name := strings.ToLower(m.Name + " " + m.ID)
	tasks := strings.ToLower(strings.Join(m.Task, " "))
	if strings.Contains(tasks, "video") || strings.Contains(tasks, "视频") || strings.Contains(name, "video") || strings.Contains(name, "视频") {
		kind = "video"
	} else if strings.Contains(tasks, "image") || strings.Contains(tasks, "图片") || strings.Contains(name, "image") || strings.Contains(name, "图片") {
		kind = "image"
	} else {
		// MLX projects without an explicit task are treated as image projects.
		// This keeps a generic H3-MLX model from accidentally launching video.
		kind = "image"
	}
	script := "generate-" + kind + ".sh"
	// Some existing MiniMax H3 MLX manifests are tagged "文本/图片" even
	// though their project only ships the video launcher. Accept that launcher
	// during capability detection; the requested modality selects the script at
	// generation time.
	alternateScript := ""
	if strings.Contains(strings.ToLower(m.Name+" "+m.ID), "h3-mlx") && kind == "image" {
		alternateScript = "generate-video.sh"
	}
	// New installations keep launchers and their environments outside model
	// storage. The legacy model-tree lookup below remains for existing installs.
	for _, base := range mlxRuntimeRoots() {
		for _, dir := range []string{filepath.Join(base, "mlx", mlxRuntimeSlug(m)), filepath.Join(base, "mlx"), filepath.Join(base, mlxRuntimeSlug(m)), base} {
			if _, e := os.Stat(filepath.Join(dir, script)); e == nil {
				return dir, kind, nil
			}
			if alternateScript != "" {
				if _, e := os.Stat(filepath.Join(dir, alternateScript)); e == nil {
					return dir, "video", nil
				}
			}
		}
	}
	for dir := modelPath; ; {
		if _, e := os.Stat(filepath.Join(dir, script)); e == nil {
			return dir, kind, nil
		}
		if alternateScript != "" {
			if _, e := os.Stat(filepath.Join(dir, alternateScript)); e == nil {
				return dir, "video", nil
			}
		}
		next := filepath.Dir(dir)
		if next == dir {
			break
		}
		dir = next
	}
	return "", kind, fmt.Errorf("没有找到与模型配套的 MLX 运行环境")
}

func mlxRuntimeRoots() []string {
	roots := []string{}
	if configured := os.Getenv("MODELDOCK_RUNTIME_ROOT"); configured != "" {
		roots = append(roots, configured)
	}
	if fallback := DefaultRoot(); fallback != "" && (len(roots) == 0 || roots[0] != fallback) {
		roots = append(roots, fallback)
	}
	if dir, e := os.UserCacheDir(); e == nil {
		fallback := filepath.Join(dir, "ModelDock", "runtimes")
		if len(roots) == 0 || roots[len(roots)-1] != fallback {
			roots = append(roots, fallback)
		}
	}
	return roots
}

func mlxRuntimeSlug(m model.Manifest) string {
	value := strings.ToLower(m.Name)
	value = strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-").Replace(value)
	return value
}

func mlxModelPath(m model.Manifest) (string, error) {
	if m.LocalPath != "" {
		if st, err := os.Stat(m.LocalPath); err == nil && st.IsDir() {
			return m.LocalPath, nil
		}
	}
	for _, f := range m.Files {
		if st, err := os.Stat(f.Path); err == nil && st.IsDir() {
			return f.Path, nil
		}
	}
	return "", fmt.Errorf("模型目录不可用：%s", m.Name)
}
