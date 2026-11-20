package local

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/modeldock/modeldock/internal/model"
)

type Candidate struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Format  string `json:"format"`
	Size    int64  `json:"size"`
	Task    string `json:"task"`
	Managed bool   `json:"managed"`
}

func Scan(root string) ([]Candidate, error) {
	seen := map[string]bool{}
	out := []Candidate{}
	e := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		if !info.IsDir() {
			return nil
		}
		name := strings.ToLower(info.Name())
		if name == "objects" || name == ".git" {
			return filepath.SkipDir
		}
		if name == ".venv" || name == "site-packages" || name == "node_modules" {
			return filepath.SkipDir
		}
		format := ""
		task := ""
		marker := has(path, "config.json") || has(path, "model_index.json") || has(path, "*.gguf") || has(path, "*.safetensors") || has(path, "*.bin")
		if !marker {
			return nil
		}
		if hasRecursive(path, "*.gguf") {
			format = "gguf"
			task = "文本"
		} else if hasRecursive(path, "*.safetensors") {
			format = "safetensors"
			task = InferTask(path)
		} else if hasRecursive(path, "*.bin") || strings.Contains(name, "mlx") {
			format = "mlx"
			task = InferTask(path)
		}
		if format == "" {
			return nil
		}
		id := path
		if !seen[id] {
			seen[id] = true
			out = append(out, Candidate{ID: id, Name: info.Name(), Path: path, Format: format, Task: task, Size: dirSize(path)})
			return filepath.SkipDir
		}
		return nil
	})
	return out, e
}

// InferTask uses model metadata rather than the weight container format.
func InferTask(dir string) string {
	var config struct {
		Architectures []string `json:"architectures"`
		ClassName     string   `json:"_class_name"`
		ModelType     string   `json:"model_type"`
	}
	for _, file := range []string{"config.json", "model_index.json"} {
		data, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil || json.Unmarshal(data, &config) != nil {
			continue
		}
		metadata := strings.ToLower(strings.Join(config.Architectures, " ") + " " + config.ClassName + " " + config.ModelType)
		if strings.Contains(metadata, "causallm") || strings.Contains(metadata, "conditionalgeneration") && !strings.Contains(metadata, "whisper") {
			return "文本"
		}
		if strings.Contains(metadata, "wan") || strings.Contains(metadata, "video") {
			return "视频"
		}
		if strings.Contains(metadata, "whisper") || strings.Contains(metadata, "audio") {
			return "音频"
		}
		if strings.Contains(metadata, "diffusion") || strings.Contains(metadata, "flux") || strings.Contains(metadata, "image") {
			return "图片"
		}
	}
	name := strings.ToLower(filepath.Base(dir))
	if strings.Contains(name, "wan2") || strings.Contains(name, "t2v") || strings.Contains(name, "video") {
		return "视频"
	}
	if strings.Contains(name, "image") || strings.Contains(name, "diffusion") || strings.Contains(name, "flux") {
		return "图片"
	}
	return "未知"
}
func has(dir, pattern string) bool {
	m, _ := filepath.Glob(filepath.Join(dir, pattern))
	return len(m) > 0
}
func hasRecursive(dir, pattern string) bool {
	found := false
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() {
			if ok, _ := filepath.Match(pattern, info.Name()); ok {
				found = true
				return filepath.SkipDir
			}
		}
		return nil
	})
	return found
}
func dirSize(dir string) int64 {
	var n int64
	_ = filepath.Walk(dir, func(_ string, i os.FileInfo, e error) error {
		if e == nil && i != nil && !i.IsDir() {
			n += i.Size()
		}
		return nil
	})
	return n
}
func ToManifest(c Candidate) model.Manifest {
	return model.Manifest{ID: "local:" + c.ID, Name: c.Name, Source: "local", Format: c.Format, Task: []string{c.Task}, Files: []model.File{{Path: c.Path}}, CreatedAt: time.Now().UTC()}
}
