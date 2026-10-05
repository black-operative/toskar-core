package quality

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// A daemon that is briefly unavailable, such as while it restarts, gets the
// request again instead of failing the case.
func TestRealDriverRetriesAnUnavailableDaemon(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			http.Error(w, "restarting", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	d := newRealDriver(srv.URL)
	var out struct{ OK bool }
	if code := d.do(t, http.MethodGet, "/api/v1/health", nil, &out); code != http.StatusOK || !out.OK {
		t.Fatalf("code %d, out %+v", code, out)
	}
	if calls.Load() != 3 {
		t.Fatalf("calls = %d, want 3", calls.Load())
	}
}

// A daemon that starts asking for a key stops the run with one reason, so
// the cases after it aren't each failed by the same thing.
func TestRealDriverStopsWhenTheDaemonWantsAKey(t *testing.T) {
	t.Setenv("TOSKAR_QUALITY_KEY", "")
	t.Setenv("YGGDRASIL_QUALITY_KEY", "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"code":"UNAUTHORIZED"}}`, http.StatusUnauthorized)
	}))
	defer srv.Close()
	d := newRealDriver(srv.URL)
	if _, err := d.request(http.MethodGet, "/api/v1/profiles/general-assistant", nil, nil, t.Logf); err == nil {
		t.Fatal("no error for a 401")
	}
	if !strings.Contains(d.Stopped(), "TOSKAR_QUALITY_KEY") {
		t.Fatalf("stopped = %q", d.Stopped())
	}
}
