package api

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/config"
)

// The API moves to a new address without returning from ListenAndServe,
// and goes back when the new one can't be bound (#216).
func TestRebindMovesTheAPI(t *testing.T) {
	srv := NewServer(Dependencies{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe("127.0.0.1:0") }()
	deadline := time.Now().Add(3 * time.Second)
	for srv.Addr() == "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	first := srv.Addr()
	if first == "" {
		t.Fatal("the API never listened")
	}
	health := func(addr string) error {
		resp, err := (&http.Client{Timeout: time.Second}).Get("http://" + addr + "/api/v1/health")
		if err == nil {
			resp.Body.Close()
		}
		return err
	}
	if err := health(first); err != nil {
		t.Fatal(err)
	}

	if err := srv.Rebind("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	second := srv.Addr()
	if second == first {
		t.Fatal("the address didn't change")
	}
	if err := health(second); err != nil {
		t.Fatalf("new address: %v", err)
	}
	if err := health(first); err == nil {
		t.Fatal("the old address still answers")
	}
	select {
	case err := <-done:
		t.Fatalf("ListenAndServe returned on a rebind: %v", err)
	default:
	}

	// An address that can't be bound leaves the API where it was.
	if err := srv.Rebind("203.0.113.7:1"); err == nil {
		t.Fatal("bound an address this computer doesn't have")
	}
	if srv.Addr() != second || health(second) != nil {
		t.Fatalf("after a failed rebind the API is at %q", srv.Addr())
	}

	if err := srv.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("ListenAndServe didn't return after Shutdown")
	}
}

// With network access off, a request from another device is refused, such
// as one on a connection from before it was turned off.
func TestLoopbackAPIRefusesOtherDevices(t *testing.T) {
	mgr, err := config.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(Dependencies{Config: mgr})
	r := localRequest(http.MethodGet, "/api/v1/health", nil)
	r.RemoteAddr = "192.168.1.20:5000"
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "LAN_ACCESS_OFF") {
		t.Fatalf("other device: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, localRequest(http.MethodGet, "/api/v1/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("this computer: %d", rec.Code)
	}
}
