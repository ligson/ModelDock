package runtime

import (
	"context"
	"testing"

	"github.com/modeldock/modeldock/internal/host"
	"github.com/modeldock/modeldock/internal/model"
)

func TestRegistryRoutesLocalGGUFToPureGo(t *testing.T) {
	m := model.Manifest{Source: "local", Format: "gguf"}
	if got := NewRegistry().Plan(context.Background(), m, host.Capability{}).Adapter; got != "pure-go" {
		t.Fatalf("adapter = %q, want pure-go", got)
	}
}
