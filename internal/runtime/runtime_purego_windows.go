//go:build windows

package runtime

import (
	"context"
	"fmt"

	"github.com/modeldock/modeldock/internal/model"
)

func mlxProject(string, model.Manifest) (string, string, error) {
	return "", "", fmt.Errorf("Windows 当前不支持 MLX 运行时")
}

func pureGoDetect(model.Manifest) Plan {
	return Plan{Adapter: "pure-go", Label: "纯 Go 推理", Status: Unsupported, Reason: "当前纯 Go GGUF 推理组件暂未提供 Windows 构建"}
}
func pureGoGenerate(context.Context, model.Manifest, string) (string, error) {
	return "", fmt.Errorf("pure Go GGUF inference is not available in this Windows build")
}

func mlxGenerate(context.Context, model.Manifest, string) (string, error) {
	return "", fmt.Errorf("Windows 当前不支持 MLX 运行时")
}

func mlxGenerateInput(context.Context, model.Manifest, Input) (string, error) {
	return "", fmt.Errorf("Windows 当前不支持 MLX 运行时")
}
