package runtime

import (
	"context"
	"testing"

	"github.com/modeldock/modeldock/internal/host"
	"github.com/modeldock/modeldock/internal/model"
)

func TestRegistryRoutesLocalGGUFToPureGo(t *testing.T) {
	m := model.Manifest{Source: "local", Format: "gguf"}
	want := "llama.cpp"
	if pureGoAvailable() {
		want = "pure-go"
	}
	if got := NewRegistry().Plan(context.Background(), m, host.Capability{}).Adapter; got != want {
		t.Fatalf("adapter = %q, want %q", got, want)
	}
}

func TestRegistryRoutesQwenImageAwayFromLlama(t *testing.T) {
	m := model.Manifest{Source: "local", Format: "gguf", Name: "chatrlabs/Qwen-Image-2.1-Uncensored-Omni-Quantized"}
	plan := NewRegistry().Plan(context.Background(), m, host.Capability{})
	if plan.Adapter != "comfyui" {
		t.Fatalf("adapter = %q, want comfyui", plan.Adapter)
	}
}
