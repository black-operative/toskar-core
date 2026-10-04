package models

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/yeixio/toskar-core/internal/store"
)

func TestEnsureStubModel(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	catalog, err := NewCatalogEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	storage := NewStorage(db.SQL, filepath.Join(dir, "models"))
	if err := storage.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(catalog, storage, nil, nil)
	if err := mgr.EnsureStubModel(context.Background()); err != nil {
		t.Fatal(err)
	}
	path, err := mgr.Path(context.Background(), StubModelID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	m, err := mgr.Get(context.Background(), StubModelID)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Installed {
		t.Fatal("expected installed")
	}
}
