package config

import (
	"os"
	"path/filepath"
	"testing"
)

// New installs use the Toskar folder and toskar.db; an install from before
// the rename keeps its Yggdrasil folder and yggdrasil.db, with nothing
// moved (#237).
func TestDataDirAndDatabaseKeepAnExistingInstall(t *testing.T) {
	root := t.TempDir()
	newDir, oldDir := filepath.Join(root, "Toskar"), filepath.Join(root, "Yggdrasil")

	if got := chooseDataDir(newDir, oldDir); got != newDir {
		t.Fatalf("fresh install: %s", got)
	}
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := chooseDataDir(newDir, oldDir); got != oldDir {
		t.Fatalf("existing install: %s", got)
	}
	// An empty Toskar folder, such as one a log or installer made, doesn't
	// hide the old folder's data.
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := chooseDataDir(newDir, oldDir); got != oldDir {
		t.Fatalf("empty new folder: %s", got)
	}
	if err := os.WriteFile(filepath.Join(newDir, "config.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := chooseDataDir(newDir, oldDir); got != newDir {
		t.Fatalf("new folder in use: %s", got)
	}

	if got := DBPathIn(oldDir); got != filepath.Join(oldDir, "toskar.db") {
		t.Fatalf("no database yet: %s", got)
	}
	if err := os.WriteFile(filepath.Join(oldDir, "yggdrasil.db"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := DBPathIn(oldDir); got != filepath.Join(oldDir, "yggdrasil.db") {
		t.Fatalf("existing database: %s", got)
	}
	// A data directory from before the rename opens with its database.
	m, err := NewManager(oldDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Get().DBPath; got != filepath.Join(oldDir, "yggdrasil.db") {
		t.Fatalf("manager database: %s", got)
	}
}
