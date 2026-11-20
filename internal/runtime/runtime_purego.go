//go:build !windows

package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/ThiraSoft/golem/chat"
	"github.com/ThiraSoft/golem/engine"
	"github.com/ThiraSoft/golem/sample"
	"github.com/ThiraSoft/golem/tensors"
	"github.com/modeldock/modeldock/internal/model"
)

func pureGoDetect(m model.Manifest) Plan {
	path, err := ggufPath(m)
	if err != nil {
		return Plan{Adapter: "pure-go", Label: "纯 Go 推理", Status: Unsupported, Reason: "没有找到可读取的 GGUF 权重文件"}
	}
	g, err := tensors.OpenGGUF(path)
	if err != nil {
		return Plan{Adapter: "pure-go", Label: "纯 Go 推理", Status: Unsupported, Reason: "GGUF 文件无法读取：" + err.Error()}
	}
	arch, err := g.String("general.architecture")
	_ = g.Close()
	if err != nil {
		return Plan{Adapter: "pure-go", Label: "纯 Go 推理", Status: Unsupported, Reason: "模型缺少架构信息"}
	}
	switch arch {
	case "gemma4", "qwen3", "qwen35", "qwen3.8":
		return Plan{Adapter: "pure-go", Label: "纯 Go 推理", Status: Ready, Reason: "此 GGUF 架构已由内置纯 Go 引擎支持（" + arch + "）"}
	default:
		return Plan{Adapter: "pure-go", Label: "纯 Go 推理", Status: Unsupported, Reason: "当前纯 Go 引擎暂不支持此 GGUF 架构（" + arch + "）"}
	}
}

func pureGoGenerate(ctx context.Context, m model.Manifest, prompt string) (string, error) {
	path, err := ggufPath(m)
	if err != nil {
		return "", err
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	opened, err := engine.Open(path, 2048)
	if err != nil {
		return "", fmt.Errorf("open GGUF with pure Go engine: %w", err)
	}
	defer opened.Close()
	rendered, err := opened.Template.Render([]chat.Message{{Role: "user", Content: prompt}}, chat.Options{AddGenerationPrompt: true})
	if err != nil {
		return "", fmt.Errorf("render model prompt: %w", err)
	}
	ids := opened.Vocab.Encode(rendered, false, true)
	if len(ids) == 0 || len(ids) >= opened.SlotContext() {
		return "", fmt.Errorf("prompt is empty or exceeds model context")
	}
	states := opened.Forward.ForwardBatch(ids, 0)
	hidden := states[len(states)-1]
	params := opened.Sampling
	if params.TopK == 0 {
		params = sample.Defaults()
	}
	sampler := sample.New(params)
	logits := make([]float32, opened.Vocabulary)
	var out strings.Builder
	for i := 0; i < 96 && len(ids)+i < opened.SlotContext(); i++ {
		if err = ctx.Err(); err != nil {
			return "", err
		}
		opened.Forward.Logits(hidden, logits)
		id := sampler.Pick(logits)
		if opened.Vocab.IsEOG(id) {
			break
		}
		out.WriteString(opened.Vocab.Piece(id, false))
		ids = append(ids, id)
		hidden = opened.Forward.ForwardBatch([]int32{id}, len(ids)-1)[0]
	}
	return strings.TrimSpace(out.String()), nil
}
