package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/modeldock/modeldock/internal/model"
)

type DB struct{ SQL *sql.DB }

func StorageRootID(path string) string {
	return fmt.Sprintf("root-%x", sha256.Sum256([]byte(path)))[:16]
}

func Open(path string) (*DB, error) {
	d, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	d.SetMaxOpenConns(1)
	if _, err = d.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		d.Close()
		return nil, err
	}
	if err = migrate(d); err != nil {
		d.Close()
		return nil, err
	}
	return &DB{SQL: d}, nil
}

func migrate(d *sql.DB) error {
	_, err := d.Exec(`CREATE TABLE IF NOT EXISTS models (
		id TEXT PRIMARY KEY, name TEXT NOT NULL, parameters TEXT, quantization TEXT,
		source TEXT NOT NULL, format TEXT NOT NULL, task_json TEXT, license TEXT,
		files_json TEXT NOT NULL, runtimes_json TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
		local_path TEXT NOT NULL DEFAULT '', available INTEGER NOT NULL DEFAULT 0, size_bytes INTEGER NOT NULL DEFAULT 0, last_scanned TEXT NOT NULL DEFAULT '', managed INTEGER NOT NULL DEFAULT 0
	);
	CREATE TABLE IF NOT EXISTS tasks (
		id TEXT PRIMARY KEY, model_id TEXT NOT NULL, status TEXT NOT NULL,
		bytes_total INTEGER NOT NULL DEFAULT 0, bytes_done INTEGER NOT NULL DEFAULT 0,
		current_file TEXT, error TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
		failure_kind TEXT NOT NULL DEFAULT '', speed_bps INTEGER NOT NULL DEFAULT 0, eta_seconds INTEGER NOT NULL DEFAULT 0
	);
	CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
	CREATE TABLE IF NOT EXISTS storage_roots (id TEXT PRIMARY KEY, path TEXT UNIQUE NOT NULL, name TEXT NOT NULL, is_default INTEGER NOT NULL DEFAULT 0, available INTEGER NOT NULL DEFAULT 0, last_scanned TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL);`)
	if err != nil {
		return err
	}
	for _, q := range []string{`ALTER TABLE models ADD COLUMN local_path TEXT NOT NULL DEFAULT ''`, `ALTER TABLE models ADD COLUMN available INTEGER NOT NULL DEFAULT 0`, `ALTER TABLE models ADD COLUMN size_bytes INTEGER NOT NULL DEFAULT 0`, `ALTER TABLE models ADD COLUMN last_scanned TEXT NOT NULL DEFAULT ''`, `ALTER TABLE models ADD COLUMN managed INTEGER NOT NULL DEFAULT 0`} {
		_, _ = d.Exec(q)
	}
	for _, q := range []string{`ALTER TABLE tasks ADD COLUMN failure_kind TEXT NOT NULL DEFAULT ''`, `ALTER TABLE tasks ADD COLUMN speed_bps INTEGER NOT NULL DEFAULT 0`, `ALTER TABLE tasks ADD COLUMN eta_seconds INTEGER NOT NULL DEFAULT 0`} {
		_, _ = d.Exec(q)
	}
	return nil
}

func (d *DB) Setting(ctx context.Context, key, fallback string) string {
	var v string
	if d.SQL.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&v) != nil {
		return fallback
	}
	return v
}
func (d *DB) SetSetting(ctx context.Context, key, value string) error {
	_, e := d.SQL.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return e
}

type StorageRoot struct {
	ID          string    `json:"id"`
	Path        string    `json:"path"`
	Name        string    `json:"name"`
	Default     bool      `json:"default"`
	Available   bool      `json:"available"`
	LastScanned time.Time `json:"lastScanned,omitempty"`
}

func (d *DB) StorageRoots(ctx context.Context) ([]StorageRoot, error) {
	rows, e := d.SQL.QueryContext(ctx, `SELECT id,path,name,is_default,available,last_scanned FROM storage_roots ORDER BY is_default DESC,name`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []StorageRoot
	for rows.Next() {
		var r StorageRoot
		var def, av int
		var last string
		if e = rows.Scan(&r.ID, &r.Path, &r.Name, &def, &av, &last); e != nil {
			return nil, e
		}
		r.Default = def != 0
		r.Available = av != 0
		if last != "" {
			r.LastScanned, _ = time.Parse(time.RFC3339Nano, last)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (d *DB) SaveStorageRoot(ctx context.Context, r StorageRoot) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, e := d.SQL.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if r.Default {
		if _, e = tx.ExecContext(ctx, `UPDATE storage_roots SET is_default=0`); e != nil {
			return e
		}
	}
	var existing string
	e = tx.QueryRowContext(ctx, `SELECT id FROM storage_roots WHERE path=?`, r.Path).Scan(&existing)
	if e == nil {
		_, e = tx.ExecContext(ctx, `UPDATE storage_roots SET name=?,is_default=?,available=?,last_scanned=? WHERE id=?`, r.Name, r.Default, r.Available, r.LastScanned.Format(time.RFC3339Nano), existing)
	} else if e == sql.ErrNoRows {
		_, e = tx.ExecContext(ctx, `INSERT INTO storage_roots(id,path,name,is_default,available,last_scanned,created_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET path=excluded.path,name=excluded.name,is_default=excluded.is_default,available=excluded.available,last_scanned=excluded.last_scanned`, r.ID, r.Path, r.Name, r.Default, r.Available, r.LastScanned.Format(time.RFC3339Nano), now)
	}
	if e != nil {
		return e
	}
	return tx.Commit()
}
func (d *DB) DeleteStorageRoot(ctx context.Context, id string) error {
	_, e := d.SQL.ExecContext(ctx, `DELETE FROM storage_roots WHERE id=?`, id)
	return e
}

func (d *DB) ModelCountUnderRoot(ctx context.Context, root string) (int, error) {
	var n int
	prefix := strings.TrimRight(root, `/\\`) + string(filepath.Separator)
	err := d.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM models WHERE substr(local_path,1,length(?))=?`, prefix, prefix).Scan(&n)
	return n, err
}

func (d *DB) RelocateModels(ctx context.Context, from, to string) error {
	from, err := filepath.Abs(from)
	if err != nil {
		return err
	}
	to, err = filepath.Abs(to)
	if err != nil {
		return err
	}
	fromPrefix := strings.TrimRight(from, string(filepath.Separator)) + string(filepath.Separator)
	rows, err := d.SQL.QueryContext(ctx, `SELECT id,local_path,files_json FROM models WHERE local_path<>''`)
	if err != nil {
		return err
	}
	type change struct {
		id, path string
		files    []model.File
	}
	changes := []change{}
	for rows.Next() {
		var id, p, raw string
		if err = rows.Scan(&id, &p, &raw); err != nil {
			rows.Close()
			return err
		}
		if p != from && !strings.HasPrefix(p, fromPrefix) {
			continue
		}
		rel, e := filepath.Rel(from, p)
		if e != nil {
			rows.Close()
			return e
		}
		var files []model.File
		if e = json.Unmarshal([]byte(raw), &files); e != nil {
			rows.Close()
			return e
		}
		for i := range files {
			if filepath.IsAbs(files[i].Path) && (files[i].Path == from || strings.HasPrefix(files[i].Path, fromPrefix)) {
				fRel, e := filepath.Rel(from, files[i].Path)
				if e != nil {
					rows.Close()
					return e
				}
				files[i].Path = filepath.Join(to, fRel)
			} else if !filepath.IsAbs(files[i].Path) {
				files[i].Path = filepath.Join(to, rel, files[i].Path)
			}
		}
		changes = append(changes, change{id: id, path: filepath.Join(to, rel), files: files})
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, c := range changes {
		raw, e := json.Marshal(c.files)
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE models SET local_path=?,files_json=?,updated_at=? WHERE id=?`, c.path, string(raw), time.Now().UTC().Format(time.RFC3339Nano), c.id); e != nil {
			return e
		}
	}
	return tx.Commit()
}

func (d *DB) Close() error { return d.SQL.Close() }

func (d *DB) SaveModel(ctx context.Context, m model.Manifest) error {
	if m.Source == "ollama" {
		// Ollama owns the model files; availability is determined by the Ollama API.
		m.Available = true
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}
	m.UpdatedAt = time.Now().UTC()
	tasks, _ := json.Marshal(m.Task)
	files, _ := json.Marshal(m.Files)
	runtimes, _ := json.Marshal(m.Runtimes)
	_, err := d.SQL.ExecContext(ctx, `INSERT INTO models(id,name,parameters,quantization,source,format,task_json,license,files_json,runtimes_json,created_at,updated_at,local_path,available,size_bytes,last_scanned,managed)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,parameters=excluded.parameters,quantization=excluded.quantization,source=excluded.source,format=excluded.format,task_json=excluded.task_json,license=excluded.license,files_json=excluded.files_json,runtimes_json=excluded.runtimes_json,updated_at=excluded.updated_at,local_path=excluded.local_path,available=excluded.available,size_bytes=excluded.size_bytes,last_scanned=excluded.last_scanned,managed=excluded.managed`,
		m.ID, m.Name, m.Parameters, m.Quant, m.Source, m.Format, string(tasks), m.License, string(files), string(runtimes), m.CreatedAt.Format(time.RFC3339Nano), m.UpdatedAt.Format(time.RFC3339Nano), m.LocalPath, m.Available, m.SizeBytes, m.LastScanned.Format(time.RFC3339Nano), m.Managed)
	return err
}

func scanModel(row interface{ Scan(...any) error }) (model.Manifest, error) {
	var m model.Manifest
	var tasks, files, runtimes, created, updated, lastScanned string
	err := row.Scan(&m.ID, &m.Name, &m.Parameters, &m.Quant, &m.Source, &m.Format, &tasks, &m.License, &files, &runtimes, &created, &updated, &m.LocalPath, &m.Available, &m.SizeBytes, &lastScanned, &m.Managed)
	if err != nil {
		return m, err
	}
	if m.Source == "ollama" {
		m.Available = true
	}
	if err = json.Unmarshal([]byte(tasks), &m.Task); err != nil {
		return m, err
	}
	if err = json.Unmarshal([]byte(files), &m.Files); err != nil {
		return m, err
	}
	if runtimes != "" {
		_ = json.Unmarshal([]byte(runtimes), &m.Runtimes)
	}
	m.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	m.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	if lastScanned != "" {
		m.LastScanned, _ = time.Parse(time.RFC3339Nano, lastScanned)
	}
	return m, nil
}

func (d *DB) Models(ctx context.Context) ([]model.Manifest, error) {
	rows, err := d.SQL.QueryContext(ctx, `SELECT id,name,parameters,quantization,source,format,task_json,license,files_json,runtimes_json,created_at,updated_at,local_path,available,size_bytes,last_scanned,managed FROM models ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]model.Manifest, 0)
	for rows.Next() {
		m, e := scanModel(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
func (d *DB) Model(ctx context.Context, id string) (model.Manifest, error) {
	return scanModel(d.SQL.QueryRowContext(ctx, `SELECT id,name,parameters,quantization,source,format,task_json,license,files_json,runtimes_json,created_at,updated_at,local_path,available,size_bytes,last_scanned,managed FROM models WHERE id=?`, id))
}
func (d *DB) DeleteModel(ctx context.Context, id string) error {
	r, err := d.SQL.ExecContext(ctx, `DELETE FROM models WHERE id=?`, id)
	if err == nil {
		n, _ := r.RowsAffected()
		if n == 0 {
			return fmt.Errorf("model %q not found", id)
		}
	}
	return err
}

func (d *DB) SetModelManaged(ctx context.Context, id string, managed bool) error {
	res, err := d.SQL.ExecContext(ctx, `UPDATE models SET managed=?,updated_at=? WHERE id=?`, managed, time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("model %q not found", id)
	}
	return nil
}

// MarkRootModelsUnavailable keeps the historical index while reflecting files
// that disappeared from a scanned storage root.
func (d *DB) MarkRootModelsUnavailable(ctx context.Context, root string, seen []string) error {
	if len(seen) == 0 {
		_, err := d.SQL.ExecContext(ctx, `UPDATE models SET available=0,updated_at=? WHERE source='local' AND local_path LIKE ?`, time.Now().UTC().Format(time.RFC3339Nano), root+"%")
		return err
	}
	q := `UPDATE models SET available=0,updated_at=? WHERE source='local' AND local_path LIKE ? AND local_path NOT IN (` + strings.Repeat("?,", len(seen)-1) + `?)`
	args := []any{time.Now().UTC().Format(time.RFC3339Nano), root + "%"}
	for _, path := range seen {
		args = append(args, path)
	}
	_, err := d.SQL.ExecContext(ctx, q, args...)
	return err
}
func (d *DB) SaveTask(ctx context.Context, t model.Task) error {
	_, err := d.SQL.ExecContext(ctx, `INSERT INTO tasks(id,model_id,status,bytes_total,bytes_done,current_file,error,created_at,updated_at,failure_kind,speed_bps,eta_seconds) VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=excluded.status,bytes_total=excluded.bytes_total,bytes_done=excluded.bytes_done,current_file=excluded.current_file,error=excluded.error,updated_at=excluded.updated_at,failure_kind=excluded.failure_kind,speed_bps=excluded.speed_bps,eta_seconds=excluded.eta_seconds`, t.ID, t.ModelID, t.Status, t.BytesTotal, t.BytesDone, t.CurrentFile, t.Error, t.CreatedAt.Format(time.RFC3339Nano), t.UpdatedAt.Format(time.RFC3339Nano), t.FailureKind, t.SpeedBPS, t.ETASeconds)
	return err
}
func (d *DB) Tasks(ctx context.Context) ([]model.Task, error) {
	rows, e := d.SQL.QueryContext(ctx, `SELECT id,model_id,status,bytes_total,bytes_done,current_file,error,created_at,updated_at,failure_kind,speed_bps,eta_seconds FROM tasks ORDER BY updated_at DESC`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := make([]model.Task, 0)
	for rows.Next() {
		var t model.Task
		var c, u string
		if e = rows.Scan(&t.ID, &t.ModelID, &t.Status, &t.BytesTotal, &t.BytesDone, &t.CurrentFile, &t.Error, &c, &u, &t.FailureKind, &t.SpeedBPS, &t.ETASeconds); e != nil {
			return nil, e
		}
		t.CreatedAt, _ = time.Parse(time.RFC3339Nano, c)
		t.UpdatedAt, _ = time.Parse(time.RFC3339Nano, u)
		out = append(out, t)
	}
	return out, rows.Err()
}

func (d *DB) UpdateTaskStatus(ctx context.Context, id, status string) error {
	res, err := d.SQL.ExecContext(ctx, `UPDATE tasks SET status=?, updated_at=? WHERE id=?`, status, time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("task %q not found", id)
	}
	return nil
}

func (d *DB) Task(ctx context.Context, id string) (model.Task, error) {
	var t model.Task
	var c, u string
	err := d.SQL.QueryRowContext(ctx, `SELECT id,model_id,status,bytes_total,bytes_done,current_file,error,created_at,updated_at,failure_kind,speed_bps,eta_seconds FROM tasks WHERE id=?`, id).Scan(&t.ID, &t.ModelID, &t.Status, &t.BytesTotal, &t.BytesDone, &t.CurrentFile, &t.Error, &c, &u, &t.FailureKind, &t.SpeedBPS, &t.ETASeconds)
	if err != nil {
		return t, err
	}
	t.CreatedAt, _ = time.Parse(time.RFC3339Nano, c)
	t.UpdatedAt, _ = time.Parse(time.RFC3339Nano, u)
	return t, nil
}
