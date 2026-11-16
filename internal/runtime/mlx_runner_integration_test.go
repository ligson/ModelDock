//go:build !windows

package runtime

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/modeldock/modeldock/internal/model"
)

func TestMLXGenerateDelegatesToModelProjectScript(t *testing.T) {
	root := t.TempDir()
	modelDir := filepath.Join(root, "models", "MiniMax-H3-Image-4bit")
	if err := os.MkdirAll(modelDir, 0755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nprintf 'generated' > \"$2\"\n"
	if err := os.WriteFile(filepath.Join(root, "generate-image.sh"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	got, err := mlxGenerate(context.Background(), model.Manifest{Name: "MiniMax-H3-Image-4bit", LocalPath: modelDir}, "a test image")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "generated" {
		t.Fatalf("output = %q", data)
	}
	_ = os.Remove(got)
}

func TestMLXGenerateEditReturnsOutputPath(t *testing.T) {
	root := t.TempDir()
	modelDir := filepath.Join(root, "models", "MiniMax-H3-Image-4bit")
	if err := os.MkdirAll(modelDir, 0755); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(root, "input.png")
	if err := os.WriteFile(input, []byte("input"), 0644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nprintf 'edited' > \"$4\"\n"
	if err := os.WriteFile(filepath.Join(root, "generate-image.sh"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	got, err := mlxGenerateInput(context.Background(), model.Manifest{Name: "MiniMax-H3-Image-4bit", LocalPath: modelDir}, Input{Prompt: "edit", ImagePath: input})
	if err != nil {
		t.Fatal(err)
	}
	if got == input {
		t.Fatalf("edit returned input path %q", got)
	}
	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "edited" {
		t.Fatalf("output = %q", data)
	}
	_ = os.Remove(got)
}
