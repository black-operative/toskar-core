package config

import (
	"os"
	"path/filepath"
	"testing"
)

// A config.json that does not name its data directory belongs to the one it
// is in. Falling back to the default would point a test or second daemon at
// the user's real database.
func TestConfigWithoutDataDirStaysInItsDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"api_port": 27331}`), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := m.Get()
	if cfg.DataDir != dir || cfg.DBPath != filepath.Join(dir, "toskar.db") || cfg.ModelsDir != filepath.Join(dir, "models") {
		t.Fatalf("config points outside %s: data %s, db %s, models %s", dir, cfg.DataDir, cfg.DBPath, cfg.ModelsDir)
	}
	if cfg.APIPort != 27331 {
		t.Fatalf("api_port = %d", cfg.APIPort)
	}
}
