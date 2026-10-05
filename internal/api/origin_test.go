package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/config"
)

// localRequest is a request from this computer to the API on 127.0.0.1:7331,
// as toskarctl or the web UI would send it.
func localRequest(method, target string, body io.Reader) *http.Request {
	r := httptest.NewRequest(method, target, body)
	r.Host = "127.0.0.1:7331"
	r.RemoteAddr = "127.0.0.1:50123"
	return r
}

const originTestKey = "ygg_origintestkey"

func originTestServer(t *testing.T, apiHost string) *Server {
	t.Helper()
	mgr, err := config.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Update(func(c *config.Config) { c.APIHost = apiHost; c.APIPort = 7331 }); err != nil {
		t.Fatal(err)
	}
	return NewServer(Dependencies{
		Config: mgr,
		VerifyAPIKey: func(ctx context.Context, got string) (auth.APIKeyRecord, error) {
			if got == originTestKey {
				return auth.APIKeyRecord{ID: "1"}, nil
			}
			return auth.APIKeyRecord{}, fmt.Errorf("invalid api key")
		},
	})
}

func TestBrowserOriginsOverLoopback(t *testing.T) {
	srv := originTestServer(t, "127.0.0.1")
	cases := []struct {
		name   string
		origin string
		host   string
		key    bool
		want   int
		echo   bool // Access-Control-Allow-Origin echoes the Origin
	}{
		{name: "no origin (toskarctl)", want: http.StatusOK},
		{name: "same origin web UI", origin: "http://127.0.0.1:7331", want: http.StatusOK, echo: true},
		{name: "same origin via localhost", origin: "http://localhost:7331", host: "localhost:7331", want: http.StatusOK, echo: true},
		{name: "loopback alias on API port", origin: "http://localhost:7331", want: http.StatusOK, echo: true},
		{name: "IPv6 loopback on API port", origin: "http://[::1]:7331", want: http.StatusOK, echo: true},
		{name: "desktop webview macOS", origin: "wails://wails", want: http.StatusOK, echo: true},
		{name: "desktop webview Windows", origin: "http://wails.localhost", want: http.StatusOK, echo: true},
		{name: "website", origin: "https://evil.example", want: http.StatusForbidden},
		{name: "loopback other port", origin: "http://localhost:3000", want: http.StatusForbidden},
		{name: "null origin", origin: "null", want: http.StatusForbidden},
		{name: "website with key", origin: "https://evil.example", key: true, want: http.StatusOK, echo: true},
		{name: "rebinding same origin", origin: "http://evil.example:7331", host: "evil.example:7331", want: http.StatusForbidden},
		{name: "rebinding GET without origin", host: "evil.example:7331", want: http.StatusForbidden},
		{name: "rebinding with key", host: "evil.example:7331", key: true, want: http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := localRequest(http.MethodGet, "/api/v1/health", nil)
			if tc.host != "" {
				req.Host = tc.host
			}
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.key {
				req.Header.Set("Authorization", "Bearer "+originTestKey)
			}
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status %d, want %d: %s", rec.Code, tc.want, rec.Body.String())
			}
			got := rec.Header().Get("Access-Control-Allow-Origin")
			if got == "*" {
				t.Fatal("wildcard origin")
			}
			if tc.echo && got != tc.origin {
				t.Fatalf("Allow-Origin %q, want %q", got, tc.origin)
			}
			if !tc.echo && got != "" {
				t.Fatalf("Allow-Origin %q, want none", got)
			}
		})
	}
}

func TestWebsiteCannotPostOverLoopback(t *testing.T) {
	srv := originTestServer(t, "127.0.0.1")
	// A form or text/plain fetch needs no preflight, so the request itself
	// must be turned away before any handler runs.
	for _, path := range []string{"/api/v1/settings/reset", "/v1/chat/completions"} {
		req := localRequest(http.MethodPost, path, nil)
		req.Header.Set("Origin", "https://evil.example")
		req.Header.Set("Content-Type", "text/plain")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s: status %d", path, rec.Code)
		}
	}
}

func TestPreflight(t *testing.T) {
	srv := originTestServer(t, "127.0.0.1")
	preflight := func(origin, headers string) *httptest.ResponseRecorder {
		req := localRequest(http.MethodOptions, "/api/v1/settings", nil)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", http.MethodPatch)
		if headers != "" {
			req.Header.Set("Access-Control-Request-Headers", headers)
		}
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		return rec
	}

	rec := preflight("wails://wails", "content-type")
	if rec.Code != http.StatusNoContent || rec.Header().Get("Access-Control-Allow-Origin") != "wails://wails" {
		t.Fatalf("desktop preflight: %d %q", rec.Code, rec.Header().Get("Access-Control-Allow-Origin"))
	}
	if rec.Header().Get("Access-Control-Allow-Private-Network") != "true" {
		t.Fatal("desktop preflight lacks Private Network Access")
	}

	if rec := preflight("https://evil.example", "content-type"); rec.Code != http.StatusForbidden {
		t.Fatalf("website preflight: %d", rec.Code)
	}

	// A website that holds a key may call the API; its preflight has no key.
	rec = preflight("https://app.example", "content-type, authorization")
	if rec.Code != http.StatusNoContent || rec.Header().Get("Access-Control-Allow-Origin") != "https://app.example" {
		t.Fatalf("keyed preflight: %d %q", rec.Code, rec.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestHostWithNetworkAccess(t *testing.T) {
	srv := originTestServer(t, "0.0.0.0")
	cases := []struct {
		name   string
		host   string
		remote string
		origin string
		want   int
	}{
		// On main every request needs a key once network access is on, so
		// these send one; the point is which Hosts reach the auth check.
		{name: "LAN IP from another device", host: "192.168.1.20:7331", remote: "192.168.1.30:51000", want: http.StatusOK},
		{name: "name from another device", host: "mac.local:7331", remote: "192.168.1.30:51000", want: http.StatusOK},
		{name: "same origin from another device", host: "192.168.1.20:7331", remote: "192.168.1.30:51000", origin: "http://192.168.1.20:7331", want: http.StatusOK},
		{name: "loopback", host: "127.0.0.1:7331", remote: "127.0.0.1:51000", want: http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := localRequest(http.MethodGet, "/api/v1/health", nil)
			req.Host, req.RemoteAddr = tc.host, tc.remote
			req.Header.Set("Authorization", "Bearer "+originTestKey)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status %d, want %d: %s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}

	// Without a key, the Host check runs before auth. Whether auth then asks
	// for a key over loopback is its own business; only the Host is tested.
	for _, tc := range []struct {
		name   string
		host   string
		refuse bool
	}{
		{name: "rebinding over loopback", host: "evil.example:7331", refuse: true},
		{name: "LAN IP over loopback", host: "192.168.1.20:7331"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := localRequest(http.MethodGet, "/api/v1/health", nil)
			req.Host = tc.host
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, req)
			if refused := rec.Code == http.StatusForbidden; refused != tc.refuse {
				t.Fatalf("status %d, refuse %v: %s", rec.Code, tc.refuse, rec.Body.String())
			}
		})
	}
}
