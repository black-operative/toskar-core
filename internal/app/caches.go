package app

import (
	"context"
	"reflect"
	"strings"
	"time"

	"github.com/yeixio/toskar-core/internal/cache"
	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/inventory"
)

// Cache policies (spec §36). Web lookups are personal: they say what the
// person asked about, so they stay in memory on this computer and are
// cleared with run records.
var (
	webSearchPolicy = cache.Policy{
		Name: "web_search", Label: "Web searches", Key: "the search query, in lower case",
		TTL: 15 * time.Minute, Invalidation: "age; cleared with run records", Scope: "this computer",
		Privacy: cache.Personal, MaxEntries: 200,
	}
	webPagePolicy = cache.Policy{
		Name: "web_pages", Label: "Web pages read", Key: "the page address",
		TTL: 30 * time.Minute, Invalidation: "age; cleared with run records", Scope: "this computer",
		Privacy: cache.Personal, MaxEntries: 100,
	}
	capabilitiesPolicy = cache.Policy{
		Name: "capabilities", Label: "Capability inventory (models, tools, computers)", Key: "one snapshot",
		TTL:          30 * time.Second,
		Invalidation: "a model downloaded, loaded, or unloaded; a computer paired, online, or offline; a tool turned on or off",
		Scope:        "this computer", Privacy: cache.Public, MaxEntries: 1,
	}
	knowledgeIndexPolicy = cache.Policy{
		Name: "knowledge_index", Label: "Knowledge search index", Key: "source and passage",
		Invalidation: "a source's files change, it is reindexed, or it is removed",
		Scope:        "this computer", Privacy: cache.Personal, Persistent: true,
	}
)

// webCache answers repeat web searches and page reads from memory (§36).
type webCache struct {
	search *cache.Cache[map[string]any]
	pages  *cache.Cache[map[string]any]
}

func (w webCache) key(toolID string, args map[string]any) (*cache.Cache[map[string]any], string) {
	switch toolID {
	case "internet.search":
		q, _ := args["query"].(string)
		return w.search, strings.ToLower(strings.Join(strings.Fields(q), " "))
	case "internet.open":
		u, _ := args["url"].(string)
		return w.pages, strings.TrimSpace(u)
	}
	return nil, ""
}

// Lookup returns a cached result for a repeat search or page.
func (w webCache) Lookup(toolID string, args map[string]any) (map[string]any, bool) {
	c, k := w.key(toolID, args)
	if c == nil || k == "" {
		return nil, false
	}
	return c.Get(k)
}

// Store keeps a result that has something in it.
func (w webCache) Store(toolID string, args map[string]any, result map[string]any) {
	c, k := w.key(toolID, args)
	if c == nil || k == "" || len(result) == 0 {
		return
	}
	// An empty search is not kept, so the next try searches again. The
	// search tool returns its own slice type, so count by reflection.
	if toolID == "internet.search" {
		rows := reflect.ValueOf(result["results"])
		if rows.Kind() != reflect.Slice || rows.Len() == 0 {
			return
		}
	}
	c.Put(k, result)
}

// setupCaches creates the caches and lists them, with the persistent stores
// other parts keep.
func (a *App) setupCaches() {
	a.Caches = cache.NewRegistry()
	web := webCache{search: cache.New[map[string]any](webSearchPolicy), pages: cache.New[map[string]any](webPagePolicy)}
	a.capCache = cache.New[inventory.Snapshot](capabilitiesPolicy)
	a.tokenCounts = cache.New[int](tokenCountPolicy)
	for _, s := range []cache.Store{web.search, web.pages, a.capCache, a.tokenCounts} {
		_ = a.Caches.Add(s)
	}
	if a.Tools != nil {
		a.Tools.SetCache(web)
	}
	if a.HF != nil && a.HF.Cache != nil {
		_ = a.Caches.Add(a.HF.Cache)
	}
	_ = a.Caches.Add(cache.Described{P: knowledgeIndexPolicy, Count: func() int {
		n := 0
		if a.DB != nil {
			_ = a.DB.SQL.QueryRow(`SELECT COUNT(*) FROM knowledge_fts`).Scan(&n)
		}
		return n
	}})
}

// capabilityEvents change what the capability inventory says.
var capabilityEvents = map[string]bool{
	events.ModelDownloadCompleted: true, events.ModelDownloadFailed: true, events.ModelLoadCompleted: true,
	events.ModelUnloaded: true, events.NodePaired: true, events.NodeOnline: true, events.NodeOffline: true,
}

// invalidateCapabilities drops the cached inventory.
func (a *App) invalidateCapabilities() {
	if a.capCache != nil {
		a.capCache.Delete("snapshot")
	}
}

// watchCapabilities drops the cached inventory when what it describes
// changes.
func (a *App) watchCapabilities(ctx context.Context) {
	if a.Bus == nil || a.capCache == nil {
		return
	}
	id, ch := a.Bus.Subscribe()
	go func() {
		defer a.Bus.Unsubscribe(id)
		for {
			select {
			case <-ctx.Done():
				return
			case evt, ok := <-ch:
				if !ok {
					return
				}
				if capabilityEvents[evt.Type] {
					a.invalidateCapabilities()
				}
			}
		}
	}()
}

// CacheList lists every cache with its policy and counts.
func (a *App) CacheList() []cache.Info {
	if a.Caches == nil {
		return []cache.Info{}
	}
	return a.Caches.List()
}

// ClearCache clears one cache by name.
func (a *App) ClearCache(name string) bool {
	return a.Caches != nil && a.Caches.Clear(name)
}
