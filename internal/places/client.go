// Package places finds places and routes with OpenStreetMap services
// (Gungnir §25): Nominatim to find places and addresses, Overpass to find
// kinds of places near somewhere, and OSRM for routes. Each request is
// recorded in What left this computer; a repeat within the hour is answered
// from memory and sends nothing. The services can be your own.
package places

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/toskar-core/internal/cache"
)

// Default public services. Their usage policies ask for light use, an
// identifying User-Agent, and at most one Nominatim request a second.
const (
	DefaultGeocoder = "https://nominatim.openstreetmap.org"
	DefaultOverpass = "https://overpass-api.de/api/interpreter"
	DefaultRouter   = "https://routing.openstreetmap.de"
)

// Attribution is the credit OpenStreetMap data needs wherever it is shown.
const Attribution = "© OpenStreetMap contributors (ODbL)"

// CachePolicy keeps lookups in memory for an hour. They say where the
// person was looking, so they stay on this computer and are cleared with
// run records.
var CachePolicy = cache.Policy{
	Name: "places", Label: "Places and routes", Key: "the request to the map service",
	TTL: time.Hour, Invalidation: "age; cleared with run records", Scope: "this computer",
	Privacy: cache.Personal, MaxEntries: 300,
}

// Client calls the map services.
type Client struct {
	// Geocoder, Overpass, and Router are the services' addresses; empty
	// uses the defaults.
	Geocoder, Overpass, Router string
	HTTP                       *http.Client
	UserAgent                  string
	// Record is called for each request that leaves this computer.
	Record func(ctx context.Context, destination, detail string)
	Cache  *cache.Cache[[]byte]

	mu          sync.Mutex
	lastGeocode time.Time
	// geocodeGap spaces Nominatim requests; tests shorten it.
	geocodeGap time.Duration
}

func (c *Client) base(v, def string) string {
	if v = strings.TrimRight(strings.TrimSpace(v), "/"); v != "" {
		return v
	}
	return def
}

func (c *Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 25 * time.Second}
}

// waitGeocoder keeps to one Nominatim request a second.
func (c *Client) waitGeocoder(ctx context.Context) error {
	gap := c.geocodeGap
	if gap == 0 {
		gap = 1100 * time.Millisecond
	}
	c.mu.Lock()
	wait := time.Until(c.lastGeocode.Add(gap))
	if wait < 0 {
		wait = 0
	}
	c.lastGeocode = time.Now().Add(wait)
	c.mu.Unlock()
	if wait == 0 {
		return nil
	}
	select {
	case <-time.After(wait):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// get fetches a URL, from the cache when it can. detail says what was
// asked, for the record; form, when set, is POSTed.
func (c *Client) get(ctx context.Context, rawURL string, form url.Values, detail string, geocoder bool) ([]byte, error) {
	key := rawURL
	if form != nil {
		key += "?" + form.Encode()
	}
	if c.Cache != nil {
		if b, ok := c.Cache.Get(key); ok {
			return b, nil
		}
	}
	if geocoder {
		if err := c.waitGeocoder(ctx); err != nil {
			return nil, err
		}
	}
	var req *http.Request
	var err error
	if form != nil {
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, rawURL, strings.NewReader(form.Encode()))
		if req != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	} else {
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	}
	if err != nil {
		return nil, err
	}
	ua := c.UserAgent
	if ua == "" {
		ua = "Toskar (+https://github.com/yeixio/toskar-core)"
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "application/json")
	if c.Record != nil {
		c.Record(ctx, req.URL.Hostname(), detail)
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("the map service could not be reached: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("the map service is busy; try again in a minute")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the map service answered HTTP %d", resp.StatusCode)
	}
	if c.Cache != nil {
		c.Cache.Put(key, body)
	}
	return body, nil
}
