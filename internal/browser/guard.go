// Package browser lets the assistant use web pages in an isolated browser
// (Gungnir §26): open a page, read it, click, type, download, and take a
// screenshot. Each chat gets its own headless browser with a fresh profile,
// never the person's own, and every request is checked so a page cannot
// reach this computer or the local network.
package browser

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/toskar-core/internal/netguard"
)

// ErrPrivate is a request for this computer or the local network.
var ErrPrivate = errors.New("this address is on your own network or computer, which the browser does not open")

// Guard decides which hosts the browser may reach. The address rules are
// netguard's, shared with internet.open; this adds a short cache, names
// that are local by convention, and a hook for tests.
type Guard struct {
	// Allow, when set, lets a host through regardless; tests use it for
	// their local servers.
	Allow func(host string) bool
	// Net looks hosts up and judges their addresses; nil uses the system's.
	Net *netguard.Guard

	mu    sync.Mutex
	cache map[string]guardEntry
}

type guardEntry struct {
	err error
	at  time.Time
}

// CheckURL allows an http or https address on the public internet.
func (g *Guard) CheckURL(ctx context.Context, raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return errors.New("the address must start with http:// or https://")
	}
	return g.CheckHost(ctx, u.Hostname())
}

// CheckHost allows a host whose every address is public. Answers are kept
// for a minute.
func (g *Guard) CheckHost(ctx context.Context, host string) error {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if g.Allow != nil && g.Allow(host) {
		return nil
	}
	if strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") || strings.HasSuffix(host, ".lan") || strings.HasSuffix(host, ".home.arpa") {
		return ErrPrivate
	}
	g.mu.Lock()
	if e, ok := g.cache[host]; ok && time.Since(e.at) < time.Minute {
		g.mu.Unlock()
		return e.err
	}
	g.mu.Unlock()
	err := g.lookup(ctx, host)
	g.mu.Lock()
	if g.cache == nil {
		g.cache = map[string]guardEntry{}
	}
	g.cache[host] = guardEntry{err: err, at: time.Now()}
	g.mu.Unlock()
	return err
}

func (g *Guard) lookup(ctx context.Context, host string) error {
	ng := g.Net
	if ng == nil {
		ng = &netguard.Guard{}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := ng.Lookup(ctx, strings.Trim(host, "[]")); err != nil {
		if errors.Is(err, netguard.ErrPrivate) {
			return ErrPrivate
		}
		return errors.New("the site's address could not be found")
	}
	return nil
}
