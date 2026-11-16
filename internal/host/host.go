package host

import (
	"context"
	"encoding/json"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

type Runtime struct {
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	Path      string `json:"path,omitempty"`
}
type Capability struct {
	OS           string    `json:"os"`
	Arch         string    `json:"arch"`
	CPU          string    `json:"cpu,omitempty"`
	MemoryGB     uint64    `json:"memoryGB,omitempty"`
	Runtimes     []Runtime `json:"runtimes"`
	Capabilities []string  `json:"capabilities"`
}

func Detect(ctx context.Context) Capability {
	c := Capability{OS: runtime.GOOS, Arch: runtime.GOARCH, Runtimes: []Runtime{}, Capabilities: []string{}}
	for _, x := range []struct {
		name string
		bins []string
	}{{"ollama", []string{"ollama"}}, {"llama.cpp", []string{"llama-server", "main"}}, {"comfyui", []string{"comfy"}}, {"mlx", []string{"python3"}}} {
		r := Runtime{Name: x.name}
		for _, b := range x.bins {
			if p, e := exec.LookPath(b); e == nil {
				r.Installed = true
				r.Path = p
				break
			}
		}
		c.Runtimes = append(c.Runtimes, r)
	}
	switch runtime.GOOS {
	case "darwin":
		c.Capabilities = append(c.Capabilities, "metal")
		if runtime.GOARCH == "arm64" {
			c.Capabilities = append(c.Capabilities, "mlx", "unified-memory")
		}
	case "linux", "windows":
		if _, e := exec.LookPath("nvidia-smi"); e == nil {
			c.Capabilities = append(c.Capabilities, "cuda")
		}
	}
	if out, e := exec.CommandContext(ctx, "sysctl", "-n", "hw.memsize").Output(); e == nil {
		if n, e := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64); e == nil {
			c.MemoryGB = n / (1024 * 1024 * 1024)
		}
	}
	if c.MemoryGB == 0 {
		if out, e := exec.CommandContext(ctx, "sh", "-c", "awk '/MemTotal/ {print $2}' /proc/meminfo").Output(); e == nil {
			if n, e := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64); e == nil {
				c.MemoryGB = n / (1024 * 1024)
			}
		}
	}
	if out, e := exec.CommandContext(ctx, "uname", "-p").Output(); e == nil {
		c.CPU = strings.TrimSpace(string(out))
	}
	return c
}
func (c Capability) JSON() []byte { b, _ := json.Marshal(c); return b }
