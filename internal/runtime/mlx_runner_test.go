//go:build !windows

package runtime

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/modeldock/modeldock/internal/host"
	"github.com/modeldock/modeldock/internal/model"
)

func TestMLXPlanRecognizesMiniMaxImage(t *testing.T) {
	root := t.TempDir()
	modelPath := filepath.Join(root, "models", "MiniMax-H3-Image-4bit")
	if err := os.MkdirAll(modelPath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "generate-image.sh"), []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	m := model.Manifest{Name: "MiniMax-H3-Image-4bit", Format: "safetensors", LocalPath: modelPath}
	if !(MLXAdapter{}).CanHandle(m) {
		t.Fatal("MiniMax H3 Image should select MLX adapter")
	}
	p := (MLXAdapter{}).Detect(context.Background(), m, host.Capability{Capabilities: []string{"mlx"}})
	if p.Status != Ready || p.Adapter != "mlx" {
		t.Fatalf("unexpected plan: %+v", p)
	}
}

func TestMLXProjectFindsAncestorScript(t *testing.T) {
	root := t.TempDir()
	modelPath := filepath.Join(root, "models", "MiniMax-H3-Image-4bit")
	if err := os.MkdirAll(modelPath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "generate-image.sh"), []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	found, kind, err := mlxProject(modelPath, model.Manifest{Name: "MiniMax-H3-Image-4bit"})
	if err != nil {
		t.Fatal(err)
	}
	if found != root || kind != "image" {
		t.Fatalf("got %q/%q", found, kind)
	}
}
