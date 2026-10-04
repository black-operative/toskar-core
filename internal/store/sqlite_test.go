package store_test

import (
	"path/filepath"
	"testing"

	"github.com/yeixio/toskar-core/internal/store"
)

func TestOpenMigrates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	var n int
	if err := db.SQL.QueryRow(`SELECT COUNT(1) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatalf("query migrations: %v", err)
	}
	if n < 1 {
		t.Fatalf("expected migrations applied, got %d", n)
	}

	// Re-open should be idempotent.
	db2, err := store.Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db2.Close()
}
