package api

import (
	"testing"

	"github.com/modeldock/modeldock/internal/model"
)

func TestSelectGGUFQuantizationDownloadsOnlySelectedWeights(t *testing.T) {
	manifest := model.Manifest{
		ID: "owner/model", Name: "owner/model", Format: "gguf",
		Files: []model.File{
			{Path: "model.Q2_K.gguf"},
			{Path: "model.Q4_K_M.gguf"},
			{Path: "model.Q8_0.gguf"},
			{Path: "model.mmproj-Q8_0.gguf"},
			{Path: "model.mmproj-f16.gguf"},
		},
	}
	got, err := selectGGUFQuantization(manifest, "Q4_K_M", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Files) != 1 || got.Files[0].Path != "model.Q4_K_M.gguf" || got.Quant != "Q4_K_M" || got.ID != "owner/model-gguf-q4_k_m" {
		t.Fatalf("unexpected selected manifest: %+v", got)
	}
}

func TestSelectGGUFQuantizationSupportsHyphenSeparatedNames(t *testing.T) {
	manifest := model.Manifest{ID: "owner/image", Name: "owner/image", Format: "gguf", Files: []model.File{
		{Path: "qwen-image-2.1-step90000-Q4_0.gguf"},
		{Path: "qwen-image-2.1-step90000-Q4_K_M.gguf"},
		{Path: "qwen-image-2.1-step90000-Q5_K_M.gguf"},
	}}
	got, err := selectGGUFQuantization(manifest, "Q4_K_M", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Files) != 1 || got.Files[0].Path != "qwen-image-2.1-step90000-Q4_K_M.gguf" {
		t.Fatalf("unexpected selected file: %+v", got.Files)
	}
}

func TestSelectGGUFQuantizationCanIncludeMultimodalProjection(t *testing.T) {
	manifest := model.Manifest{Format: "gguf", Files: []model.File{
		{Path: "model.Q4_K_M.gguf"},
		{Path: "model.mmproj-Q8_0.gguf"},
		{Path: "model.mmproj-f16.gguf"},
	}}
	got, err := selectGGUFQuantization(manifest, "Q4_K_M", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Files) != 2 || got.Files[1].Path != "model.mmproj-Q8_0.gguf" {
		t.Fatalf("unexpected multimodal files: %+v", got.Files)
	}
}

func TestSelectGGUFQuantizationRejectsMissingOrWrongFormat(t *testing.T) {
	if _, err := selectGGUFQuantization(model.Manifest{Format: "safetensors"}, "Q4_K_M", false); err == nil {
		t.Fatal("expected non-GGUF format to be rejected")
	}
	if _, err := selectGGUFQuantization(model.Manifest{Format: "gguf", Files: []model.File{{Path: "model.Q8_0.gguf"}}}, "Q4_K_M", false); err == nil {
		t.Fatal("expected missing quantization to be rejected")
	}
}
