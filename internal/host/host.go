package host

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

type Runtime struct {
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	Path      string `json:"path,omitempty"`
}
type GPU struct {
	Vendor   string `json:"vendor"`
	Name     string `json:"name"`
	MemoryGB uint64 `json:"memoryGB,omitempty"`
}
type Capability struct {
	OS           string    `json:"os"`
	Arch         string    `json:"arch"`
	CPU          string    `json:"cpu,omitempty"`
	MemoryGB     uint64    `json:"memoryGB,omitempty"`
	GPUs         []GPU     `json:"gpus,omitempty"`
	Runtimes     []Runtime `json:"runtimes"`
	Capabilities []string  `json:"capabilities"`
}

func Detect(ctx context.Context) Capability {
	c := Capability{OS: runtime.GOOS, Arch: runtime.GOARCH, Runtimes: []Runtime{}, Capabilities: []string{}}
	for _, x := range []struct {
		name string
		bins []string
	}{{"ollama", []string{"ollama"}}, {"llama.cpp", []string{"llama-server", "llama-cli", "llama-run"}}, {"comfyui", []string{"comfy"}}} {
		r := Runtime{Name: x.name}
		if x.name == "comfyui" {
			if configured := strings.TrimSpace(os.Getenv("MODELDOCK_COMFYUI_PATH")); configured != "" {
				if info, err := os.Stat(configured); err == nil && info.IsDir() {
					if _, err = os.Stat(filepath.Join(configured, "main.py")); err == nil {
						r.Installed, r.Path = true, configured
					}
				}
			}
		}
		if x.name == "llama.cpp" {
			if configured := strings.TrimSpace(os.Getenv("MODELDOCK_LLAMA_CPP_PATH")); configured != "" {
				if p, e := executablePath(configured); e == nil {
					r.Installed, r.Path = true, p
				}
			}
		}
		if r.Installed {
			c.Runtimes = append(c.Runtimes, r)
			continue
		}
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
	mlx := Runtime{Name: "mlx"}
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		if p, err := exec.LookPath("python3"); err == nil {
			mlx.Installed, mlx.Path = true, p
		}
	}
	c.Runtimes = append(c.Runtimes, mlx)
	if runtime.GOOS == "windows" {
		c.MemoryGB = windowsMemoryGB(ctx)
		c.CPU = windowsValue(ctx, "(Get-CimInstance Win32_Processor | Select-Object -First 1 -ExpandProperty Name)")
		c.GPUs = windowsGPUs(ctx)
		for _, gpu := range c.GPUs {
			name := strings.ToLower(gpu.Name)
			if strings.Contains(name, "nvidia") && !contains(c.Capabilities, "cuda") {
				c.Capabilities = append(c.Capabilities, "cuda")
			}
		}
	}
	if (runtime.GOOS == "windows" || runtime.GOOS == "linux") && contains(c.Capabilities, "cuda") {
		if version := nvidiaCUDAVersion(ctx); version != "" {
			c.Runtimes = append(c.Runtimes, Runtime{Name: "cuda", Installed: true, Version: version, Path: "nvidia-smi"})
			c.Capabilities = replaceCapability(c.Capabilities, "cuda", "cuda "+version)
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
	if c.CPU == "" {
		if out, e := exec.CommandContext(ctx, "uname", "-p").Output(); e == nil {
			c.CPU = strings.TrimSpace(string(out))
		}
	}
	return c
}

func executablePath(value string) (string, error) {
	path := value
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		name := "llama-server"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		path = filepath.Join(path, name)
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", os.ErrNotExist
	}
	return path, nil
}

func windowsValue(ctx context.Context, expression string) string {
	out, err := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", expression).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func windowsMemoryGB(ctx context.Context) uint64 {
	value := windowsValue(ctx, "[math]::Round((Get-CimInstance Win32_ComputerSystem).TotalPhysicalMemory / 1GB)")
	n, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
	if err == nil && n > 0 {
		return n
	}
	return 0
}

func windowsGPUs(ctx context.Context) []GPU {
	if out, err := exec.CommandContext(ctx, "nvidia-smi", "--query-gpu=name,memory.total", "--format=csv,noheader,nounits").Output(); err == nil {
		var gpus []GPU
		for _, line := range strings.Split(string(out), "\n") {
			parts := strings.Split(strings.TrimSpace(line), ",")
			if len(parts) < 2 || strings.TrimSpace(parts[0]) == "" {
				continue
			}
			gpu := GPU{Name: strings.TrimSpace(parts[0]), Vendor: "nvidia"}
			if megabytes, parseErr := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 64); parseErr == nil {
				gpu.MemoryGB = (megabytes + 512) / 1024
			}
			gpus = append(gpus, gpu)
		}
		if len(gpus) > 0 {
			return gpus
		}
	}
	value := windowsValue(ctx, "Get-CimInstance Win32_VideoController | ForEach-Object { $name=$_.Name; $ram=$_.AdapterRAM; if ($ram -and $ram -gt 0) { \"$name`t$ram\" } else { \"$name`t0\" } }")
	var gpus []GPU
	for _, line := range strings.Split(value, "\n") {
		parts := strings.Split(strings.TrimSpace(line), "\t")
		if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
			continue
		}
		if strings.Contains(strings.ToLower(parts[0]), "virtual") {
			continue
		}
		gpu := GPU{Name: strings.TrimSpace(parts[0]), Vendor: gpuVendor(parts[0])}
		if len(parts) > 1 {
			if bytes, err := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 64); err == nil {
				gpu.MemoryGB = bytes / (1024 * 1024 * 1024)
			}
		}
		gpus = append(gpus, gpu)
	}
	return gpus
}

func nvidiaCUDAVersion(ctx context.Context) string {
	out, err := exec.CommandContext(ctx, "nvidia-smi").Output()
	if err != nil {
		return ""
	}
	match := regexp.MustCompile(`CUDA (?:Version|UMD Version):\s*([0-9.]+)`).FindStringSubmatch(string(out))
	if len(match) == 2 {
		return match[1]
	}
	return ""
}

func gpuVendor(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "nvidia"):
		return "nvidia"
	case strings.Contains(lower, "amd"), strings.Contains(lower, "radeon"):
		return "amd"
	case strings.Contains(lower, "intel"):
		return "intel"
	default:
		return "unknown"
	}
}

func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func replaceCapability(values []string, oldValue, newValue string) []string {
	for index, value := range values {
		if value == oldValue {
			values[index] = newValue
			return values
		}
	}
	return append(values, newValue)
}
func (c Capability) JSON() []byte { b, _ := json.Marshal(c); return b }
