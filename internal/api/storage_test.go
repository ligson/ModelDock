package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/modeldock/modeldock/internal/db"
	"github.com/modeldock/modeldock/internal/download"
	"github.com/modeldock/modeldock/internal/model"
)

func TestStorageMigrationCopiesAndUpdatesIndex(t *testing.T) {
	base := t.TempDir()
	from := filepath.Join(base, "from")
	to := filepath.Join(base, "to")
	modelDir := filepath.Join(from, "objects", "sample")
	if err := os.MkdirAll(modelDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := []byte("weights")
	if err := os.WriteFile(filepath.Join(modelDir, "model.gguf"), content, 0644); err != nil {
		t.Fatal(err)
	}
	database, err := db.Open(filepath.Join(base, "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	m := model.Manifest{ID: "sample", Name: "sample", Source: "local", Format: "gguf", LocalPath: modelDir, Available: true, Managed: true, Files: []model.File{{Path: filepath.Join(modelDir, "model.gguf")}}}
	if err = database.SaveModel(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	if err = database.SaveStorageRoot(context.Background(), db.StorageRoot{ID: db.StorageRootID(from), Path: from, Name: "from", Default: true, Available: true}); err != nil {
		t.Fatal(err)
	}
	dl := &download.Manager{DB: database, Root: from}
	server := &Server{DB: database, Downloads: dl}
	body, _ := json.Marshal(map[string]string{"from": from, "to": to})
	req := httptest.NewRequest(http.MethodPost, "/api/storage/migrate", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("migration status %d: %s", rec.Code, rec.Body.String())
	}
	saved, err := database.Model(context.Background(), m.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(to, "objects", "sample")
	if saved.LocalPath != want {
		t.Fatalf("local path = %q, want %q", saved.LocalPath, want)
	}
	got, err := os.ReadFile(filepath.Join(want, "model.gguf"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Fatal("copied model content differs")
	}
	if dl.Root != to {
		t.Fatalf("download root = %q, want %q", dl.Root, to)
	}
	roots, err := database.StorageRoots(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 || roots[0].Path != to || !roots[0].Default {
		t.Fatalf("unexpected storage roots: %+v", roots)
	}
}
