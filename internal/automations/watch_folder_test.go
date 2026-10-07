package automations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A folder counts as changed when a file came, went, or changed, and the
// run gets the new and changed text files' contents (#204).
func TestCheckFolder(t *testing.T) {
	home := withHome(t)
	dir := filepath.Join(home, "Invoices")
	write := func(name, body string) {
		t.Helper()
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("march.txt", "Total: 120")
	write("old.csv", "a,b")
	write(".hidden/secret.txt", "skip me")
	found, state, err := CheckFolder("~/Invoices", nil)
	if err != nil || found.Changed {
		t.Fatalf("first check = %+v, %v", found, err)
	}
	if found, state, _ = CheckFolder("~/Invoices", state); found.Changed {
		t.Fatal("an unchanged folder counted as a change")
	}

	write("2026/april.txt", "Total: 340")
	write("march.txt", "Total: 125, corrected")
	future := time.Now().Add(time.Minute)
	_ = os.Chtimes(filepath.Join(dir, "march.txt"), future, future)
	_ = os.Remove(filepath.Join(dir, "old.csv"))
	write("scan.pdf", "%PDF\x00binary")
	write(".hidden/secret.txt", "changed, still skipped")
	found, _, err = CheckFolder("~/Invoices", state)
	if err != nil || !found.Changed {
		t.Fatalf("change = %+v, %v", found, err)
	}
	for _, want := range []string{"Added: 2026/april.txt", "Added: scan.pdf", "Changed: march.txt", "Removed: old.csv", "--- 2026/april.txt ---\nTotal: 340", "Total: 125, corrected"} {
		if !strings.Contains(found.Summary, want) {
			t.Errorf("summary lacks %q:\n%s", want, found.Summary)
		}
	}
	for _, unwanted := range []string{"secret", "binary"} {
		if strings.Contains(found.Summary, unwanted) {
			t.Errorf("summary has %q:\n%s", unwanted, found.Summary)
		}
	}
}

// One file can be watched too, and only in the home folder.
func TestCheckFolderOneFileInHome(t *testing.T) {
	home := withHome(t)
	path := filepath.Join(home, "notes.md")
	_ = os.WriteFile(path, []byte("one"), 0o644)
	_, state, err := CheckFolder("~/notes.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(path, []byte("one and two"), 0o644)
	if found, _, _ := CheckFolder("~/notes.md", state); !found.Changed || !strings.Contains(found.Summary, "one and two") {
		t.Fatalf("file change = %+v", found)
	}
	if _, _, err := CheckFolder("/etc", nil); err == nil {
		t.Fatal("watched a folder outside the home folder")
	}
	if (&Trigger{Kind: TriggerFolder, Path: "/etc"}).Validate() == nil || (&Trigger{Kind: TriggerFolder}).Validate() == nil {
		t.Fatal("accepted a folder trigger outside the home folder, or without a path")
	}
}
