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
	return outputPath, nil
}

func mlxProject(modelPath string, m model.Manifest) (root, kind string, err error) {
	name := strings.ToLower(m.Name + " " + m.ID + " " + strings.Join(m.Task, " "))
	if strings.Contains(name, "video") || strings.Contains(name, "h3-mlx") || strings.Contains(name, "视频") {
		kind = "video"
	} else if strings.Contains(name, "image") || strings.Contains(name, "图片") {
		kind = "image"
	} else {
		kind = "video"
	}
	script := "generate-" + kind + ".sh"
	// New installations keep launchers and their environments outside model
	// storage. The legacy model-tree lookup below remains for existing installs.
	for _, base := range mlxRuntimeRoots() {
		for _, dir := range []string{filepath.Join(base, "mlx", mlxRuntimeSlug(m)), filepath.Join(base, "mlx"), filepath.Join(base, mlxRuntimeSlug(m)), base} {
			if _, e := os.Stat(filepath.Join(dir, script)); e == nil {
				return dir, kind, nil
			}
		}
	}
	for dir := modelPath; ; {
		if _, e := os.Stat(filepath.Join(dir, script)); e == nil {
			return dir, kind, nil
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
