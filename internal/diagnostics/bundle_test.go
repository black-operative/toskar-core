package diagnostics

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yeixio/toskar-core/internal/config"
)

func TestWriteBundleExcludesSecrets(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.DataDir = dir
	cfg.LogsDir = filepath.Join(dir, "logs")
	cfg.ModelsDir = filepath.Join(dir, "models")
	cfg.RuntimesDir = filepath.Join(dir, "runtimes")
	cfg.DBPath = filepath.Join(dir, "yggdrasil.db")
	cfg.NodeID = "test-node"
	cfg.NodeName = "Test"
	_ = os.MkdirAll(cfg.LogsDir, 0o755)
	_ = os.WriteFile(filepath.Join(cfg.LogsDir, "daemon.log"), []byte("hello"), 0o644)
	_ = os.WriteFile(filepath.Join(cfg.LogsDir, "secret.key"), []byte("nope"), 0o600)

	out := filepath.Join(dir, "bundle.zip")
	if err := WriteBundle(out, Options{Config: cfg, HardwareJSON: []byte(`{"os":"test"}`)}); err != nil {
		t.Fatalf("write: %v", err)
	}
	st, err := os.Stat(out)
	if err != nil || st.Size() == 0 {
		t.Fatalf("expected non-empty bundle")
	}
}
