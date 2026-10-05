package app

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

// TOSKAR_WEB_FIXTURES makes the daemon's web search read the test pages,
// as the self-hosted quality run does.
func TestWebFixturesReplaceTheWebTools(t *testing.T) {
	path := filepath.Join(t.TempDir(), "web.json")
	if err := os.WriteFile(path, []byte(`{"sites":[{"match":"(?i)bitcoin","results":[{"title":"BTC","url":"https://coins.example/btc","snippet":"64,210"}]}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TOSKAR_WEB_FIXTURES", path)
	t.Setenv("TOSKAR_DISCOVERY_ENABLED", "false")
	a, err := New(Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.DB.Close() })
	tool, err := a.Tools.Get("internet.search")
	if err != nil {
		t.Fatal(err)
	}
	out, err := tool.Execute(context.Background(), map[string]any{"query": "bitcoin price"})
	if err != nil {
		t.Fatal(err)
	}
	if got := out["results"].([]any)[0].(map[string]any)["snippet"]; got != "64,210" {
		t.Fatalf("search answered %v", got)
	}

	t.Setenv("TOSKAR_WEB_FIXTURES", filepath.Join(t.TempDir(), "missing.json"))
	if _, err := New(Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}); err == nil {
		t.Fatal("started with a missing fixtures file")
	}
}
