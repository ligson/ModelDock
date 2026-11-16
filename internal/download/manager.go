package download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/modeldock/modeldock/internal/db"
	"github.com/modeldock/modeldock/internal/model"
)

type Manager struct {
	DB           *db.DB
	Root         string
	Client       *http.Client
	StallTimeout time.Duration
	mu           sync.Mutex
	jobs         map[string]context.CancelFunc
	stopStatus   map[string]string
}

func (m *Manager) SetRoot(root string) { m.mu.Lock(); m.Root = root; m.mu.Unlock() }

func (m *Manager) Start(ctx context.Context, manifest model.Manifest) (model.Task, error) {
	now := time.Now().UTC()
	t := model.Task{ID: fmt.Sprintf("task-%d", now.UnixNano()), ModelID: manifest.ID, Status: "queued", CreatedAt: now, UpdatedAt: now}
	for _, f := range manifest.Files {
		t.BytesTotal += f.Size
	}
	if err := m.DB.SaveTask(ctx, t); err != nil {
		return t, err
	}
	return t, m.launch(manifest, t)
}

func (m *Manager) Resume(ctx context.Context, id string) (model.Task, error) {
	t, err := m.DB.Task(ctx, id)
	if err != nil {
		return t, err
	}
	if t.Status != "paused" && t.Status != "cancelled" && t.Status != "failed" && t.Status != "stalled" {
		return t, fmt.Errorf("task is %s and cannot be resumed", t.Status)
	}
	manifest, err := m.DB.Model(ctx, t.ModelID)
	if err != nil {
		return t, err
	}
	t.Status, t.Error, t.FailureKind = "queued", "", ""
	t.UpdatedAt = time.Now().UTC()
	if err = m.DB.SaveTask(ctx, t); err != nil {
		return t, err
	}
	return t, m.launch(manifest, t)
}

func (m *Manager) launch(manifest model.Manifest, t model.Task) error {
	m.mu.Lock()
	if m.jobs == nil {
		m.jobs = make(map[string]context.CancelFunc)
	}
	if m.stopStatus == nil {
		m.stopStatus = make(map[string]string)
	}
	if m.jobs[t.ID] != nil {
		m.mu.Unlock()
		return fmt.Errorf("task %s is already running", t.ID)
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.jobs[t.ID] = cancel
	m.mu.Unlock()
	go func() {
		defer func() { m.mu.Lock(); delete(m.jobs, t.ID); delete(m.stopStatus, t.ID); m.mu.Unlock() }()
		m.run(ctx, t, manifest)
	}()
	return nil
}

func (m *Manager) Stop(id, status string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cancel := m.jobs[id]; cancel != nil {
		m.stopStatus[id] = status
		cancel()
		return true
	}
	return false
}
func (m *Manager) Cancel(id string) bool { return m.Stop(id, "cancelled") }

// RecoverInterruptedTasks turns process-bound states into resumable tasks. Partial files remain on disk.
func (m *Manager) RecoverInterruptedTasks(ctx context.Context) error {
	tasks, err := m.DB.Tasks(ctx)
	if err != nil {
		return err
	}
	for _, t := range tasks {
		if t.Status == "queued" || t.Status == "downloading" || t.Status == "verifying" || t.Status == "retrying" {
			if err = m.DB.UpdateTaskStatus(ctx, t.ID, "paused"); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *Manager) client() *http.Client {
	if m.Client != nil {
		return m.Client
	}
	return &http.Client{Timeout: 0}
}

func (m *Manager) run(ctx context.Context, t model.Task, manifest model.Manifest) {
	t.Status, t.Error, t.FailureKind = "downloading", "", ""
	_ = m.DB.SaveTask(context.Background(), t)
	root := m.Root
	dir := filepath.Join(root, "objects", safeSegment(manifest.ID))
	if err := os.MkdirAll(dir, 0755); err != nil {
		m.fail(t, err)
		return
	}
	var completed int64
	for _, f := range manifest.Files {
		p, e := safePath(dir, f.Path)
		if e == nil {
			if st, e := os.Stat(p); e == nil && !st.IsDir() && (f.Size == 0 || st.Size() == f.Size) {
				completed += st.Size()
			}
		}
	}
	t.BytesDone = completed
	for _, f := range manifest.Files {
		if err := ctx.Err(); err != nil {
			m.stop(t)
			return
		}
		path, err := safePath(dir, f.Path)
		if err != nil {
			m.fail(t, err)
			return
		}
		if st, e := os.Stat(path); e == nil && !st.IsDir() && (f.Size == 0 || st.Size() == f.Size) {
			if f.SHA256 != "" {
				ok, e := hashFile(path, f.SHA256)
				if e != nil || !ok {
					_ = os.Remove(path)
				} else {
					continue
				}
			} else {
				continue
			}
		}
		if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			m.fail(t, err)
			return
		}
		t.CurrentFile = f.Path
		t.UpdatedAt = time.Now().UTC()
		_ = m.DB.SaveTask(context.Background(), t)
		base := t.BytesDone
		got, err := m.file(ctx, path, f, func(n, speed int64) {
			t.BytesDone = base + n
			t.SpeedBPS = speed
			if speed > 0 && t.BytesTotal > t.BytesDone {
				t.ETASeconds = (t.BytesTotal - t.BytesDone) / speed
			} else {
				t.ETASeconds = 0
			}
			t.UpdatedAt = time.Now().UTC()
			_ = m.DB.SaveTask(context.Background(), t)
		})
		if err != nil {
			if ctx.Err() != nil {
				m.stop(t)
			} else {
				m.fail(t, err)
			}
			return
		}
		t.BytesDone = base + got
	}
	t.Status, t.CurrentFile, t.SpeedBPS, t.ETASeconds = "verifying", "", 0, 0
	t.UpdatedAt = time.Now().UTC()
	_ = m.DB.SaveTask(context.Background(), t)
	for _, f := range manifest.Files {
		p, err := safePath(dir, f.Path)
		if err != nil {
			m.fail(t, err)
			return
		}
		st, err := os.Stat(p)
		if err != nil || st.IsDir() || (f.Size > 0 && st.Size() != f.Size) {
			m.fail(t, fmt.Errorf("file verification failed: %s", f.Path))
			return
		}
		if f.SHA256 != "" {
			ok, e := hashFile(p, f.SHA256)
			if e != nil || !ok {
				m.fail(t, fmt.Errorf("sha256 mismatch: %s", f.Path))
				return
			}
		}
	}
	t.Status, t.CurrentFile, t.Error, t.FailureKind = "installed", "", "", ""
	t.UpdatedAt = time.Now().UTC()
	_ = m.DB.SaveTask(context.Background(), t)
	if saved, err := m.DB.Model(context.Background(), manifest.ID); err == nil {
		saved.LocalPath = dir
		saved.Available = true
		saved.SizeBytes = t.BytesDone
		saved.LastScanned = time.Now().UTC()
		saved.Managed = true
		_ = m.DB.SaveModel(context.Background(), saved)
	}
}

func (m *Manager) file(ctx context.Context, path string, f model.File, progress func(int64, int64)) (int64, error) {
	if f.URL == "" {
		return 0, errors.New("download URL is empty")
	}
	part := path + ".part"
	for attempt := 0; attempt < 4; attempt++ {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		var offset int64
		if st, err := os.Stat(part); err == nil {
			offset = st.Size()
		}
		if f.Size > 0 && offset == f.Size {
			if f.SHA256 != "" {
				ok, err := hashFile(part, f.SHA256)
				if err != nil {
					return offset, err
				}
				if !ok {
					return offset, errors.New("sha256 mismatch")
				}
			}
			if err := os.Rename(part, path); err != nil {
				return offset, err
			}
			return offset, nil
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.URL, nil)
		if err != nil {
			return offset, err
		}
		if offset > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
		}
		resp, err := m.client().Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return offset, ctx.Err()
			}
			if attempt < 3 {
				if wait(ctx, time.Duration(attempt+1)*time.Second) != nil {
					return offset, ctx.Err()
				}
				continue
			}
			return offset, err
		}
		if offset > 0 && resp.StatusCode == http.StatusOK { // Server ignored Range; restart instead of corrupting the partial file.
			offset = 0
		}
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
			status := resp.Status
			resp.Body.Close()
			return offset, fmt.Errorf("http %s", status)
		}
		if resp.StatusCode == http.StatusPartialContent && offset == 0 && !strings.HasPrefix(resp.Header.Get("Content-Range"), "bytes 0-") {
			resp.Body.Close()
			return offset, errors.New("unexpected partial response")
		}
		if resp.StatusCode == http.StatusPartialContent && offset > 0 && !strings.HasPrefix(resp.Header.Get("Content-Range"), fmt.Sprintf("bytes %d-", offset)) {
			resp.Body.Close()
			return offset, errors.New("unexpected range response")
		}
		flags := os.O_CREATE | os.O_WRONLY
		if offset == 0 {
			flags |= os.O_TRUNC
		}
		out, err := os.OpenFile(part, flags, 0644)
		if err != nil {
			resp.Body.Close()
			return offset, err
		}
		if offset > 0 {
			_, err = out.Seek(offset, io.SeekStart)
		}
		if progress != nil {
			progress(offset, 0)
		}
		lastUpdate, lastBytes := time.Now(), offset
		stallTimeout := m.StallTimeout
		if stallTimeout <= 0 {
			stallTimeout = 120 * time.Second
		}
		var stalledMu sync.Mutex
		stalled := false
		stallTimer := time.AfterFunc(stallTimeout, func() { stalledMu.Lock(); stalled = true; stalledMu.Unlock(); _ = resp.Body.Close() })
		buf := make([]byte, 256*1024)
		for err == nil {
			if e := ctx.Err(); e != nil {
				err = e
				break
			}
			n, readErr := resp.Body.Read(buf)
			if n > 0 {
				stallTimer.Reset(stallTimeout)
				wn, e := out.Write(buf[:n])
				offset += int64(wn)
				if e != nil {
					err = e
					break
				}
				if wn != n {
					err = io.ErrShortWrite
					break
				}
			}
			if time.Since(lastUpdate) >= 500*time.Millisecond || readErr != nil {
				elapsed := time.Since(lastUpdate).Seconds()
				speed := int64(float64(offset-lastBytes) / max(elapsed, .001))
				tNow := time.Now()
				_ = speed
				if progress != nil {
					progress(offset, speed)
				}
				lastBytes, lastUpdate = offset, tNow
			}
			if readErr != nil {
				if readErr != io.EOF {
					err = readErr
				}
				break
			}
		}
		stallTimer.Stop()
		closeErr := out.Close()
		resp.Body.Close()
		if err == nil && closeErr != nil {
			err = closeErr
		}
		if err != nil {
			stalledMu.Lock()
			wasStalled := stalled
			stalledMu.Unlock()
			if wasStalled {
				err = errors.New("stalled: no bytes received before timeout")
			}
			if ctx.Err() != nil {
				return offset, ctx.Err()
			}
			if attempt < 3 {
				if wait(ctx, time.Duration(attempt+1)*time.Second) != nil {
					return offset, ctx.Err()
				}
				continue
			}
			return offset, err
		}
		if f.Size > 0 && offset != f.Size {
			if attempt < 3 {
				continue
			}
			return offset, fmt.Errorf("size mismatch: expected %d bytes, received %d", f.Size, offset)
		}
		if f.SHA256 != "" {
			ok, e := hashFile(part, f.SHA256)
			if e != nil {
				return offset, e
			}
			if !ok {
				return offset, errors.New("sha256 mismatch")
			}
		}
		if err = os.Rename(part, path); err != nil {
			return offset, err
		}
		if progress != nil {
			progress(offset, 0)
		}
		return offset, nil
	}
	return 0, errors.New("download retries exhausted")
}

func (m *Manager) pause(t model.Task) {
	t.Status, t.Error = "paused", "任务已暂停，可继续下载"
	t.SpeedBPS, t.ETASeconds = 0, 0
	t.UpdatedAt = time.Now().UTC()
	_ = m.DB.SaveTask(context.Background(), t)
}
func (m *Manager) stop(t model.Task) {
	m.mu.Lock()
	status := m.stopStatus[t.ID]
	m.mu.Unlock()
	if status != "cancelled" {
		status = "paused"
	}
	t.Status = status
	if status == "cancelled" {
		t.Error = "任务已取消，可继续下载"
	} else {
		t.Error = "任务已暂停，可继续下载"
	}
	t.SpeedBPS, t.ETASeconds = 0, 0
	t.UpdatedAt = time.Now().UTC()
	_ = m.DB.SaveTask(context.Background(), t)
}
func (m *Manager) fail(t model.Task, err error) {
	t.Status, t.Error, t.FailureKind = "failed", err.Error(), classify(err)
	t.SpeedBPS, t.ETASeconds = 0, 0
	t.UpdatedAt = time.Now().UTC()
	_ = m.DB.SaveTask(context.Background(), t)
}

func classify(err error) string {
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	var u *url.Error
	if errors.As(err, &u) {
		var op *net.OpError
		if errors.As(u.Err, &op) {
			if op.Op == "dial" {
				return "network"
			}
		}
		if strings.Contains(strings.ToLower(u.Error()), "tls") {
			return "tls"
		}
		return "network"
	}
	s := strings.ToLower(err.Error())
	switch {
	case strings.Contains(s, "stalled"):
		return "stalled"
	case strings.Contains(s, "http 403"):
		return "http_403"
	case strings.Contains(s, "http 404"):
		return "http_404"
	case strings.Contains(s, "sha256"):
		return "checksum"
	case strings.Contains(s, "no space") || strings.Contains(s, "disk quota"):
		return "disk_full"
	case strings.Contains(s, "tls"):
		return "tls"
	case strings.Contains(s, "proxy"):
		return "proxy"
	case strings.Contains(s, "http "):
		return "http"
	default:
		return "io"
	}
}
func wait(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
func safeSegment(v string) string {
	v = strings.Trim(v, "/\\")
	v = strings.ReplaceAll(v, "..", "_")
	return v
}
func safePath(root, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", errors.New("manifest path must be relative")
	}
	p := filepath.Join(root, filepath.Clean(rel))
	r, err := filepath.Rel(root, p)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", errors.New("manifest path escapes model directory")
	}
	return p, nil
}
func hashFile(path, want string) (bool, error) {
	in, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer in.Close()
	h := sha256.New()
	if _, err = io.Copy(h, in); err != nil {
		return false, err
	}
	return hex.EncodeToString(h.Sum(nil)) == strings.ToLower(want), nil
}
