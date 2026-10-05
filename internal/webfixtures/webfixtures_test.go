package webfixtures

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/yeixio/toskar-core/internal/tools"
)

const sample = `{"sites":[{"match":"(?i)bitcoin","results":[{"title":"BTC price","url":"https://coins.example/btc","snippet":"64,210"}],
"pages":{"https://coins.example/":"Bitcoin is 64,210 USD.","https://coins.example/btc":"BTC: 64,210 USD today."}}]}`

func write(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "web.json")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFixturesAnswerTheWebTools(t *testing.T) {
	f, err := Load(write(t, sample))
	if err != nil {
		t.Fatal(err)
	}
	r := tools.NewRegistry(t.TempDir(), nil)
	Register(r, f)
	run := func(id string, args map[string]any) map[string]any {
		t.Helper()
		tool, err := r.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		out, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	results := run("internet.search", map[string]any{"query": "Bitcoin price now"})["results"].([]any)
	if got := results[0].(map[string]any)["url"]; got != "https://coins.example/btc" {
		t.Errorf("search: %v", got)
	}
	if got := run("internet.search", map[string]any{"query": "kayaks"})["results"].([]any)[0].(map[string]any)["url"]; got != "https://example.com/kayaks" {
		t.Errorf("unmatched search: %v", got)
	}
	// The longest prefix wins.
	if got := run("internet.open", map[string]any{"url": "https://coins.example/btc?x=1"})["content"]; got != "BTC: 64,210 USD today." {
		t.Errorf("open: %v", got)
	}
	if got := run("internet.open", map[string]any{"url": "https://elsewhere.example/"})["content"]; got != "This page has no more about that." {
		t.Errorf("unknown page: %v", got)
	}
	if got := run("places.search", map[string]any{"query": "bitcoin", "near": "Juneau"})["places"].([]any); len(got) != 1 {
		t.Errorf("places: %v", got)
	}
	if _, ok := run("maps.route", nil)["error"]; !ok {
		t.Error("maps.route should have nothing")
	}
}

func TestLoadRefusesABadPattern(t *testing.T) {
	if _, err := Load(write(t, `{"sites":[{"match":"("}]}`)); err == nil {
		t.Fatal("no error for a bad pattern")
	}
}
