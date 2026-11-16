package local

import (
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
			task = "文本/图片"
		} else if hasRecursive(path, "*.bin") || strings.Contains(name, "mlx") {
			format = "mlx"
			task = "文本/图片"
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
