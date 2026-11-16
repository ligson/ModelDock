package api

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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
	writeJSON(w, v)
}

func (s *Server) task(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/tasks/"), "/"), "/")
	if len(parts) < 1 || parts[0] == "" {
		http.Error(w, "task id required", 400)
		return
	}
	id := parts[0]
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
	v, e := source.Search(r.Context(), src, q)
	if e != nil {
		http.Error(w, e.Error(), 502)
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
		Source string `json:"source"`
		ID     string `json:"id"`
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
	if e = s.DB.SaveModel(r.Context(), m); e != nil {
		http.Error(w, e.Error(), 500)
		return
	}
	writeJSON(w, m)
}
func (s *Server) host(w http.ResponseWriter, r *http.Request) { writeJSON(w, host.Detect(r.Context())) }
func (s *Server) recommend(w http.ResponseWriter, r *http.Request) {
	ms, e := s.DB.Models(r.Context())
	if e != nil {
		http.Error(w, e.Error(), 500)
		return
	}
	h := host.Detect(r.Context())
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
	reg := s.Runtimes
	if len(reg.Adapters()) == 0 {
		reg = runtime.NewRegistry()
	}
	plan := reg.Plan(r.Context(), m, host.Detect(r.Context()))
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
	writeJSON(w, reg.Plan(r.Context(), m, host.Detect(r.Context())))
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
