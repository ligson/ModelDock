package recommend

import (
	"github.com/modeldock/modeldock/internal/host"
	"github.com/modeldock/modeldock/internal/model"
	"strings"
)

type Result struct {
	Model  model.Manifest `json:"model"`
	Status string         `json:"status"`
	Score  int            `json:"score"`
	Reason string         `json:"reason"`
}

func One(m model.Manifest, h host.Capability) Result {
	score := 50
	status := "可以运行"
	reason := []string{}
	if len(m.Runtimes) > 0 {
		ok := false
		for _, want := range m.Runtimes {
			for _, r := range h.Runtimes {
				if r.Installed && strings.EqualFold(want, r.Name) {
					ok = true
				}
			}
		}
		if !ok {
			status = "不可用"
			score = 0
			reason = append(reason, "缺少兼容的已安装运行时")
		}
	}
	if strings.Contains(strings.ToLower(m.Format), "mlx") && !contains(h.Capabilities, "mlx") {
		status = "不可用"
		score = 0
		reason = append(reason, "需要 Apple MLX 能力")
	}
	if status != "不可用" {
		if h.MemoryGB >= 32 {
			score += 30
			status = "推荐"
		} else {
			score += 10
			reason = append(reason, "系统内存余量有限")
		}
	}
	if len(reason) == 0 {
		reason = append(reason, "格式和已安装运行时匹配")
	}
	return Result{Model: m, Status: status, Score: score, Reason: strings.Join(reason, "；")}
}
func contains(v []string, x string) bool {
	for _, a := range v {
		if a == x {
			return true
		}
	}
	return false
}
