package automations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func withHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	saved := homeDir
	homeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { homeDir = saved })
	return home
}

// A save folder is a full path in the home folder (#204).
func TestSaveFolderPath(t *testing.T) {
	home := withHome(t)
	for in, want := range map[string]string{
		"~/Documents/Toskar":                filepath.Join(home, "Documents", "Toskar"),
		filepath.Join(home, "Notes"):        filepath.Join(home, "Notes"),
		filepath.Join(home, "a", "..", "b"): filepath.Join(home, "b"),
		"":                                  "",
	} {
		if got, err := SaveFolderPath(in); err != nil || got != want {
			t.Errorf("%q: %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"Documents", "/etc", filepath.Join(home, "..", "other"), "~/../other"} {
		if _, err := SaveFolderPath(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestSaveResult(t *testing.T) {
	withHome(t)
	a := Automation{Name: "News: AI/ML", SaveFolder: "~/Toskar", Schedule: Schedule{TimeZone: "America/Juneau"}}
	at := time.Date(2026, 10, 7, 16, 30, 0, 0, time.UTC)
	first, err := SaveResult(a, at, "Three stories today.")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(first) != "2026-10-07 08.30 News- AI-ML.md" {
		t.Fatalf("name = %q", filepath.Base(first))
	}
	body, _ := os.ReadFile(first)
	if !strings.HasPrefix(string(body), "# News: AI/ML\n\n2026-10-07 08:30 AKDT\n\nThree stories today.") {
		t.Fatalf("body = %q", body)
	}
	// Run now in the same minute keeps both.
	second, err := SaveResult(a, at, "Again.")
	if err != nil || filepath.Base(second) != "2026-10-07 08.30 News- AI-ML (2).md" {
		t.Fatalf("second = %q, %v", second, err)
	}
}
