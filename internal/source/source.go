package source

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/modeldock/modeldock/internal/model"
)

type Result struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Source    string   `json:"source"`
	Downloads int64    `json:"downloads,omitempty"`
	Tags      []string `json:"tags,omitempty"`
}
type Adapter interface {
	Search(context.Context, string) ([]Result, error)
	Manifest(context.Context, string) (model.Manifest, error)
}
type HuggingFace struct{ Client *http.Client }

type ModelScope struct{ Client *http.Client }
type Ollama struct {
	Client  *http.Client
	BaseURL string
}

func httpClient(c *http.Client) *http.Client {
	if c != nil {
		return c
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (h HuggingFace) client() *http.Client {
	return httpClient(h.Client)
}
func (h HuggingFace) Search(ctx context.Context, q string) ([]Result, error) {
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, "https://huggingface.co/api/models?search="+url.QueryEscape(q)+"&limit=30", nil)
	if e != nil {
		return nil, e
	}
	resp, e := h.client().Do(req)
	if e != nil {
		return nil, e
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("hugging face search: %s", resp.Status)
	}
	var raw []struct {
		ID        string   `json:"id"`
		Downloads int64    `json:"downloads"`
		Tags      []string `json:"tags"`
	}
	if e = json.NewDecoder(resp.Body).Decode(&raw); e != nil {
		return nil, e
	}
	out := make([]Result, 0, len(raw))
	for _, x := range raw {
		out = append(out, Result{ID: x.ID, Name: x.ID, Source: "huggingface", Downloads: x.Downloads, Tags: x.Tags})
	}
	return out, nil
}

func (m ModelScope) client() *http.Client { return httpClient(m.Client) }
func (m ModelScope) Search(ctx context.Context, q string) ([]Result, error) {
	u := "https://modelscope.cn/api/v1/models?Name=" + url.QueryEscape(q) + "&PageNumber=1&PageSize=30"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := m.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("modelscope search: %s", resp.Status)
	}
	var raw struct {
		Data struct {
			Models []struct {
				Name string   `json:"Name"`
				Path string   `json:"Path"`
				Tags []string `json:"Tags"`
			} `json:"Models"`
		} `json:"Data"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]Result, 0, len(raw.Data.Models))
	for _, x := range raw.Data.Models {
		id := x.Path
		if id == "" {
			id = x.Name
		}
		out = append(out, Result{ID: id, Name: x.Name, Source: "modelscope", Tags: x.Tags})
	}
	return out, nil
}
func (m ModelScope) Manifest(ctx context.Context, id string) (model.Manifest, error) {
	safe := strings.Trim(id, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://modelscope.cn/api/v1/models/"+safe, nil)
	if err != nil {
		return model.Manifest{}, err
	}
	resp, err := m.client().Do(req)
	if err != nil {
		return model.Manifest{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return model.Manifest{}, fmt.Errorf("modelscope model: %s", resp.Status)
	}
	var raw struct {
		Data struct {
			Name    string   `json:"Name"`
			Path    string   `json:"Path"`
			Tags    []string `json:"Tags"`
			License string   `json:"License"`
			Files   []struct {
				Path   string `json:"Path"`
				Size   int64  `json:"Size"`
				Sha256 string `json:"Sha256"`
			} `json:"Files"`
		} `json:"Data"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return model.Manifest{}, err
	}
	name := raw.Data.Name
	if name == "" {
		name = safe
	}
	mf := model.Manifest{ID: safe, Name: name, Source: "modelscope", Format: "unknown", License: raw.Data.License, CreatedAt: time.Now().UTC()}
	for _, f := range raw.Data.Files {
		if f.Path == "" || strings.HasSuffix(strings.ToLower(f.Path), ".md") {
			continue
		}
		format := strings.ToLower(filepathExt(f.Path))
		if mf.Format == "unknown" {
			switch format {
			case "gguf":
				mf.Format = "gguf"
			case "safetensors":
				mf.Format = "safetensors"
			}
		}
		mf.Files = append(mf.Files, model.File{Path: f.Path, Size: f.Size, SHA256: f.Sha256, URL: "https://modelscope.cn/models/" + safe + "/resolve/master/" + f.Path})
	}
	return mf, nil
}

func (o Ollama) client() *http.Client { return httpClient(o.Client) }
func (o Ollama) base() string {
	if o.BaseURL != "" {
		return strings.TrimRight(o.BaseURL, "/")
	}
	return "http://127.0.0.1:11434"
}
func (o Ollama) Search(ctx context.Context, q string) ([]Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.base()+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := o.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("ollama: %s", resp.Status)
	}
	var raw struct {
		Models []struct {
			Name   string `json:"name"`
			Size   int64  `json:"size"`
			Digest string `json:"digest"`
		} `json:"models"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	out := []Result{}
	for _, x := range raw.Models {
		if q == "" || strings.Contains(strings.ToLower(x.Name), strings.ToLower(q)) {
			out = append(out, Result{ID: x.Name, Name: x.Name, Source: "ollama", Downloads: x.Size, Tags: []string{x.Digest}})
		}
	}
	return out, nil
}
func (o Ollama) Manifest(ctx context.Context, id string) (model.Manifest, error) { // Ollama stores the model outside ModelDock; import as a logical runtime model.
	return model.Manifest{ID: id, Name: id, Source: "ollama", Format: "ollama", Task: []string{"文本"}, Runtimes: []string{"ollama"}, Files: []model.File{{Path: id}}, CreatedAt: time.Now().UTC()}, nil
}
func (h HuggingFace) Manifest(ctx context.Context, id string) (model.Manifest, error) {
	safe := strings.Trim(id, "/")
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, "https://huggingface.co/api/models/"+safe+"?blobs=true", nil)
	if e != nil {
		return model.Manifest{}, e
	}
	resp, e := h.client().Do(req)
	if e != nil {
		return model.Manifest{}, e
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return model.Manifest{}, fmt.Errorf("hugging face model: %s", resp.Status)
	}
	var raw struct {
		ID       string   `json:"id"`
		Tags     []string `json:"tags"`
		License  string   `json:"license"`
		Siblings []struct {
			Rfilename string `json:"rfilename"`
			Size      int64  `json:"size"`
			LFS       *struct {
				Size   int64  `json:"size"`
				SHA256 string `json:"sha256"`
			} `json:"lfs"`
		} `json:"siblings"`
	}
	if e = json.NewDecoder(resp.Body).Decode(&raw); e != nil {
		return model.Manifest{}, e
	}
	m := model.Manifest{ID: raw.ID, Name: raw.ID, Source: "huggingface", Format: "unknown", License: raw.License, CreatedAt: time.Now().UTC()}
	for _, f := range raw.Siblings {
		size := f.Size
		sha := ""
		if f.LFS != nil {
			if f.LFS.Size > size {
				size = f.LFS.Size
			}
			sha = f.LFS.SHA256
		}
		if strings.HasPrefix(f.Rfilename, ".") || strings.HasSuffix(f.Rfilename, ".md") {
			continue
		}
		m.Files = append(m.Files, model.File{Path: f.Rfilename, URL: "https://huggingface.co/" + safe + "/resolve/main/" + f.Rfilename, Size: size, SHA256: sha})
		if m.Format == "unknown" {
			ext := filepathExt(f.Rfilename)
			switch ext {
			case "gguf":
				m.Format = "gguf"
			case "safetensors":
				m.Format = "safetensors"
			case "bin":
				m.Format = "transformers"
			}
		}
	}
	return m, nil
}

func Search(ctx context.Context, sourceName, q string) ([]Result, error) {
	switch strings.ToLower(sourceName) {
	case "", "huggingface", "hf":
		return HuggingFace{}.Search(ctx, q)
	case "modelscope", "ms":
		return ModelScope{}.Search(ctx, q)
	case "ollama":
		return Ollama{}.Search(ctx, q)
	default:
		return nil, fmt.Errorf("source %q is not implemented", sourceName)
	}
}
func Manifest(ctx context.Context, sourceName, id string) (model.Manifest, error) {
	switch strings.ToLower(sourceName) {
	case "", "huggingface", "hf":
		return HuggingFace{}.Manifest(ctx, id)
	case "modelscope", "ms":
		return ModelScope{}.Manifest(ctx, id)
	case "ollama":
		return Ollama{}.Manifest(ctx, id)
	default:
		return model.Manifest{}, fmt.Errorf("source %q is not implemented", sourceName)
	}
}

func filepathExt(path string) string {
	i := strings.LastIndex(path, ".")
	if i < 0 {
		return ""
	}
	return path[i+1:]
}
