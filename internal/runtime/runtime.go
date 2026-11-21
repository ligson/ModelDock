package runtime

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/modeldock/modeldock/internal/host"
	"github.com/modeldock/modeldock/internal/model"
)

type Status string

const (
	Ready       Status = "ready"
	NeedsSetup  Status = "needs_setup"
	Unsupported Status = "unsupported"
)

type Plan struct {
	Adapter string   `json:"adapter"`
	Label   string   `json:"label"`
	Status  Status   `json:"status"`
	Reason  string   `json:"reason"`
	Setup   []string `json:"setup,omitempty"`
	Inputs  []string `json:"inputs,omitempty"`
	Outputs []string `json:"outputs,omitempty"`
}

type Adapter interface {
	Name() string
	CanHandle(model.Manifest) bool
	Detect(context.Context, model.Manifest, host.Capability) Plan
	Generate(context.Context, model.Manifest, string) (string, error)
}

type Input struct {
	Prompt    string
	ImagePath string
	Options   ImageOptions
	Messages  []Message
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ImageOptions contains the small set of image controls that can be safely
// exposed to ordinary users. Zero values use the model project's defaults.
type ImageOptions struct {
	Modality        string  `json:"modality,omitempty"`
	Size            string  `json:"size,omitempty"`
	Steps           int     `json:"steps,omitempty"`
	Guidance        float64 `json:"guidance,omitempty"`
	Seed            *int64  `json:"seed,omitempty"`
	PromptExpansion bool    `json:"promptExpansion,omitempty"`
}

type InputAdapter interface {
	GenerateInput(context.Context, model.Manifest, Input) (string, error)
}

type Registry struct{ adapters []Adapter }

func (r Registry) Adapters() []Adapter { return r.adapters }
func NewRegistry() Registry {
	return Registry{adapters: []Adapter{PureGoAdapter{}, OllamaAdapter{}, LlamaCPPAdapter{}, MLXAdapter{}, ComfyUIAdapter{}}}
}
func (r Registry) Plan(ctx context.Context, m model.Manifest, h host.Capability) Plan {
	for _, a := range r.adapters {
		if a.CanHandle(m) {
			return a.Detect(ctx, m, h)
		}
	}
	if strings.EqualFold(m.Format, "safetensors") {
		return Plan{Adapter: "none", Label: "暂不支持", Status: Unsupported, Reason: "当前 SafeTensors 模型没有匹配的推理适配器。文本模型请使用 GGUF 版本配合 llama.cpp，或接入支持该模型的 Transformers 等文本运行时；安装 ComfyUI 不能解决文本模型运行问题"}
	}
	return Plan{Adapter: "none", Label: "暂不支持", Status: Unsupported, Reason: fmt.Sprintf("暂时没有适配 %s 格式的本地推理组件", m.Format)}
}
func (r Registry) Generate(ctx context.Context, m model.Manifest, prompt string) (string, error) {
	return r.GenerateInput(ctx, m, Input{Prompt: prompt})
}
func (r Registry) GenerateInput(ctx context.Context, m model.Manifest, input Input) (string, error) {
	for _, a := range r.adapters {
		if a.CanHandle(m) {
			if ia, ok := a.(InputAdapter); ok {
				return ia.GenerateInput(ctx, m, input)
			}
			return a.Generate(ctx, m, input.Prompt)
		}
	}
	return "", fmt.Errorf("no runtime adapter for %s", m.Format)
}

type PureGoAdapter struct{}

func (PureGoAdapter) Name() string { return "pure-go" }
func (PureGoAdapter) CanHandle(m model.Manifest) bool {
	name := strings.ToLower(m.Name + " " + m.ID)
	// The current golem qwen35 SSM path panics for DeepSeek-V4/Qwen3.5
	// variants. Let llama.cpp handle these GGUF models instead.
	if strings.Contains(name, "deepseek-v4") || strings.Contains(name, "qwen3.5") || strings.Contains(name, "qwen35") {
		return false
	}
	return pureGoAvailable() && strings.EqualFold(m.Format, "gguf") && !imageModel(m) && (m.Source == "local" || m.LocalPath != "")
}
func (PureGoAdapter) Detect(_ context.Context, m model.Manifest, _ host.Capability) Plan {
	return pureGoDetect(m)
}
func (PureGoAdapter) Generate(ctx context.Context, m model.Manifest, prompt string) (string, error) {
	return pureGoGenerate(ctx, m, prompt)
}

type OllamaAdapter struct{}

func (OllamaAdapter) Name() string { return "ollama" }
func (OllamaAdapter) CanHandle(m model.Manifest) bool {
	return strings.EqualFold(m.Source, "ollama") || strings.EqualFold(m.Format, "ollama")
}
func (OllamaAdapter) Detect(_ context.Context, _ model.Manifest, h host.Capability) Plan {
	for _, r := range h.Runtimes {
		if r.Name == "ollama" && r.Installed {
			return Plan{Adapter: "ollama", Label: "Ollama", Status: Ready, Reason: "已检测到 Ollama，可直接生成"}
		}
	}
	return Plan{Adapter: "ollama", Label: "Ollama", Status: NeedsSetup, Reason: "模型适合 Ollama，但当前未检测到运行服务", Setup: []string{"启动 Ollama 服务"}}
}
func (OllamaAdapter) Generate(context.Context, model.Manifest, string) (string, error) {
	return "", fmt.Errorf("ollama generation is handled by the API adapter")
}

type LlamaCPPAdapter struct{}

func (LlamaCPPAdapter) Name() string { return "llama.cpp" }
func (LlamaCPPAdapter) CanHandle(m model.Manifest) bool {
	return strings.EqualFold(m.Format, "gguf") && !imageModel(m)
}
func (LlamaCPPAdapter) Detect(_ context.Context, _ model.Manifest, h host.Capability) Plan {
	for _, r := range h.Runtimes {
		if r.Name == "llama.cpp" && r.Installed {
			return Plan{Adapter: "llama.cpp", Label: "llama.cpp", Status: Ready, Reason: "已检测到 llama.cpp，可直接生成"}
		}
	}
	return Plan{Adapter: "llama.cpp", Label: "llama.cpp", Status: NeedsSetup, Reason: "GGUF 模型需要推理适配器，ModelDock 可负责准备", Setup: []string{"自动下载匹配当前平台的推理组件"}}
}
func (LlamaCPPAdapter) Generate(context.Context, model.Manifest, string) (string, error) {
	return "", fmt.Errorf("llama.cpp generation requires the ModelDock runtime service")
}
func (LlamaCPPAdapter) GenerateInput(ctx context.Context, m model.Manifest, input Input) (string, error) {
	return llamaCPPGenerate(ctx, m, input)
}

type MLXAdapter struct{}

func (MLXAdapter) Name() string { return "mlx" }
func (MLXAdapter) CanHandle(m model.Manifest) bool {
	name := strings.ToLower(m.Name + " " + m.ID)
	return strings.EqualFold(m.Format, "mlx") || strings.Contains(name, "mlx") || strings.Contains(name, "minimax-h3-image")
}
func (MLXAdapter) Detect(_ context.Context, m model.Manifest, h host.Capability) Plan {
	for _, c := range h.Capabilities {
		if c == "mlx" {
			if _, _, err := mlxProject(m.LocalPath, m); err != nil {
				return Plan{Adapter: "mlx", Label: "ModelDock MLX 运行时", Status: NeedsSetup, Reason: err.Error(), Setup: []string{"请将模型目录与对应的 MLX 运行项目放在同一目录树中"}}
			}
			return Plan{Adapter: "mlx", Label: "ModelDock MLX 运行时", Status: Ready, Reason: "ModelDock 会自动调用模型目录中的 MLX 运行环境", Inputs: []string{"文本", "图片"}, Outputs: []string{"图片", "视频"}}
		}
	}
	return Plan{Adapter: "mlx", Label: "MLX", Status: Unsupported, Reason: "当前机器没有 Apple MLX 能力"}
}
func (MLXAdapter) Generate(ctx context.Context, m model.Manifest, prompt string) (string, error) {
	return mlxGenerate(ctx, m, prompt)
}
func (MLXAdapter) GenerateInput(ctx context.Context, m model.Manifest, input Input) (string, error) {
	return mlxGenerateInput(ctx, m, input)
}

type ComfyUIAdapter struct{}

func (ComfyUIAdapter) Name() string { return "comfyui" }
func (ComfyUIAdapter) CanHandle(m model.Manifest) bool {
	if imageModel(m) {
		return true
	}
	for _, task := range m.Task {
		if strings.Contains(strings.ToLower(task), "image") || strings.Contains(strings.ToLower(task), "video") || strings.Contains(strings.ToLower(task), "图片") || strings.Contains(strings.ToLower(task), "视频") {
			return true
		}
	}
	return strings.EqualFold(m.Format, "diffusers") || strings.EqualFold(m.Format, "comfyui")
}

func imageModel(m model.Manifest) bool {
	text := strings.ToLower(m.Name + " " + m.ID + " " + strings.Join(m.Task, " "))
	for _, marker := range []string{"qwen-image", "qwen_image", "text-to-image", "image-generation", "image-editing", "diffusion", "flux", "sdxl", "wan2"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	for _, task := range m.Task {
		task = strings.ToLower(task)
		if strings.Contains(task, "image") || strings.Contains(task, "图片") || strings.Contains(task, "video") || strings.Contains(task, "视频") {
			return true
		}
	}
	return false
}
func (ComfyUIAdapter) Detect(ctx context.Context, _ model.Manifest, h host.Capability) Plan {
	client := &http.Client{Timeout: 1200 * time.Millisecond}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:8188/system_stats", nil)
	if err == nil {
		if resp, e := client.Do(req); e == nil {
			resp.Body.Close()
			if resp.StatusCode/100 == 2 {
				return Plan{Adapter: "comfyui", Label: "ComfyUI", Status: Ready, Reason: "检测到本机 ComfyUI 服务"}
			}
		}
	}
	for _, r := range h.Runtimes {
		if r.Name == "comfyui" && r.Installed {
			return Plan{Adapter: "comfyui", Label: "ComfyUI", Status: NeedsSetup, Reason: "发现 ComfyUI 程序，但服务未启动", Setup: []string{"启动本机 ComfyUI 服务"}}
		}
	}
	return Plan{Adapter: "comfyui", Label: "ComfyUI", Status: NeedsSetup, Reason: "该模型属于图片或视频工作流，需要 ComfyUI 执行", Setup: []string{"ModelDock 尚未自动准备 ComfyUI 运行环境"}}
}
func (ComfyUIAdapter) Generate(context.Context, model.Manifest, string) (string, error) {
	return "", fmt.Errorf("ComfyUI 图片生成需要完整输入，请通过模型交互入口提交提示词")
}
func (ComfyUIAdapter) GenerateInput(ctx context.Context, m model.Manifest, input Input) (string, error) {
	return comfyGenerate(ctx, m, input)
}
