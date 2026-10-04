package models

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

func TestDownloadIntegrityAndAtomicInstall(t *testing.T) {
	payload := []byte("gguf-model-bytes-for-test")
	sum := sha256.Sum256(payload)
	sha := hex.EncodeToString(sum[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(payload)))
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	modelsDir := filepath.Join(dir, "models")
	storage := NewStorage(db.SQL, modelsDir)
	bus := events.NewBus(8)
	dl := NewDownloader(storage, bus, db.SQL)

	entry := CatalogEntry{
		ID:                "test-model",
		DisplayName:       "Test",
		SizeBytes:         uint64(len(payload)),
		MemoryNeededBytes: 1000,
		Source:            contracts.ModelSource{URL: srv.URL, SHA256: sha},
	}
	if err := storage.UpsertCatalogEntry(context.Background(), entry); err != nil {
		t.Fatal(err)
	}

	if err := dl.Download(context.Background(), entry); err != nil {
		t.Fatal(err)
	}

	installed, path, err := storage.IsInstalled(context.Background(), "test-model")
	if err != nil || !installed {
		t.Fatalf("installed=%v path=%q err=%v", installed, path, err)
	}
	if _, err := os.Stat(storage.TempPath("test-model")); !os.IsNotExist(err) {
		t.Fatal("partial file should not remain")
	}
}
