package runtime

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/modeldock/modeldock/internal/model"
)

func TestGGUFPathResolvesManagedModelFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "weights.Q4_K_M.gguf")
	if err := os.WriteFile(path, []byte("gguf"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := ggufPath(model.Manifest{LocalPath: root, Files: []model.File{{Path: filepath.Base(path)}}})
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Fatalf("path = %q, want %q", got, path)
	}
}

func TestLlamaServerPathAcceptsConfiguredExecutable(t *testing.T) {
	path := filepath.Join(t.TempDir(), llamaServerName())
	if err := os.WriteFile(path, []byte("test executable placeholder"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := llamaServerPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Fatalf("server path = %q, want %q", got, path)
	}
}
