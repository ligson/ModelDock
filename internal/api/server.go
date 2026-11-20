package api

import (
	"archive/zip"
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	stdruntime "runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/modeldock/modeldock/internal/catalog"
	"github.com/modeldock/modeldock/internal/db"
	"github.com/modeldock/modeldock/internal/download"
	"github.com/modeldock/modeldock/internal/host"
	localmodels "github.com/modeldock/modeldock/internal/local"
	"github.com/modeldock/modeldock/internal/model"
	"github.com/modeldock/modeldock/internal/recommend"
	"github.com/modeldock/modeldock/internal/runtime"
	"github.com/modeldock/modeldock/internal/source"
)

//go:embed static
var staticFiles embed.FS

type Server struct {
	DB             *db.DB
	Downloads      *download.Manager
	Runtimes       runtime.Registry
	GenerationMu   sync.RWMutex
	Generations    map[string]*GenerationJob
	ConversationMu sync.RWMutex
	Conversations  map[string][]ChatMessage
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type GenerationStep struct {
	Key        string    `json:"key"`
	Label      string    `json:"label"`
	Status     string    `json:"status"`
	Detail     string    `json:"detail,omitempty"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt,omitempty"`
}
type GenerationJob struct {
	ID             string           `json:"id"`
	ModelID        string           `json:"modelId"`
	ConversationID string           `json:"conversationId,omitempty"`
	Status         string           `json:"status"`
	Phase          string           `json:"phase"`
	Message        string           `json:"message,omitempty"`
	Response       string           `json:"response,omitempty"`
	ArtifactURL    string           `json:"artifactURL,omitempty"`
	ArtifactName   string           `json:"artifactName,omitempty"`
	ArtifactBytes  int64            `json:"artifactBytes,omitempty"`
	Error          string           `json:"error,omitempty"`
	StartedAt      time.Time        `json:"startedAt"`
	FinishedAt     time.Time        `json:"finishedAt,omitempty"`
	Steps          []GenerationStep `json:"steps"`
}

type APIResponse struct {
	Success  bool   `json:"success"`
	Data     any    `json:"data"`
	ErrorMsg string `json:"errorMsg"`
	Error    string `json:"error"`
}

type apiCaptureWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *apiCaptureWriter) Header() http.Header { return w.header }
func (w *apiCaptureWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *apiCaptureWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(p)
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.health)
	mux.HandleFunc("/api/models", s.models)
	mux.HandleFunc("/api/models/", s.model)
	mux.HandleFunc("/api/tasks", s.tasks)
	mux.HandleFunc("/api/tasks/", s.task)
	mux.HandleFunc("/api/search", s.search)
	mux.HandleFunc("/api/source/manifest", s.manifest)
	mux.HandleFunc("/api/host", s.host)
	mux.HandleFunc("/api/recommend", s.recommend)
	mux.HandleFunc("/api/catalog", s.catalog)
	mux.HandleFunc("/api/storage", s.storage)
	mux.HandleFunc("/api/storage/roots", s.storageRoots)
	mux.HandleFunc("/api/storage/migrate", s.storageMigrate)
	mux.HandleFunc("/api/generate", s.generate)
	mux.HandleFunc("/api/generate/jobs/", s.generationJob)
	mux.HandleFunc("/api/uploads", s.upload)
	mux.HandleFunc("/api/artifacts/", s.artifact)
	mux.HandleFunc("/api/runtime/plan", s.runtimePlan)
	mux.HandleFunc("/api/runtime", s.runtime)
	mux.HandleFunc("/api/runtime/engines", s.runtimeEngines)
	mux.HandleFunc("/api/runtime/comfyui", s.comfyUI)
	mux.HandleFunc("/api/runtime/comfyui/prepare", s.prepareComfyUI)
	mux.HandleFunc("/api/runtime/comfyui/component-download", s.downloadComfyComponent)
	mux.HandleFunc("/api/runtime/comfyui/restart", s.restartComfyUI)
	mux.HandleFunc("/api/runtime/comfyui/start", s.startComfyUI)
	mux.HandleFunc("/api/runtime/comfyui/log", s.comfyUILog)
	mux.HandleFunc("/api/ollama", s.ollama)
	mux.HandleFunc("/api/ollama/import", s.ollamaImport)
	mux.HandleFunc("/api/local/scan", s.localScan)
	mux.HandleFunc("/api/local/scan-all", s.localScanAll)
	mux.HandleFunc("/api/local/import", s.localImport)
	mux.HandleFunc("/api/local/unimport", s.localUnimport)
	ui, _ := fs.Sub(staticFiles, "static")
	fileHandler := http.FileServer(http.FS(ui))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			fileHandler.ServeHTTP(w, r)
			return
		}
		capture := &apiCaptureWriter{header: make(http.Header)}
		mux.ServeHTTP(capture, r)
		status := capture.status
		if status == 0 {
			status = http.StatusOK
		}
		if status >= 400 {
			msg := strings.TrimSpace(capture.body.String())
			if msg == "" {
				msg = http.StatusText(status)
			}
			writeJSONStatus(w, status, APIResponse{Success: false, Data: nil, ErrorMsg: msg, Error: msg})
			return
		}
		for key, values := range capture.header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(status)
		_, _ = w.Write(capture.body.Bytes())
	})
}

// comfyUI downloads the upstream ComfyUI source into a ModelDock-managed
// directory. External installations are never modified by this endpoint.
func (s *Server) comfyUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		Path   string `json:"path"`
		Action string `json:"action"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil || strings.TrimSpace(in.Path) == "" {
		http.Error(w, "ComfyUI 目录不能为空", http.StatusBadRequest)
		return
	}
	if in.Action != "install" && in.Action != "update" {
		http.Error(w, "不支持的 ComfyUI 操作", http.StatusBadRequest)
		return
	}
	path, err := filepath.Abs(strings.TrimSpace(in.Path))
	if err != nil {
		http.Error(w, "ComfyUI 目录无效", http.StatusBadRequest)
		return
	}
	configured := s.DB.Setting(r.Context(), "runtime.comfyui.path", "")
	mode := s.DB.Setting(r.Context(), "runtime.comfyui.mode", "external")
	if mode != "managed" || (configured != "" && filepath.Clean(configured) != filepath.Clean(path)) {
		http.Error(w, "只有 ModelDock 管理的 ComfyUI 目录允许自动安装或更新", http.StatusBadRequest)
		return
	}
	if err := installComfySource(r.Context(), path, in.Action == "update"); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	_ = s.DB.SetSetting(r.Context(), "runtime.comfyui.path", path)
	writeJSON(w, discoverComfyEngine(path, "managed", s.DB.Setting(r.Context(), "runtime.comfyui.api_port", "8188"), "false"))
}

func (s *Server) startComfyUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		Path string `json:"path"`
		Port int    `json:"port"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil || strings.TrimSpace(in.Path) == "" {
		http.Error(w, "ComfyUI 目录不能为空", http.StatusBadRequest)
		return
	}
	if in.Port < 1 || in.Port > 65535 {
		in.Port = 8188
	}
	if err := startComfyProcess(in.Path, in.Port, s.DB.Setting(r.Context(), "runtime.comfyui.mode", "external") == "managed"); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	mode := s.DB.Setting(r.Context(), "runtime.comfyui.mode", "external")
	writeJSON(w, discoverComfyEngine(in.Path, mode, fmt.Sprint(in.Port), s.DB.Setting(r.Context(), "runtime.comfyui.auto_update", "false")))
}

func (s *Server) restartComfyUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		Path string `json:"path"`
		Port int    `json:"port"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	if strings.TrimSpace(in.Path) == "" {
		in.Path = s.DB.Setting(r.Context(), "runtime.comfyui.path", "")
	}
	if strings.TrimSpace(in.Path) == "" {
		http.Error(w, "请先在运行时设置中配置 ComfyUI 目录", http.StatusBadRequest)
		return
	}
	if in.Port < 1 || in.Port > 65535 {
		in.Port = 8188
	}
	if err := stopComfyProcess(in.Port); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := startComfyProcess(in.Path, in.Port, s.DB.Setting(r.Context(), "runtime.comfyui.mode", "external") == "managed"); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	mode := s.DB.Setting(r.Context(), "runtime.comfyui.mode", "external")
	writeJSON(w, discoverComfyEngine(in.Path, mode, fmt.Sprint(in.Port), s.DB.Setting(r.Context(), "runtime.comfyui.auto_update", "false")))
}

func stopComfyProcess(port int) error {
	if !comfyHealth(port) {
		return nil
	}
	if stdruntime.GOOS == "windows" {
		out, err := exec.Command("cmd", "/c", "netstat -ano -p tcp").Output()
		if err != nil {
			return fmt.Errorf("无法查找 ComfyUI 进程：%w", err)
		}
		needle := fmt.Sprintf(":%d", port)
		pids := map[string]bool{}
		for _, line := range strings.Split(string(out), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 5 && strings.Contains(fields[1], needle) {
				pids[fields[4]] = true
			}
		}
		for pid := range pids {
			if err := exec.Command("taskkill", "/PID", pid, "/T", "/F").Run(); err != nil {
				return fmt.Errorf("无法停止 ComfyUI 进程：%w", err)
			}
		}
	} else {
		if err := exec.Command("sh", "-c", fmt.Sprintf("lsof -ti tcp:%d | xargs -r kill", port)).Run(); err != nil {
			return fmt.Errorf("无法停止 ComfyUI 进程：%w", err)
		}
	}
	for i := 0; i < 20; i++ {
		time.Sleep(250 * time.Millisecond)
		if !comfyHealth(port) {
			return nil
		}
	}
	return errors.New("ComfyUI 进程未退出，请关闭后重试")
}

// prepareComfyUI installs the small adapter needed by GGUF image models and
// adds the selected model directory to ComfyUI's search paths. It is explicit
// because an external ComfyUI installation must never be changed silently.
func (s *Server) prepareComfyUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		ModelID string `json:"modelId"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil || strings.TrimSpace(in.ModelID) == "" {
		http.Error(w, "modelId 不能为空", http.StatusBadRequest)
		return
	}
	m, err := s.DB.Model(r.Context(), in.ModelID)
	if err != nil {
		http.Error(w, "模型不存在", http.StatusNotFound)
		return
	}
	root := s.DB.Setting(r.Context(), "runtime.comfyui.path", "")
	if strings.TrimSpace(root) == "" {
		http.Error(w, "请先在运行时设置中配置 ComfyUI 目录", http.StatusBadRequest)
		return
	}
	if _, err = os.Stat(filepath.Join(root, "main.py")); err != nil {
		http.Error(w, "ComfyUI 目录中没有 main.py", http.StatusBadRequest)
		return
	}
	if err = installComfyGGUFNode(r.Context(), root); err != nil {
		http.Error(w, "安装 ComfyUI-GGUF 失败："+err.Error(), http.StatusBadGateway)
		return
	}
	if m.LocalPath == "" {
		http.Error(w, "模型没有本地路径，无法自动接入 ComfyUI", http.StatusBadRequest)
		return
	}
	if err = addComfyModelPath(root, m.LocalPath); err != nil {
		http.Error(w, "接入模型目录失败："+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"installed": true, "node": "ComfyUI-GGUF", "modelPath": m.LocalPath, "restartRequired": true, "recommendations": []map[string]any{
		{"kind": "text_encoder", "name": "qwen3vl_8b_w4a8.safetensors", "folder": "models/text_encoders", "sizeBytes": int64(6312105364), "url": "https://huggingface.co/Comfy-Org/Qwen-Image-2.1/resolve/main/text_encoders/qwen3vl_8b_w4a8.safetensors"},
		{"kind": "vae", "name": "qwen_image_2.1_vae_bf16.safetensors", "folder": "models/vae", "sizeBytes": int64(675509688), "url": "https://huggingface.co/Comfy-Org/Qwen-Image-2.1/resolve/main/vae/qwen_image_2.1_vae_bf16.safetensors"},
	}, "message": "已安装 ComfyUI-GGUF 并接入模型目录；请重启 ComfyUI 后重新生成。推荐组件：qwen3vl_8b_w4a8.safetensors（约 5.9 GB）放入 models/text_encoders，qwen_image_2.1_vae_bf16.safetensors（约 645 MB）放入 models/vae。下载地址来自 Comfy-Org/Qwen-Image-2.1"})
}

func (s *Server) downloadComfyComponent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		http.Error(w, "组件参数无效", http.StatusBadRequest)
		return
	}
	components := map[string]struct {
		Role, Folder, Path string
	}{
		"qwen3vl_8b_w4a8.safetensors":         {"Qwen-Image 文本编码器", "text_encoders", "text_encoders/qwen3vl_8b_w4a8.safetensors"},
		"qwen_image_2.1_vae_bf16.safetensors": {"Qwen-Image VAE 解码器", "vae", "vae/qwen_image_2.1_vae_bf16.safetensors"},
	}
	component, ok := components[strings.TrimSpace(in.Name)]
	if !ok {
		http.Error(w, "不支持的组件文件", http.StatusBadRequest)
		return
	}
	root := s.DB.Setting(r.Context(), "runtime.comfyui.path", "")
	if root == "" {
		http.Error(w, "请先配置 ComfyUI 目录", http.StatusBadRequest)
		return
	}
	manifest, err := (source.HuggingFace{}).Manifest(r.Context(), "Comfy-Org/Qwen-Image-2.1")
	if err != nil {
		http.Error(w, "读取 Hugging Face 组件清单失败："+err.Error(), http.StatusBadGateway)
		return
	}
	var file model.File
	for _, candidate := range manifest.Files {
		if candidate.Path == component.Path {
			file = candidate
			break
		}
	}
	if file.URL == "" {
		http.Error(w, "官方仓库中未找到该组件文件", http.StatusBadGateway)
		return
	}
	target := filepath.Join(root, "models", component.Folder)
	if err := os.MkdirAll(target, 0755); err != nil {
		http.Error(w, "无法创建组件目录："+err.Error(), http.StatusInternalServerError)
		return
	}
	id := "component:qwen-image:" + strings.TrimSuffix(component.Folder, "s") + ":" + strings.TrimSuffix(component.Role, " ")
	file.Path = in.Name
	m := model.Manifest{ID: id, Name: in.Name, Source: "huggingface", Format: "safetensors", Task: []string{"comfyui-component"}, LocalPath: target, Available: false, Files: []model.File{file}, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := s.DB.SaveModel(r.Context(), m); err != nil {
		http.Error(w, "保存组件任务失败："+err.Error(), http.StatusInternalServerError)
		return
	}
	task, err := s.Downloads.Start(context.Background(), m)
	if err != nil {
		http.Error(w, "创建下载任务失败："+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"task": task, "name": in.Name, "role": component.Role, "target": target})
}

func (s *Server) comfyUILog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	path := s.DB.Setting(r.Context(), "runtime.comfyui.path", "")
	if strings.TrimSpace(path) == "" {
		http.Error(w, "尚未配置 ComfyUI 目录", http.StatusBadRequest)
		return
	}
	data, err := os.ReadFile(filepath.Join(path, "modeldock-comfyui.log"))
	if err != nil && !os.IsNotExist(err) {
		http.Error(w, "读取 ComfyUI 日志失败："+err.Error(), http.StatusInternalServerError)
		return
	}
	const maxLogBytes = 16000
	if len(data) > maxLogBytes {
		data = data[len(data)-maxLogBytes:]
	}
	writeJSON(w, map[string]string{"path": filepath.Join(path, "modeldock-comfyui.log"), "content": string(data)})
}

func startComfyProcess(root string, port int, managed bool) error {
	root, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		return fmt.Errorf("ComfyUI 目录无效：%w", err)
	}
	if stdruntime.GOOS == "darwin" && strings.HasSuffix(strings.ToLower(root), ".app") {
		if comfyHealth(port) {
			return nil
		}
		if err := exec.Command("open", root).Run(); err != nil {
			return fmt.Errorf("无法打开 Comfy Desktop：%w", err)
		}
		for i := 0; i < 120; i++ {
			time.Sleep(500 * time.Millisecond)
			if comfyHealth(port) {
				return nil
			}
		}
		return fmt.Errorf("Comfy Desktop 已打开，但 API 端口 %d 尚未就绪", port)
	}
	if info, err := os.Stat(filepath.Join(root, "main.py")); err != nil || info.IsDir() {
		return fmt.Errorf("目录中没有 ComfyUI main.py")
	}
	if comfyHealth(port) {
		return nil
	}
	python := ""
	systemPython := ""
	for _, candidate := range []string{
		filepath.Join(root, "python_embeded", "python.exe"),
		"python", "python3",
	} {
		if filepath.IsAbs(candidate) {
			if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
				systemPython = candidate
				break
			}
			continue
		}
		if found, lookErr := exec.LookPath(candidate); lookErr == nil {
			systemPython = found
			break
		}
	}
	logFile, err := os.OpenFile(filepath.Join(root, "modeldock-comfyui.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("无法创建 ComfyUI 日志：%w", err)
	}
	defer logFile.Close()
	if systemPython == "" {
		return fmt.Errorf("未找到 Python。请安装 Python 3.10+，ModelDock 会自动准备其余依赖")
	}
	python = comfyVenvPython(root)
	if !managed {
		if _, statErr := os.Stat(python); statErr != nil {
			return fmt.Errorf("外部 ComfyUI 未找到独立 Python 环境；请先在该目录准备依赖，ModelDock 不会修改外部目录")
		}
	}
	if managed {
		if _, statErr := os.Stat(python); statErr != nil {
			if err := runComfyCommand(logFile, systemPython, "-m", "venv", filepath.Join(root, "venv")); err != nil {
				return fmt.Errorf("创建 ComfyUI 独立环境失败：%w；请查看日志", err)
			}
		}
		if _, nvidiaErr := exec.LookPath("nvidia-smi"); nvidiaErr == nil && !comfyTorchCUDA(python) {
			if err := runComfyCommand(logFile, python, "-m", "pip", "install", "--disable-pip-version-check", "--force-reinstall", "--no-deps", "--index-url", "https://download.pytorch.org/whl/cu128", "torch", "torchvision", "torchaudio"); err != nil {
				return fmt.Errorf("CUDA 版 PyTorch 安装失败：%w；请查看日志", err)
			}
			if !comfyTorchCUDA(python) {
				return fmt.Errorf("已安装依赖，但当前 PyTorch 仍不支持 CUDA；请查看日志")
			}
		}
	}
	if managed {
		if _, markerErr := os.Stat(filepath.Join(root, ".modeldock-deps-ready")); markerErr != nil {
			requirements := filepath.Join(root, "requirements.txt")
			if _, reqErr := os.Stat(requirements); reqErr != nil {
				return fmt.Errorf("未找到 requirements.txt")
			}
			if err := runComfyCommand(logFile, python, "-m", "pip", "install", "--disable-pip-version-check", "-r", requirements); err != nil {
				return fmt.Errorf("ComfyUI 依赖安装失败：%w；请查看日志", err)
			}
			if markerErr := os.WriteFile(filepath.Join(root, ".modeldock-deps-ready"), []byte(time.Now().UTC().Format(time.RFC3339)), 0644); markerErr != nil {
				return fmt.Errorf("无法保存依赖状态：%w", markerErr)
			}
		}
	}
	cmd := exec.Command(python, "main.py", "--listen", "127.0.0.1", "--port", fmt.Sprint(port))
	cmd.Dir, cmd.Stdout, cmd.Stderr = root, logFile, logFile
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 ComfyUI 失败：%w", err)
	}
	go func() { _ = cmd.Wait() }()
	for i := 0; i < 20; i++ {
		time.Sleep(500 * time.Millisecond)
		if comfyHealth(port) {
			return nil
		}
	}
	return fmt.Errorf("ComfyUI 进程已启动但服务未响应，请查看 %s", filepath.Join(root, "modeldock-comfyui.log"))
}

func comfyTorchCUDA(python string) bool {
	out, err := exec.Command(python, "-c", "import torch; print('1' if torch.cuda.is_available() else '0')").Output()
	return err == nil && strings.TrimSpace(string(out)) == "1"
}

func comfyVenvPython(root string) string {
	if stdruntime.GOOS == "windows" {
		return filepath.Join(root, "venv", "Scripts", "python.exe")
	}
	return filepath.Join(root, "venv", "bin", "python")
}

func runComfyCommand(logFile *os.File, command string, args ...string) error {
	_, _ = fmt.Fprintf(logFile, "\n[%s] ModelDock 自动执行：%s %s\n", time.Now().UTC().Format(time.RFC3339), command, strings.Join(args, " "))
	cmd := exec.Command(command, args...)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	return cmd.Run()
}

func installComfySource(ctx context.Context, target string, update bool) error {
	if !update {
		if info, err := os.Stat(target); err == nil && info.IsDir() {
			if _, mainErr := os.Stat(filepath.Join(target, "main.py")); mainErr == nil {
				return fmt.Errorf("目标目录已经存在 ComfyUI，请使用更新操作")
			}
			entries, readErr := os.ReadDir(target)
			if readErr != nil {
				return fmt.Errorf("无法检查目标目录：%w", readErr)
			}
			if len(entries) == 0 {
				if removeErr := os.Remove(target); removeErr != nil {
					return fmt.Errorf("无法准备空目标目录：%w", removeErr)
				}
			} else {
				return fmt.Errorf("目标目录已有文件，请选择空目录或使用更新操作")
			}
		}
	}
	archiveURL := "https://codeload.github.com/comfyanonymous/ComfyUI/zip/refs/heads/master"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, archiveURL, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("下载 ComfyUI 失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("下载 ComfyUI 失败：HTTP %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 200<<20))
	if err != nil {
		return fmt.Errorf("读取 ComfyUI 压缩包失败：%w", err)
	}
	if len(data) == 0 {
		return fmt.Errorf("ComfyUI 下载内容为空")
	}
	temp, err := os.MkdirTemp(filepath.Dir(target), ".modeldock-comfyui-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("ComfyUI 压缩包无效：%w", err)
	}
	root := ""
	for _, file := range reader.File {
		name := filepath.ToSlash(file.Name)
		parts := strings.Split(strings.Trim(name, "/"), "/")
		if len(parts) < 2 || parts[0] == "" {
			continue
		}
		if root == "" {
			root = parts[0]
		}
		if parts[0] != root || strings.Contains(name, "..") {
			continue
		}
		rel := filepath.FromSlash(strings.Join(parts[1:], "/"))
		outPath := filepath.Join(temp, rel)
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(outPath, 0755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
			return err
		}
		in, err := file.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(outPath)
		if err == nil {
			_, err = io.Copy(out, in)
			_ = out.Close()
		}
		_ = in.Close()
		if err != nil {
			return err
		}
	}
	if _, err := os.Stat(filepath.Join(temp, "main.py")); err != nil {
		return fmt.Errorf("ComfyUI 压缩包中没有 main.py")
	}
	backup := ""
	if update {
		if _, err := os.Stat(target); err == nil {
			backup = target + ".backup-" + time.Now().UTC().Format("20060102-150405")
			if err := os.Rename(target, backup); err != nil {
				return fmt.Errorf("备份旧 ComfyUI 失败：%w", err)
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	if err := os.Rename(temp, target); err != nil {
		if backup != "" {
			_ = os.Rename(backup, target)
		}
		return fmt.Errorf("安装 ComfyUI 失败：%w", err)
	}
	// temp is now the installed target; do not remove it in the deferred cleanup.
	temp = ""
	return nil
}

func installComfyGGUFNode(ctx context.Context, root string) error {
	target := filepath.Join(root, "custom_nodes", "ComfyUI-GGUF")
	if _, err := os.Stat(filepath.Join(target, "nodes.py")); err == nil {
		return nil
	}
	resp, err := (&http.Client{Timeout: 2 * time.Minute}).Do(func() *http.Request {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://codeload.github.com/city96/ComfyUI-GGUF/zip/refs/heads/main", nil)
		return req
	}())
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("HTTP %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 100<<20))
	if err != nil {
		return err
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	temp, err := os.MkdirTemp(filepath.Join(root, "custom_nodes"), ".modeldock-gguf-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	base := ""
	for _, file := range reader.File {
		parts := strings.Split(strings.Trim(filepath.ToSlash(file.Name), "/"), "/")
		if len(parts) < 2 || strings.Contains(file.Name, "..") {
			continue
		}
		if base == "" {
			base = parts[0]
		}
		if parts[0] != base {
			continue
		}
		outPath := filepath.Join(temp, filepath.FromSlash(strings.Join(parts[1:], "/")))
		if file.FileInfo().IsDir() {
			if err = os.MkdirAll(outPath, 0755); err != nil {
				return err
			}
			continue
		}
		if err = os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
			return err
		}
		in, openErr := file.Open()
		if openErr != nil {
			return openErr
		}
		out, createErr := os.Create(outPath)
		if createErr == nil {
			_, createErr = io.Copy(out, in)
			_ = out.Close()
		}
		_ = in.Close()
		if createErr != nil {
			return createErr
		}
	}
	if _, err = os.Stat(filepath.Join(temp, "nodes.py")); err != nil {
		return errors.New("压缩包中没有 nodes.py")
	}
	if _, err = os.Stat(target); err == nil {
		_ = os.RemoveAll(target)
	}
	if err = os.Rename(temp, target); err != nil {
		return err
	}
	temp = ""
	return nil
}

func addComfyModelPath(root, modelPath string) error {
	modelPath, err := filepath.Abs(modelPath)
	if err != nil {
		return err
	}
	config := filepath.Join(root, "extra_model_paths.yaml")
	data, _ := os.ReadFile(config)
	marker := "# ModelDock managed model path"
	if strings.Contains(string(data), marker) {
		return nil
	}
	entry := fmt.Sprintf("\n%s\nmodeldock:\n  base_path: %s\n  unet: .\n  diffusion_models: .\n  clip: .\n  text_encoders: .\n  vae: .\n", marker, yamlPath(modelPath))
	return os.WriteFile(config, append(data, []byte(entry)...), 0644)
}

func yamlPath(path string) string {
	return strings.ReplaceAll(strings.ReplaceAll(path, "\\", "/"), "\"", "\\\"")
}
func writeJSON(w http.ResponseWriter, v any) {
	writeJSONStatus(w, http.StatusOK, APIResponse{Success: true, Data: v})
}
func writeJSONStatus(w http.ResponseWriter, status int, response APIResponse) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response)
}
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"status": "ok", "service": "modeldock"})
}

func (s *Server) ollamaURL(ctx context.Context) string {
	value := strings.TrimSpace(s.DB.Setting(ctx, "ollama.base_url", "http://127.0.0.1:11434"))
	return strings.TrimRight(value, "/")
}

func (s *Server) ollama(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var in struct {
			BaseURL string `json:"baseURL"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil || strings.TrimSpace(in.BaseURL) == "" {
			http.Error(w, "baseURL is required", http.StatusBadRequest)
			return
		}
		u, err := url.Parse(strings.TrimRight(strings.TrimSpace(in.BaseURL), "/"))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			http.Error(w, "Ollama 地址必须是 http 或 https URL", http.StatusBadRequest)
			return
		}
		if err = s.DB.SetSetting(r.Context(), "ollama.base_url", strings.TrimRight(in.BaseURL, "/")); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	base := s.ollamaURL(r.Context())
	models, err := (source.Ollama{BaseURL: base}).Search(r.Context(), "")
	result := map[string]any{"baseURL": base, "connected": err == nil, "models": models}
	if err != nil {
		result["error"] = fmt.Sprintf("无法连接 Ollama：%v", err)
	}
	writeJSON(w, result)
}

func (s *Server) ollamaImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil || strings.TrimSpace(in.Name) == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	m, err := (source.Ollama{BaseURL: s.ollamaURL(r.Context())}).Manifest(r.Context(), strings.TrimSpace(in.Name))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	m.Managed = true
	if err = s.DB.SaveModel(r.Context(), m); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, m)
}
func (s *Server) models(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		v, e := s.DB.Models(r.Context())
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		writeJSON(w, v)
	case http.MethodPost:
		var m model.Manifest
		if e := json.NewDecoder(r.Body).Decode(&m); e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		if m.ID == "" || m.Name == "" {
			http.Error(w, "id and name are required", 400)
			return
		}
		if e := s.DB.SaveModel(r.Context(), m); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		writeJSON(w, m)
	default:
		w.WriteHeader(405)
	}
}
func (s *Server) model(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/models/")
	if r.Method == http.MethodPost && strings.HasSuffix(id, "/test") {
		id = strings.TrimSuffix(id, "/test")
		m, e := s.DB.Model(r.Context(), id)
		if e != nil {
			http.Error(w, e.Error(), 404)
			return
		}
		ok, detail := verifyLocalModel(m)
		m.Available = ok
		m.LastScanned = time.Now().UTC()
		_ = s.DB.SaveModel(r.Context(), m)
		status := "unavailable"
		if ok {
			status = "available"
		}
		writeJSON(w, map[string]any{"status": status, "name": m.Name, "path": m.LocalPath, "detail": detail})
		return
	}
	if r.Method == http.MethodDelete {
		m, e := s.DB.Model(r.Context(), id)
		if e != nil {
			http.Error(w, e.Error(), 404)
			return
		}
		if r.URL.Query().Get("deleteFiles") == "true" && m.LocalPath != "" {
			if !m.Managed {
				http.Error(w, "只有已纳入管理的模型才能由 ModelDock 删除文件", 403)
				return
			}
			if err := deleteManagedPath(s, m.LocalPath); err != nil {
				http.Error(w, err.Error(), 409)
				return
			}
		}
		if e := s.DB.DeleteModel(r.Context(), id); e != nil {
			http.Error(w, e.Error(), 404)
			return
		}
		w.WriteHeader(204)
		return
	}
	if r.Method == http.MethodPost && strings.HasSuffix(id, "/download") {
		id = strings.TrimSuffix(id, "/download")
		m, e := s.DB.Model(r.Context(), id)
		if e != nil {
			http.Error(w, e.Error(), 404)
			return
		}
		t, e := s.Downloads.Start(context.Background(), m)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		writeJSON(w, t)
		return
	}
	http.Error(w, "method not allowed", 405)
}

func deleteManagedPath(s *Server, path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	roots, err := s.DB.StorageRoots(context.Background())
	if err != nil {
		return err
	}
	allowed := false
	for _, root := range roots {
		rp, e := filepath.Abs(root.Path)
		if e == nil && (abs == rp || strings.HasPrefix(abs, strings.TrimRight(rp, string(filepath.Separator))+string(filepath.Separator))) {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("模型路径不在已配置的存储目录内")
	}
	if abs == string(filepath.Separator) || abs == "." {
		return fmt.Errorf("拒绝删除危险路径")
	}
	return os.RemoveAll(abs)
}

func verifyLocalModel(m model.Manifest) (bool, string) {
	if m.LocalPath == "" {
		return false, "没有记录本地文件夹"
	}
	st, err := os.Stat(m.LocalPath)
	if err != nil {
		return false, "找不到模型文件夹"
	}
	if !st.IsDir() {
		return false, "模型路径不是文件夹"
	}
	if len(m.Files) == 0 {
		return false, "模型没有文件清单"
	}
	checked := 0
	for _, file := range m.Files {
		p := file.Path
		if !filepath.IsAbs(p) {
			p = filepath.Join(m.LocalPath, p)
		}
		fi, e := os.Stat(p)
		if e == nil && !fi.IsDir() {
			if file.Size > 0 && fi.Size() != file.Size {
				return false, "模型文件大小与记录不一致"
			}
			checked++
			continue
		}
		if fi, e = os.Stat(filepath.Join(m.LocalPath, file.Path)); e == nil && fi.IsDir() {
			checked++
			continue
		}
		if strings.HasPrefix(m.ID, "local:") && checked == 0 { // Scanned model records store the model directory as the first file path.
			if fi, e = os.Stat(m.LocalPath); e == nil && fi.IsDir() {
				checked++
				continue
			}
		}
		return false, "缺少文件：" + filepath.Base(file.Path)
	}
	if checked == 0 {
		return false, "没有找到可用的模型文件"
	}
	return true, fmt.Sprintf("已检查 %d 项模型文件", checked)
}

func (s *Server) storageMigrate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	var in struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil || in.From == "" || in.To == "" {
		http.Error(w, "from and to are required", 400)
		return
	}
	from, err := filepath.Abs(in.From)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	to, err := filepath.Abs(in.To)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if from == to || strings.HasPrefix(to, strings.TrimRight(from, string(filepath.Separator))+string(filepath.Separator)) {
		http.Error(w, "目标目录不能与来源相同或位于来源目录之内", 400)
		return
	}
	models, err := s.DB.Models(r.Context())
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if err = os.MkdirAll(to, 0755); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	moved := []string{}
	rollback := func() {
		for _, p := range moved {
			_ = os.RemoveAll(p)
		}
	}
	for _, m := range models {
		if m.LocalPath == "" || (m.LocalPath != from && !strings.HasPrefix(m.LocalPath, strings.TrimRight(from, string(filepath.Separator))+string(filepath.Separator))) {
			continue
		}
		rel, e := filepath.Rel(from, m.LocalPath)
		if e != nil {
			rollback()
			http.Error(w, e.Error(), 500)
			return
		}
		dst := filepath.Join(to, rel)
		if _, e = os.Stat(dst); e == nil {
			rollback()
			http.Error(w, "目标位置已存在模型："+rel, 409)
			return
		}
		if e = copyModelTree(m.LocalPath, dst); e != nil {
			rollback()
			http.Error(w, e.Error(), 500)
			return
		}
		moved = append(moved, dst)
	}
	if err = s.DB.RelocateModels(r.Context(), from, to); err != nil {
		rollback()
		http.Error(w, err.Error(), 500)
		return
	}
	_ = s.DB.SaveStorageRoot(r.Context(), db.StorageRoot{ID: db.StorageRootID(to), Path: to, Name: filepath.Base(to), Default: true, Available: true})
	roots, _ := s.DB.StorageRoots(r.Context())
	for _, root := range roots {
		if root.Path == from {
			_ = s.DB.DeleteStorageRoot(r.Context(), root.ID)
		}
	}
	_ = s.DB.SetSetting(r.Context(), "storage.root", to)
	s.Downloads.SetRoot(to)
	writeJSON(w, map[string]any{"status": "migrated", "models": len(moved), "from": from, "to": to})
}

func copyModelTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("不支持迁移特殊文件：%s", path)
		}
		if err = os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, in)
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		copied, err := os.Stat(target)
		if err != nil {
			return err
		}
		if copied.Size() != info.Size() {
			return fmt.Errorf("迁移校验失败：%s", path)
		}
		return nil
	})
}
func (s *Server) tasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(405)
		return
	}
	v, e := s.DB.Tasks(r.Context())
	if e != nil {
		http.Error(w, e.Error(), 500)
		return
	}
	for i := range v {
		if m, err := s.DB.Model(r.Context(), v[i].ModelID); err == nil {
			v[i].ModelName = m.Name
		}
	}
	writeJSON(w, v)
}

func (s *Server) task(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/tasks/"), "/"), "/")
	if len(parts) < 1 || parts[0] == "" {
		http.Error(w, "task id required", 400)
		return
	}
	id := parts[0]
	if r.Method == http.MethodGet && len(parts) == 2 && parts[1] == "events" {
		if _, e := s.DB.Task(r.Context(), id); e != nil {
			http.Error(w, e.Error(), 404)
			return
		}
		events, e := s.DB.TaskEvents(r.Context(), id)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		writeJSON(w, events)
		return
	}
	if r.Method == http.MethodDelete && len(parts) == 1 {
		t, e := s.DB.Task(r.Context(), id)
		if e != nil {
			http.Error(w, e.Error(), 404)
			return
		}
		if slices.Contains([]string{"queued", "resolving", "downloading", "verifying", "retrying"}, t.Status) {
			http.Error(w, "请先暂停或取消下载任务", 409)
			return
		}
		if e = s.DB.DeleteTask(r.Context(), id); e != nil {
			http.Error(w, e.Error(), 404)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method == http.MethodPost && len(parts) == 2 {
		switch parts[1] {
		case "pause", "cancel", "resume":
			if parts[1] == "resume" {
				t, e := s.Downloads.Resume(r.Context(), id)
				if e != nil {
					http.Error(w, e.Error(), 409)
					return
				}
				writeJSON(w, t)
				return
			}
			status := map[string]string{"pause": "paused", "cancel": "cancelled"}[parts[1]]
			if !s.Downloads.Stop(id, status) {
				if e := s.DB.UpdateTaskStatus(r.Context(), id, status); e != nil {
					http.Error(w, e.Error(), 404)
					return
				}
				kind, detail := "paused", "用户暂停了下载任务"
				if status == "cancelled" {
					kind, detail = "cancelled", "用户取消了下载任务"
				}
				_ = s.DB.AddTaskEvent(r.Context(), id, kind, detail)
			}
			writeJSON(w, map[string]string{"status": parts[1]})
			return
		}
	}
	t, e := s.DB.Task(r.Context(), id)
	if e != nil {
		http.Error(w, e.Error(), 404)
		return
	}
	writeJSON(w, t)
}
func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	src := r.URL.Query().Get("source")
	if q == "" {
		http.Error(w, "q is required", 400)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	v, e := source.Search(ctx, src, q)
	if e != nil {
		http.Error(w, "模型源搜索失败，请检查网络连接或稍后重试", 502)
		return
	}
	writeJSON(w, v)
}
func (s *Server) manifest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	var in struct {
		Source        string `json:"source"`
		ID            string `json:"id"`
		Quantization  string `json:"quantization"`
		IncludeMMProj bool   `json:"includeMmproj"`
		InspectOnly   bool   `json:"inspectOnly"`
	}
	if e := json.NewDecoder(r.Body).Decode(&in); e != nil || in.ID == "" {
		http.Error(w, "source and id are required", 400)
		return
	}
	m, e := source.Manifest(r.Context(), in.Source, in.ID)
	if e != nil {
		http.Error(w, e.Error(), 502)
		return
	}
	if strings.TrimSpace(in.Quantization) != "" {
		m, e = selectGGUFQuantization(m, in.Quantization, in.IncludeMMProj)
		if e != nil {
			http.Error(w, e.Error(), http.StatusUnprocessableEntity)
			return
		}
	}
	if !in.InspectOnly {
		if e = s.DB.SaveModel(r.Context(), m); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
	}
	writeJSON(w, m)
}

func selectGGUFQuantization(m model.Manifest, quantization string, includeMMProj bool) (model.Manifest, error) {
	if m.Format != "gguf" {
		return m, fmt.Errorf("所选仓库不是 GGUF 格式，无法按量化版本下载")
	}
	quant := strings.ToLower(strings.TrimSpace(quantization))
	selected := make([]model.File, 0, 1)
	for _, file := range m.Files {
		name := strings.ToLower(filepath.Base(file.Path))
		if ggufFileMatchesQuantization(name, quant) && !strings.Contains(name, "mmproj") {
			selected = append(selected, file)
		}
	}
	if len(selected) != 1 {
		return m, fmt.Errorf("仓库中没有唯一匹配的 GGUF 量化文件，请重新选择版本")
	}
	if includeMMProj {
		var projection *model.File
		for i := range m.Files {
			name := strings.ToLower(filepath.Base(m.Files[i].Path))
			if !strings.Contains(name, "mmproj") || !strings.HasSuffix(name, ".gguf") {
				continue
			}
			if strings.Contains(name, "q8_0") {
				projection = &m.Files[i]
				break
			}
			if projection == nil || strings.Contains(name, "f16") {
				projection = &m.Files[i]
			}
		}
		if projection == nil {
			return m, fmt.Errorf("所选仓库没有可用的 mmproj 多模态投影文件")
		}
		selected = append(selected, *projection)
	}
	m.Files = selected
	m.Quant = strings.ToUpper(quantization)
	m.ID = m.ID + "-gguf-" + strings.ToLower(quant)
	m.Name = m.Name + " · " + strings.ToUpper(quantization)
	return m, nil
}

func ggufFileMatchesQuantization(name, quant string) bool {
	name = strings.TrimSuffix(strings.ToLower(name), ".gguf")
	quant = strings.ToLower(strings.TrimSpace(quant))
	if quant == "" || !strings.HasSuffix(name, quant) {
		return false
	}
	if len(name) == len(quant) {
		return true
	}
	separator := name[len(name)-len(quant)-1]
	return separator == '.' || separator == '-' || separator == '_'
}

func (s *Server) detectHost(ctx context.Context) host.Capability {
	if path := strings.TrimSpace(s.DB.Setting(ctx, "runtime.llama_cpp.path", "")); path != "" {
		_ = os.Setenv("MODELDOCK_LLAMA_CPP_PATH", path)
	}
	mode := strings.TrimSpace(s.DB.Setting(ctx, "runtime.llama_cpp.mode", "cuda"))
	_ = os.Setenv("MODELDOCK_LLAMA_CPP_MODE", mode)
	if path := strings.TrimSpace(s.DB.Setting(ctx, "runtime.comfyui.path", "")); path != "" {
		_ = os.Setenv("MODELDOCK_COMFYUI_PATH", path)
	}
	return host.Detect(ctx)
}
func (s *Server) host(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.detectHost(r.Context()))
}
func (s *Server) recommend(w http.ResponseWriter, r *http.Request) {
	ms, e := s.DB.Models(r.Context())
	if e != nil {
		http.Error(w, e.Error(), 500)
		return
	}
	h := s.detectHost(r.Context())
	out := make([]recommend.Result, 0, len(ms))
	for _, m := range ms {
		out = append(out, recommend.One(m, h))
	}
	writeJSON(w, out)
}
func (s *Server) catalog(w http.ResponseWriter, r *http.Request) { writeJSON(w, catalog.Popular()) }
func (s *Server) storage(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, map[string]any{"root": s.Downloads.Root, "roots": s.storageRootDetails(r)})
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	var in struct {
		Root string `json:"root"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil || in.Root == "" {
		http.Error(w, "root is required", 400)
		return
	}
	root, e := filepath.Abs(in.Root)
	if e != nil {
		http.Error(w, e.Error(), 400)
		return
	}
	if e = os.MkdirAll(root, 0755); e != nil {
		http.Error(w, e.Error(), 400)
		return
	}
	s.Downloads.SetRoot(root)
	if e = s.DB.SetSetting(r.Context(), "storage.root", root); e != nil {
		http.Error(w, e.Error(), 500)
		return
	}
	_ = s.DB.SaveStorageRoot(r.Context(), db.StorageRoot{ID: "default", Path: root, Name: filepath.Base(root), Default: true, Available: true})
	writeJSON(w, map[string]string{"root": root})
}
func (s *Server) storageRoots(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		roots, e := s.DB.StorageRoots(r.Context())
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		_ = roots
		writeJSON(w, s.storageRootDetails(r))
	case http.MethodPost:
		var in struct {
			Path    string `json:"path"`
			Name    string `json:"name"`
			Default bool   `json:"default"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil || in.Path == "" {
			http.Error(w, "path is required", 400)
			return
		}
		p, e := filepath.Abs(in.Path)
		if e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		if e = os.MkdirAll(p, 0755); e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		id := db.StorageRootID(p)
		r0 := db.StorageRoot{ID: id, Path: p, Name: in.Name, Default: in.Default, Available: true}
		if r0.Name == "" {
			r0.Name = filepath.Base(p)
		}
		if e = s.DB.SaveStorageRoot(r.Context(), r0); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		writeJSON(w, r0)
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "id is required", 400)
			return
		}
		roots, e := s.DB.StorageRoots(r.Context())
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		for _, root := range roots {
			if root.ID == id && root.Default {
				http.Error(w, "默认目录不能移除，请先切换默认目录", 409)
				return
			}
		}
		if e = s.DB.DeleteStorageRoot(r.Context(), id); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(405)
	}
}

func (s *Server) storageRootDetails(r *http.Request) []map[string]any {
	roots, err := s.DB.StorageRoots(r.Context())
	if err != nil {
		return []map[string]any{}
	}
	out := make([]map[string]any, 0, len(roots))
	for _, root := range roots {
		item := map[string]any{"id": root.ID, "path": root.Path, "name": root.Name, "default": root.Default, "available": root.Available, "lastScanned": root.LastScanned}
		if stat, err := os.Stat(root.Path); err == nil && stat.IsDir() {
			item["available"] = true
			item["totalBytes"], item["freeBytes"] = diskSpace(root.Path)
		} else {
			item["available"] = false
		}
		item["modelCount"], _ = s.DB.ModelCountUnderRoot(r.Context(), root.Path)
		out = append(out, item)
	}
	return out
}
func (s *Server) generate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	var in struct {
		ModelID        string               `json:"modelId"`
		Prompt         string               `json:"prompt"`
		ImagePath      string               `json:"imagePath"`
		Options        runtime.ImageOptions `json:"options"`
		ConversationID string               `json:"conversationId"`
		Messages       []ChatMessage        `json:"messages"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil || in.ModelID == "" || in.Prompt == "" {
		http.Error(w, "modelId and prompt are required", 400)
		return
	}
	m, e := s.DB.Model(r.Context(), in.ModelID)
	if e != nil {
		http.Error(w, "model not found", 404)
		return
	}
	if !m.Managed {
		writeJSON(w, map[string]any{"status": "not_ready", "message": "模型尚未纳入管理，请先在本地模型页面完成纳入管理", "runtime": map[string]string{"status": "needs_setup"}})
		return
	}
	if !m.Available {
		writeJSON(w, map[string]any{"status": "not_ready", "message": "模型文件当前不可用，请先扫描模型目录或检查存储磁盘", "runtime": map[string]string{"status": "needs_setup"}})
		return
	}
	reg := s.Runtimes
	if len(reg.Adapters()) == 0 {
		reg = runtime.NewRegistry()
	}
	plan := reg.Plan(r.Context(), m, s.detectHost(r.Context()))
	if plan.Status != runtime.Ready {
		writeJSON(w, map[string]any{"status": "not_ready", "message": plan.Reason, "runtime": plan})
		return
	}
	now := time.Now().UTC()
	if in.ConversationID == "" {
		in.ConversationID = fmt.Sprintf("conversation-%d", now.UnixNano())
	}
	if len(in.Messages) == 0 {
		in.Messages = []ChatMessage{{Role: "user", Content: in.Prompt}}
	}
	s.ConversationMu.Lock()
	if s.Conversations == nil {
		s.Conversations = map[string][]ChatMessage{}
	}
	s.Conversations[in.ConversationID] = append([]ChatMessage(nil), in.Messages...)
	s.ConversationMu.Unlock()
	id := fmt.Sprintf("generation-%d", now.UnixNano())
	job := &GenerationJob{ID: id, ModelID: in.ModelID, ConversationID: in.ConversationID, Status: "running", Phase: "analyzing", Message: "已接收请求，正在分析模型能力", StartedAt: now, Steps: []GenerationStep{{Key: "analyzing", Label: "分析需求", Status: "running", StartedAt: now}, {Key: "planning", Label: "规划运行方案", Status: "pending"}, {Key: "loading", Label: "加载模型", Status: "pending"}, {Key: "acting", Label: "执行生成", Status: "pending"}, {Key: "testing", Label: "测试结果", Status: "pending"}}}
	s.GenerationMu.Lock()
	if s.Generations == nil {
		s.Generations = map[string]*GenerationJob{}
	}
	s.Generations[id] = job
	s.GenerationMu.Unlock()
	chatMessages := make([]runtime.Message, len(in.Messages))
	for i, msg := range in.Messages {
		chatMessages[i] = runtime.Message{Role: msg.Role, Content: msg.Content}
	}
	go s.runGeneration(job, m, reg, plan, runtime.Input{Prompt: in.Prompt, ImagePath: in.ImagePath, Options: in.Options, Messages: chatMessages})
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, job)
}

func (s *Server) runGeneration(job *GenerationJob, m model.Manifest, reg runtime.Registry, plan runtime.Plan, input runtime.Input) {
	update := func(phase, message string) {
		s.GenerationMu.Lock()
		defer s.GenerationMu.Unlock()
		now := time.Now().UTC()
		for i := range job.Steps {
			if job.Steps[i].Key == job.Phase && job.Steps[i].Status == "running" {
				job.Steps[i].Status = "done"
				job.Steps[i].FinishedAt = now
			}
			if job.Steps[i].Key == phase {
				if job.Steps[i].StartedAt.IsZero() {
					job.Steps[i].StartedAt = now
				}
				job.Steps[i].Status = "running"
			}
		}
		job.Phase = phase
		job.Message = message
	}
	update("planning", "已选择 "+plan.Label+"，准备调用本地运行时")
	update("loading", "正在加载模型和运行环境")
	var answer string
	var err error
	if plan.Adapter == "ollama" || m.Source == "ollama" || m.Format == "ollama" {
		payload, _ := json.Marshal(map[string]any{"model": m.Name, "messages": input.Messages, "stream": false})
		req, _ := http.NewRequest(http.MethodPost, s.ollamaURL(context.Background())+"/api/chat", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		client := &http.Client{Timeout: 0}
		resp, e := client.Do(req)
		if e != nil {
			err = e
		} else {
			defer resp.Body.Close()
			if resp.StatusCode/100 != 2 {
				err = fmt.Errorf("Ollama 返回 %s", resp.Status)
			} else {
				var out struct {
					Message ChatMessage `json:"message"`
				}
				err = json.NewDecoder(resp.Body).Decode(&out)
				answer = out.Message.Content
				s.ConversationMu.Lock()
				s.Conversations[job.ConversationID] = append(s.Conversations[job.ConversationID], ChatMessage{Role: "assistant", Content: answer})
				s.ConversationMu.Unlock()
			}
		}
	} else {
		update("acting", "运行 "+plan.Label)
		answer, err = reg.GenerateInput(context.Background(), m, input)
	}
	if err != nil {
		s.GenerationMu.Lock()
		job.Status = "failed"
		job.Error = err.Error()
		job.Message = "运行失败"
		job.FinishedAt = time.Now().UTC()
		for i := range job.Steps {
			if job.Steps[i].Status == "running" {
				job.Steps[i].Status = "error"
				job.Steps[i].FinishedAt = job.FinishedAt
			}
		}
		s.GenerationMu.Unlock()
		return
	}
	update("testing", "检查生成结果并准备返回")
	if isArtifactPath(answer) {
		info, statErr := os.Stat(answer)
		if statErr != nil || info.IsDir() || info.Size() == 0 {
			if statErr == nil {
				statErr = fmt.Errorf("生成文件为空")
			}
			s.failGeneration(job, fmt.Errorf("结果文件不可用：%w", statErr))
			return
		}
	}
	s.GenerationMu.Lock()
	job.Status = "completed"
	job.Phase = "completed"
	job.Message = "生成完成，结果已通过检查"
	job.Response = answer
	if isArtifactPath(answer) {
		job.ArtifactName = filepath.Base(answer)
		if info, e := os.Stat(answer); e == nil {
			job.ArtifactBytes = info.Size()
		}
		job.ArtifactURL = "/api/artifacts/" + url.PathEscape(filepath.Base(answer))
	}
	job.FinishedAt = time.Now().UTC()
	for i := range job.Steps {
		if job.Steps[i].Status == "running" {
			job.Steps[i].Status = "done"
			job.Steps[i].FinishedAt = job.FinishedAt
		}
	}
	s.GenerationMu.Unlock()
}

func isArtifactPath(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".png") || strings.HasSuffix(lower, ".jpg") || strings.HasSuffix(lower, ".jpeg") || strings.HasSuffix(lower, ".webp") || strings.HasSuffix(lower, ".mp4") || strings.HasSuffix(lower, ".mov")
}

func (s *Server) failGeneration(job *GenerationJob, err error) {
	s.GenerationMu.Lock()
	defer s.GenerationMu.Unlock()
	job.Status = "failed"
	job.Phase = "failed"
	job.Error = err.Error()
	job.Message = "结果检查失败"
	job.FinishedAt = time.Now().UTC()
	for i := range job.Steps {
		if job.Steps[i].Status == "running" {
			job.Steps[i].Status = "error"
			job.Steps[i].FinishedAt = job.FinishedAt
		}
	}
}

func (s *Server) generationJob(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/generate/jobs/"), "/")
	s.GenerationMu.RLock()
	job := s.Generations[id]
	if job != nil {
		copy := *job
		copy.Steps = append([]GenerationStep(nil), job.Steps...)
		job = &copy
	}
	s.GenerationMu.RUnlock()
	if job == nil {
		http.Error(w, "generation job not found", 404)
		return
	}
	writeJSON(w, job)
}

func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "无法读取上传文件", 400)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "请选择图片文件", 400)
		return
	}
	defer file.Close()
	cache, err := os.UserCacheDir()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	dir := filepath.Join(cache, "ModelDock", "inputs")
	if err = os.MkdirAll(dir, 0755); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	ext := strings.ToLower(filepath.Ext(header.Filename))
	allowed := map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".webp": true}
	if !allowed[ext] {
		http.Error(w, "只支持 PNG、JPG、JPEG 和 WebP 图片", 400)
		return
	}
	path := filepath.Join(dir, fmt.Sprintf("input-%d%s", time.Now().UnixNano(), ext))
	out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer out.Close()
	if _, err = io.Copy(out, file); err != nil {
		_ = os.Remove(path)
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, map[string]string{"path": path})
}

func (s *Server) artifact(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(strings.TrimPrefix(r.URL.Path, "/api/artifacts/"))
	if name == "." || name == "" || strings.Contains(name, "..") {
		http.Error(w, "invalid artifact", 400)
		return
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	path := filepath.Join(cache, "ModelDock", "outputs", name)
	if _, err = os.Stat(path); err != nil {
		http.Error(w, "artifact not found", 404)
		return
	}
	http.ServeFile(w, r, path)
}
func (s *Server) runtimePlan(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ModelID string `json:"modelId"`
	}
	if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&in) != nil || in.ModelID == "" {
		http.Error(w, "modelId is required", 400)
		return
	}
	m, e := s.DB.Model(r.Context(), in.ModelID)
	if e != nil {
		http.Error(w, "model not found", 404)
		return
	}
	reg := s.Runtimes
	if len(reg.Adapters()) == 0 {
		reg = runtime.NewRegistry()
	}
	writeJSON(w, reg.Plan(r.Context(), m, s.detectHost(r.Context())))
}

func (s *Server) runtime(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		root := s.DB.Setting(r.Context(), "runtime.root", runtime.ConfiguredRoot())
		writeJSON(w, runtimeDirectoryStatus(root))
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		Root string `json:"root"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil || strings.TrimSpace(in.Root) == "" {
		http.Error(w, "runtime root is required", http.StatusBadRequest)
		return
	}
	root, err := filepath.Abs(strings.TrimSpace(in.Root))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err = os.MkdirAll(root, 0755); err != nil {
		http.Error(w, fmt.Sprintf("无法创建运行时目录：%v", err), http.StatusBadRequest)
		return
	}
	if err = s.DB.SetSetting(r.Context(), "runtime.root", root); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = os.Setenv("MODELDOCK_RUNTIME_ROOT", root)
	writeJSON(w, runtimeDirectoryStatus(root))
}

type runtimeEngine struct {
	Name       string `json:"name"`
	Installed  bool   `json:"installed"`
	Path       string `json:"path,omitempty"`
	Version    string `json:"version,omitempty"`
	Mode       string `json:"mode"`
	APIPort    int    `json:"apiPort"`
	Source     string `json:"source"`
	Managed    bool   `json:"managed,omitempty"`
	Healthy    bool   `json:"healthy,omitempty"`
	AutoUpdate bool   `json:"autoUpdate,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

func (s *Server) runtimeEngines(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	path := s.DB.Setting(ctx, "runtime.llama_cpp.path", "")
	mode := s.DB.Setting(ctx, "runtime.llama_cpp.mode", "cuda")
	portText := s.DB.Setting(ctx, "runtime.llama_cpp.api_port", "8686")
	port := 8686
	if _, err := fmt.Sscanf(portText, "%d", &port); err != nil || port < 1 || port > 65535 {
		port = 8686
	}
	if r.Method == http.MethodPost {
		var in struct {
			Path            string `json:"path"`
			Mode            string `json:"mode"`
			APIPort         int    `json:"apiPort"`
			ComfyPath       string `json:"comfyPath"`
			ComfyMode       string `json:"comfyMode"`
			ComfyPort       int    `json:"comfyPort"`
			ComfyAutoUpdate bool   `json:"comfyAutoUpdate"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil {
			http.Error(w, "运行时配置格式无效", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(in.Path) != "" {
			p, err := filepath.Abs(strings.TrimSpace(in.Path))
			if err != nil {
				http.Error(w, "llama.cpp 路径无效", 400)
				return
			}
			if _, err = os.Stat(p); err != nil {
				http.Error(w, "找不到 llama.cpp 可执行文件或目录", 400)
				return
			}
			path = p
			_ = s.DB.SetSetting(ctx, "runtime.llama_cpp.path", path)
			_ = os.Setenv("MODELDOCK_LLAMA_CPP_PATH", path)
		}
		if in.Mode == "cpu" || in.Mode == "cuda" {
			mode = in.Mode
			_ = s.DB.SetSetting(ctx, "runtime.llama_cpp.mode", mode)
		}
		if in.APIPort >= 1 && in.APIPort <= 65535 {
			port = in.APIPort
			_ = s.DB.SetSetting(ctx, "runtime.llama_cpp.api_port", fmt.Sprint(port))
		}
		if strings.TrimSpace(in.ComfyPath) != "" {
			p, err := filepath.Abs(strings.TrimSpace(in.ComfyPath))
			if err != nil {
				http.Error(w, "ComfyUI 路径无效", 400)
				return
			}
			if info, statErr := os.Stat(p); statErr != nil || !info.IsDir() {
				http.Error(w, "找不到 ComfyUI 目录", 400)
				return
			}
			_ = s.DB.SetSetting(ctx, "runtime.comfyui.path", p)
		}
		if in.ComfyMode == "managed" || in.ComfyMode == "external" {
			_ = s.DB.SetSetting(ctx, "runtime.comfyui.mode", in.ComfyMode)
		}
		if in.ComfyPort >= 1 && in.ComfyPort <= 65535 {
			_ = s.DB.SetSetting(ctx, "runtime.comfyui.api_port", fmt.Sprint(in.ComfyPort))
		}
		_ = s.DB.SetSetting(ctx, "runtime.comfyui.auto_update", fmt.Sprint(in.ComfyAutoUpdate))
	}
	llama := discoverLlamaEngine(path, s.DB.Setting(ctx, "runtime.root", runtime.ConfiguredRoot()))
	llama.Mode, llama.APIPort = mode, port
	engines := []runtimeEngine{llama}
	comfyMode := s.DB.Setting(ctx, "runtime.comfyui.mode", "external")
	comfyPath := s.DB.Setting(ctx, "runtime.comfyui.path", "")
	if comfyMode == "managed" && strings.TrimSpace(comfyPath) == "" {
		root := s.DB.Setting(ctx, "runtime.root", runtime.ConfiguredRoot())
		if strings.TrimSpace(root) != "" {
			comfyPath = filepath.Join(root, "comfyui")
		}
	}
	comfy := discoverComfyEngine(comfyPath, comfyMode, s.DB.Setting(ctx, "runtime.comfyui.api_port", "8188"), s.DB.Setting(ctx, "runtime.comfyui.auto_update", "false"))
	engines = append(engines, comfy)
	for _, detected := range s.detectHost(ctx).Runtimes {
		if detected.Name == "llama.cpp" || detected.Name == "comfyui" {
			continue
		}
		engines = append(engines, runtimeEngine{
			Name: detected.Name, Installed: detected.Installed, Path: detected.Path,
			Version: detected.Version, Mode: "系统探测", APIPort: 0,
			Source: "主机探测",
		})
	}
	writeJSON(w, map[string]any{"engines": engines})
}

func discoverComfyEngine(configured, mode, portText, autoUpdate string) runtimeEngine {
	port := 8188
	if _, err := fmt.Sscanf(portText, "%d", &port); err != nil || port < 1 || port > 65535 {
		port = 8188
	}
	e := runtimeEngine{Name: "comfyui", Mode: mode, APIPort: port, Path: configured, Source: "未发现", Managed: mode == "managed", AutoUpdate: strings.EqualFold(autoUpdate, "true")}
	candidates := []string{}
	if strings.TrimSpace(configured) != "" {
		candidates = append(candidates, configured)
	}
	if root, err := os.UserConfigDir(); err == nil {
		candidates = append(candidates, filepath.Join(root, "ModelDock", "runtimes", "comfyui"))
	}
	if root := runtime.ConfiguredRoot(); root != "" {
		candidates = append(candidates, filepath.Join(root, "comfyui"))
	}
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err != nil || !info.IsDir() {
			continue
		}
		main := filepath.Join(candidate, "main.py")
		if _, err = os.Stat(main); err != nil {
			continue
		}
		e.Installed, e.Path, e.Source = true, candidate, "已配置"
		if e.Managed && configured == "" {
			e.Source = "ModelDock 运行时目录"
		}
		e.Healthy = comfyHealth(port)
		e.Detail = "已找到 ComfyUI；" + map[bool]string{true: "服务已连接", false: "服务未启动"}[e.Healthy]
		return e
	}
	if stdruntime.GOOS == "darwin" {
		for _, app := range []string{"/Applications/Comfy Desktop.app", filepath.Join(os.Getenv("HOME"), "Applications", "Comfy Desktop.app")} {
			if info, err := os.Stat(app); err == nil && info.IsDir() {
				e.Installed, e.Path, e.Source = true, app, "Comfy Desktop"
				e.Healthy = comfyHealth(port)
				e.Detail = "已找到 Comfy Desktop；" + map[bool]string{true: "服务已连接", false: "请打开 Comfy Desktop 启动服务"}[e.Healthy]
				return e
			}
		}
	}
	if p, err := exec.LookPath("comfy"); err == nil {
		e.Installed, e.Path, e.Source = true, p, "PATH"
		e.Healthy = comfyHealth(port)
		e.Detail = "已找到命令行入口；" + map[bool]string{true: "服务已连接", false: "服务未启动"}[e.Healthy]
	}
	return e
}

func comfyHealth(port int) bool {
	client := &http.Client{Timeout: 1200 * time.Millisecond}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/system_stats", port))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode/100 == 2
}

func discoverLlamaEngine(configured, root string) runtimeEngine {
	e := runtimeEngine{Name: "llama.cpp", Mode: "cuda", Source: "未发现"}
	candidates := []string{}
	if configured != "" {
		candidates = append(candidates, configured)
	}
	if p, err := os.UserConfigDir(); err == nil {
		candidates = append(candidates, filepath.Join(p, "ModelDock", "runtimes", "llama.cpp"))
	}
	if root != "" {
		candidates = append(candidates, filepath.Join(root, "llama.cpp"))
	}
	for _, name := range []string{"llama-server", "llama-cli", "llama-run"} {
		if p, err := exec.LookPath(name); err == nil {
			candidates = append(candidates, p)
		}
		if stdruntime.GOOS == "windows" {
			if p, err := exec.LookPath(name + ".exe"); err == nil {
				candidates = append(candidates, p)
			}
		}
	}
	for _, candidate := range candidates {
		p := candidate
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			p = filepath.Join(p, "llama-server")
			if stdruntime.GOOS == "windows" {
				p += ".exe"
			}
		}
		info, err := os.Stat(p)
		if err != nil || info.IsDir() {
			continue
		}
		e.Installed, e.Path, e.Source = true, p, "已配置"
		if out, err := exec.Command(p, "--version").CombinedOutput(); err == nil {
			e.Version = strings.TrimSpace(string(out))
			if len(e.Version) > 120 {
				e.Version = e.Version[:120]
			}
		}
		return e
	}
	return e
}

func runtimeDirectoryStatus(root string) map[string]any {
	entries := make([]map[string]any, 0, 4)
	for _, name := range []string{"mlx", "ollama", "llama.cpp", "comfyui"} {
		_, err := os.Stat(filepath.Join(root, name))
		entries = append(entries, map[string]any{"name": name, "installed": err == nil})
	}
	_, err := os.Stat(root)
	return map[string]any{"root": root, "available": err == nil, "runtimes": entries}
}
func (s *Server) localScan(w http.ResponseWriter, r *http.Request) {
	root := s.Downloads.Root
	if v := r.URL.Query().Get("root"); v != "" {
		root = v
	}
	v, e := localmodels.Scan(root)
	if e != nil {
		http.Error(w, e.Error(), 500)
		return
	}
	writeJSON(w, v)
}
func (s *Server) localScanAll(w http.ResponseWriter, r *http.Request) {
	roots, e := s.DB.StorageRoots(r.Context())
	if e != nil {
		http.Error(w, e.Error(), 500)
		return
	}
	all := []localmodels.Candidate{}
	for _, root := range roots {
		if _, statErr := os.Stat(root.Path); statErr != nil {
			root.Available = false
			_ = s.DB.SaveStorageRoot(r.Context(), root)
			continue
		}
		v, scanErr := localmodels.Scan(root.Path)
		if scanErr != nil {
			http.Error(w, scanErr.Error(), 500)
			return
		}
		seen := make([]string, 0, len(v))
		for _, c := range v {
			seen = append(seen, c.Path)
			m0, e0 := s.DB.Model(r.Context(), "local:"+c.Path)
			c.Managed = e0 == nil && m0.Managed
			all = append(all, c)
			m := localmodels.ToManifest(c)
			m.Managed = c.Managed
			m.LocalPath = c.Path
			m.Available = true
			m.SizeBytes = c.Size
			m.LastScanned = time.Now().UTC()
			_ = s.DB.SaveModel(r.Context(), m)
		}
		if e = s.DB.MarkRootModelsUnavailable(r.Context(), root.Path, seen); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		root.LastScanned = time.Now().UTC()
		root.Available = true
		_ = s.DB.SaveStorageRoot(r.Context(), root)
	}
	writeJSON(w, all)
}

func (s *Server) localUnimport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	var in struct {
		ID   string `json:"id"`
		Path string `json:"path"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		http.Error(w, "id or path is required", 400)
		return
	}
	id := in.ID
	if id == "" && in.Path != "" {
		id = "local:" + in.Path
	}
	if id == "" {
		http.Error(w, "id or path is required", 400)
		return
	}
	if e := s.DB.SetModelManaged(r.Context(), id, false); e != nil {
		http.Error(w, e.Error(), 404)
		return
	}
	writeJSON(w, map[string]string{"status": "unmanaged", "id": id})
}
func (s *Server) localImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	var c localmodels.Candidate
	if json.NewDecoder(r.Body).Decode(&c) != nil || c.Path == "" {
		http.Error(w, "path is required", 400)
		return
	}
	m := localmodels.ToManifest(c)
	m.Managed = true
	m.LocalPath = c.Path
	m.Available = true
	m.SizeBytes = c.Size
	m.LastScanned = time.Now().UTC()
	if e := s.DB.SaveModel(r.Context(), m); e != nil {
		http.Error(w, e.Error(), 500)
		return
	}
	writeJSON(w, map[string]string{"id": m.ID, "name": m.Name, "status": "imported"})
}
