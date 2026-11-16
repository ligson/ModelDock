package catalog

type Item struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	Format      string `json:"format"`
	Description string `json:"description"`
	Source      string `json:"source"`
	Popular     bool   `json:"popular"`
}

func Popular() []Item {
	return []Item{
		{ID: "Qwen/Qwen3-8B", Name: "Qwen3 8B", Category: "文本", Format: "safetensors", Description: "通用对话与推理", Source: "huggingface", Popular: true},
		{ID: "Qwen/Qwen2.5-Coder-7B-Instruct", Name: "Qwen2.5 Coder 7B", Category: "代码", Format: "safetensors", Description: "代码生成与补全", Source: "huggingface", Popular: true},
		{ID: "stabilityai/stable-diffusion-xl-base-1.0", Name: "Stable Diffusion XL", Category: "图片", Format: "diffusers", Description: "文生图基础模型", Source: "huggingface", Popular: true},
		{ID: "black-forest-labs/FLUX.1-schnell", Name: "FLUX.1 Schnell", Category: "图片", Format: "diffusers", Description: "快速高质量文生图", Source: "huggingface", Popular: true},
		{ID: "Wan-AI/Wan2.1-T2V-1.3B", Name: "Wan 2.1 1.3B", Category: "视频", Format: "safetensors", Description: "文本生成视频", Source: "huggingface", Popular: true},
		{ID: "openai/whisper-large-v3-turbo", Name: "Whisper Large V3 Turbo", Category: "声音", Format: "safetensors", Description: "语音识别", Source: "huggingface", Popular: true},
	}
}
