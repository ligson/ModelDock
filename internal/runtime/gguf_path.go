package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/modeldock/modeldock/internal/model"
)

func ggufPath(m model.Manifest) (string, error) {
	for _, file := range m.Files {
		candidates := []string{file.Path}
		if m.LocalPath != "" && !filepath.IsAbs(file.Path) {
			candidates = append([]string{filepath.Join(m.LocalPath, file.Path)}, candidates...)
		}
		for _, candidate := range candidates {
			if strings.HasSuffix(strings.ToLower(candidate), ".gguf") {
				if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
					return candidate, nil
				}
			}
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				found := ""
				_ = filepath.Walk(candidate, func(path string, entry os.FileInfo, walkErr error) error {
					if walkErr == nil && entry != nil && !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), ".gguf") {
						found = path
						return filepath.SkipDir
					}
					return nil
				})
				if found != "" {
					return found, nil
				}
			}
		}
	}
	return "", fmt.Errorf("no GGUF file found for %s", m.Name)
}
