package download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modeldock/modeldock/internal/db"
	"github.com/modeldock/modeldock/internal/model"
)

func TestDownloadAndPersistModel(t *testing.T) {
	payload := []byte(strings.Repeat("model weights ", 2048))
	sum := sha256.Sum256(payload)
	srv := rangeServer(payload)
	defer srv.Close()
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	m := model.Manifest{ID: "sample", Name: "sample", Source: "test", Format: "gguf", Files: []model.File{{Path: "nested/model.gguf", URL: srv.URL, Size: int64(len(payload)), SHA256: hex.EncodeToString(sum[:])}}}
	if err = database.SaveModel(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	dl := &Manager{DB: database, Root: t.TempDir(), Client: srv.Client(), StallTimeout: time.Second}
	task, err := dl.Start(context.Background(), m)
	if err != nil {
		t.Fatal(err)
	}
	finished := waitTask(t, database, task.ID)
	if finished.Status != "installed" {
		t.Fatalf("task status = %s, error=%s", finished.Status, finished.Error)
	}
	saved, err := database.Model(context.Background(), m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.Available || !saved.Managed || saved.LocalPath == "" {
		t.Fatalf("model not indexed as installed: %+v", saved)
	}
	got, err := os.ReadFile(filepath.Join(saved.LocalPath, "nested", "model.gguf"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatal("downloaded content differs")
	}
}

func TestResumeUsesRangePartialFile(t *testing.T) {
	payload := []byte(strings.Repeat("0123456789", 20000))
	ranged := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHeader := r.Header.Get("Range")
		if strings.HasPrefix(rangeHeader, "bytes=") {
			ranged = true
			start, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(rangeHeader, "bytes="), "-"))
			if start < 0 || start >= len(payload) {
				w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
				return
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(payload)-1, len(payload)))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(payload[start:])
			return
		}
		_, _ = w.Write(payload)
	}))
	defer srv.Close()
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	root := t.TempDir()
	path := filepath.Join(root, "objects", "sample", "model.gguf.part")
	if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, payload[:32768], 0644); err != nil {
		t.Fatal(err)
	}
	m := model.Manifest{ID: "sample", Name: "sample", Source: "test", Format: "gguf", Files: []model.File{{Path: "model.gguf", URL: srv.URL, Size: int64(len(payload))}}}
	_ = database.SaveModel(context.Background(), m)
	dl := &Manager{DB: database, Root: root, Client: srv.Client(), StallTimeout: time.Second}
	task := model.Task{ID: "resume", ModelID: m.ID, Status: "paused", BytesTotal: int64(len(payload)), CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err = database.SaveTask(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if _, err = dl.Resume(context.Background(), task.ID); err != nil {
		t.Fatal(err)
	}
	finished := waitTask(t, database, task.ID)
	if finished.Status != "installed" {
		t.Fatalf("task status = %s, error=%s", finished.Status, finished.Error)
	}
	if !ranged {
		t.Fatal("resume request did not use HTTP Range")
	}
}

func TestChecksumFailureDoesNotInstall(t *testing.T) {
	payload := []byte("corrupt model")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(payload) }))
	defer srv.Close()
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	m := model.Manifest{ID: "bad", Name: "bad", Source: "test", Format: "gguf", Files: []model.File{{Path: "model.gguf", URL: srv.URL, Size: int64(len(payload)), SHA256: strings.Repeat("0", 64)}}}
	_ = database.SaveModel(context.Background(), m)
	dl := &Manager{DB: database, Root: t.TempDir(), Client: srv.Client()}
	task, err := dl.Start(context.Background(), m)
	if err != nil {
		t.Fatal(err)
	}
	finished := waitTask(t, database, task.ID)
	if finished.Status != "failed" || finished.FailureKind != "checksum" {
		t.Fatalf("expected checksum failure, got %+v", finished)
	}
	if _, err = database.Model(context.Background(), m.ID); err != nil {
		t.Fatal(err)
	}
}

func rangeServer(payload []byte) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHeader := r.Header.Get("Range")
		if strings.HasPrefix(rangeHeader, "bytes=") {
			start, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(rangeHeader, "bytes="), "-"))
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(payload)-1, len(payload)))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(payload[start:])
			return
		}
		_, _ = w.Write(payload)
	}))
}

func waitTask(t *testing.T, database *db.DB, id string) model.Task {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		task, err := database.Task(context.Background(), id)
		if err == nil && (task.Status == "installed" || task.Status == "failed" || task.Status == "paused") {
			return task
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("task did not finish before timeout")
	return model.Task{}
}
