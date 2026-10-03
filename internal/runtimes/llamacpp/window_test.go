package llamacpp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWindowReadsProps(t *testing.T) {
	for _, tc := range []struct {
		body string
		want int
	}{
		{`{"default_generation_settings":{"n_ctx":16384},"total_slots":1}`, 16384},
		{`{"n_ctx":4096}`, 4096},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/props" {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(tc.body))
		}))
		got, err := Window(context.Background(), srv.URL)
		srv.Close()
		if err != nil || got != tc.want {
			t.Fatalf("Window(%s) = %d, %v; want %d", tc.body, got, err, tc.want)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) }))
	defer srv.Close()
	if _, err := Window(context.Background(), srv.URL); err == nil {
		t.Fatal("want an error when /props has no window")
	}
}
