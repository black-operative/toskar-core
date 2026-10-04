package app

import (
	"testing"

	"github.com/yeixio/toskar-core/internal/cache"
)

func TestWebCacheKeysAndPrivacy(t *testing.T) {
	a := &App{}
	a.setupCaches()
	web := webCache{search: cache.New[map[string]any](webSearchPolicy), pages: cache.New[map[string]any](webPagePolicy)}
	web.Store("internet.search", map[string]any{"query": "Tide  Times"}, map[string]any{"results": []any{"x"}})
	if _, ok := web.Lookup("internet.search", map[string]any{"query": "tide times"}); !ok {
		t.Fatal("same query, different case and spacing, missed")
	}
	type hit struct{ Title string }
	web.Store("internet.search", map[string]any{"query": "typed"}, map[string]any{"results": []hit{{"a"}}})
	if _, ok := web.Lookup("internet.search", map[string]any{"query": "typed"}); !ok {
		t.Fatal("results of the search tool's own type were not cached")
	}
	web.Store("internet.search", map[string]any{"query": "nothing"}, map[string]any{"results": []any{}})
	if _, ok := web.Lookup("internet.search", map[string]any{"query": "nothing"}); ok {
		t.Fatal("an empty search was cached")
	}
	if _, ok := web.Lookup("terminal", map[string]any{"command": "ls"}); ok {
		t.Fatal("a command was cached")
	}
	names := map[string]cache.Info{}
	for _, info := range a.CacheList() {
		names[info.Name] = info
	}
	for _, want := range []string{"web_search", "web_pages", "capabilities", "knowledge_index"} {
		if _, ok := names[want]; !ok {
			t.Errorf("missing cache %s in %v", want, names)
		}
	}
	if names["web_search"].Privacy != cache.Personal || names["capabilities"].Privacy != cache.Public || !names["knowledge_index"].Persistent {
		t.Fatalf("policies = %+v", names)
	}
	if !a.ClearCache("web_search") || a.ClearCache("nope") {
		t.Fatal("clear")
	}
}
