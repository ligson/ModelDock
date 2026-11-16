//go:build !windows

package runtime

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/modeldock/modeldock/internal/host"
	"github.com/modeldock/modeldock/internal/model"
)

func TestPureGoIntegration(t *testing.T) {
	path := os.Getenv("MODELDOCK_TEST_GGUF")
	if path == "" {
		t.Skip("set MODELDOCK_TEST_GGUF to run the pure Go model smoke test")
	}
	m := model.Manifest{ID: "smoke", Name: "Qwen3 smoke", Source: "local", Format: "gguf", Files: []model.File{{Path: path}}}
	plan := (PureGoAdapter{}).Detect(context.Background(), m, host.Capability{})
	if plan.Status != Ready {
		t.Fatalf("plan status = %q: %s", plan.Status, plan.Reason)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	answer, err := (PureGoAdapter{}).Generate(ctx, m, "Reply with the word ready.")
	if err != nil {
		t.Fatal(err)
	}
	if answer == "" {
		t.Fatal("model returned an empty answer")
	}
}
