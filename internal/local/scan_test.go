package local

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanSafeTensorsTasks(t *testing.T) {
	for _, tc := range []struct{ name, config, want string }{
		{"DeepSeek", `{"architectures":["Qwen2ForCausalLM"],"model_type":"qwen2"}`, "文本"},
		{"Wan", `{"_class_name":"WanPipeline"}`, "视频"},
		{"SDXL", `{"_class_name":"StableDiffusionXLPipeline"}`, "图片"},
		{"unknown", `{}`, "未知"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(tc.config), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "model.safetensors"), []byte("weights"), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := Scan(dir)
			if err != nil || len(got) != 1 {
				t.Fatalf("scan: %v, %v", got, err)
			}
			if got[0].Task != tc.want {
				t.Fatalf("task = %q, want %q", got[0].Task, tc.want)
			}
		})
	}
}
