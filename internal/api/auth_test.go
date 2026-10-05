package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/config"
)

func TestControlAPIAuthFollowsBind(t *testing.T) {
	dir := t.TempDir()
	mgr, err := config.NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Update(func(c *config.Config) { c.APIHost = "0.0.0.0" }); err != nil {
		t.Fatal(err)
	}
	const secret = "ygg_testkeyvalue"
	srv := NewServer(Dependencies{
		Config: mgr,
		VerifyAPIKey: func(ctx context.Context, got string) (auth.APIKeyRecord, error) {
			if got == secret {
				return auth.APIKeyRecord{ID: "1"}, nil
			}
			return auth.APIKeyRecord{}, fmt.Errorf("invalid api key")
		},
	})

	unauth := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, unauth)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("non-loopback without key: %d", rec.Code)
	}

	inURL := httptest.NewRequest(http.MethodGet, "/api/v1/health?api_key="+secret, nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, inURL)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("key in URL: %d", rec.Code)
	}

	ok := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	ok.Header.Set("Authorization", "Bearer "+secret)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, ok)
	if rec.Code != http.StatusOK {
		t.Fatalf("bearer: %d %s", rec.Code, rec.Body.String())
	}

	// An app on this computer needs no key with network access on; one
	// behind a proxy here is from the network.
	local := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	local.RemoteAddr = "127.0.0.1:50000"
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, local)
	if rec.Code != http.StatusOK {
		t.Fatalf("loopback client with network access on: %d", rec.Code)
	}
	local.Header.Set("X-Forwarded-For", "192.168.1.20")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, local)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("proxied loopback client: %d", rec.Code)
	}

	about := httptest.NewRequest(http.MethodGet, "/source", nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, about)
	if rec.Code != http.StatusOK {
		t.Fatalf("source offer should stay open: %d", rec.Code)
	}

	if err := mgr.Update(func(c *config.Config) { c.APIHost = "127.0.0.1" }); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("loopback: %d", rec.Code)
	}
}
