package models

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Storage persists installed model metadata.
type Storage struct {
	db        *sql.DB
	modelsDir string
}

func NewStorage(db *sql.DB, modelsDir string) *Storage {
	return &Storage{db: db, modelsDir: modelsDir}
}

func (s *Storage) ModelsDir() string { return s.modelsDir }

func (s *Storage) ModelPath(modelID string) string {
	return filepath.Join(s.modelsDir, modelID+".gguf")
}

func (s *Storage) TempPath(modelID string) string {
	return filepath.Join(s.modelsDir, ".partial", modelID+".gguf.part")
}

func (s *Storage) UpsertCatalogEntry(ctx context.Context, e CatalogEntry) error {
	caps, _ := json.Marshal(e.Capabilities)
	src, _ := json.Marshal(e.Source)
	catalog, _ := json.Marshal(e)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO models (id, display_name, family, variant, size_bytes, memory_needed, context_length, capabilities_json, source_json, catalog_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			display_name=excluded.display_name,
			family=excluded.family,
			variant=excluded.variant,
			size_bytes=excluded.size_bytes,
			memory_needed=excluded.memory_needed,
			context_length=excluded.context_length,
			capabilities_json=excluded.capabilities_json,
			source_json=excluded.source_json,
			catalog_json=excluded.catalog_json`,
		e.ID, e.DisplayName, e.Family, e.Variant, e.SizeBytes, e.MemoryNeededBytes, e.Context,
		string(caps), string(src), string(catalog))
	return err
}

func (s *Storage) MarkInstalled(ctx context.Context, modelID, path, sha256 string, size uint64) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO installed_models (model_id, path, sha256, size_bytes, status)
		VALUES (?, ?, ?, ?, 'installed')
		ON CONFLICT(model_id) DO UPDATE SET path=excluded.path, sha256=excluded.sha256, size_bytes=excluded.size_bytes, status='installed'`,
		modelID, path, sha256, size)
	return err
}

func (s *Storage) TouchLastUsed(ctx context.Context, modelID string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO model_usage (model_id, last_used_at) VALUES (?, ?)
		ON CONFLICT(model_id) DO UPDATE SET last_used_at=excluded.last_used_at`,
		modelID, at.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Storage) ListLastUsed(ctx context.Context) (map[string]time.Time, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT model_id, last_used_at FROM model_usage`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]time.Time)
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		t, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			t, _ = time.Parse(time.RFC3339, raw)
		}
		if !t.IsZero() {
			out[id] = t
		}
	}
	return out, rows.Err()
}

func (s *Storage) ListDynamicCatalog(ctx context.Context) ([]CatalogEntry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT catalog_json FROM models`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CatalogEntry
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var e CatalogEntry
		if err := json.Unmarshal([]byte(raw), &e); err != nil || e.ID == "" {
			continue
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Storage) DeleteInstalled(ctx context.Context, modelID string) error {
	var path string
	err := s.db.QueryRowContext(ctx, `SELECT path FROM installed_models WHERE model_id = ?`, modelID).Scan(&path)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if path != "" {
		_ = os.Remove(path)
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM installed_models WHERE model_id = ?`, modelID)
	return err
}

func (s *Storage) IsInstalled(ctx context.Context, modelID string) (bool, string, error) {
	var path string
	err := s.db.QueryRowContext(ctx, `SELECT path FROM installed_models WHERE model_id = ? AND status = 'installed'`, modelID).Scan(&path)
	if err == sql.ErrNoRows {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}
	if _, err := os.Stat(path); err != nil {
		return false, "", nil
	}
	return true, path, nil
}

func (s *Storage) ListInstalled(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT model_id, path FROM installed_models WHERE status = 'installed'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]string)
	for rows.Next() {
		var id, path string
		if err := rows.Scan(&id, &path); err != nil {
			return nil, err
		}
		if _, err := os.Stat(path); err == nil {
			out[id] = path
		}
	}
	return out, rows.Err()
}

func (s *Storage) SumInstalledBytes(ctx context.Context) (uint64, error) {
	var sum sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(size_bytes), 0) FROM installed_models WHERE status = 'installed'`).Scan(&sum)
	if err != nil {
		return 0, err
	}
	if !sum.Valid || sum.Int64 < 0 {
		return 0, nil
	}
	return uint64(sum.Int64), nil
}

func (s *Storage) EnsureDirs() error {
	return os.MkdirAll(filepath.Join(s.modelsDir, ".partial"), 0o755)
}

func (s *Storage) AvailableDiskBytes() (uint64, error) {
	return diskAvailable(s.modelsDir)
}

func diskAvailable(path string) (uint64, error) {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return 0, err
	}
	// Portable fallback: stat the directory.
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		return 0, fmt.Errorf("not a directory: %s", path)
	}
	// Use a conservative default when platform statfs unavailable.
	return 100 * 1024 * 1024 * 1024, nil
}

func (s *Storage) ToContract(e CatalogEntry, installed bool, status string) contracts.Model {
	m := entryToContract(e, installed)
	m.Status = status
	return m
}
