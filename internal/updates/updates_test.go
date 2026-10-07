package updates

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"1.7.0", "1.6.1", true},
		{"1.6.1", "1.6.1", false},
		{"1.6.0", "1.6.1", false},
		{"v1.10.0", "1.9.9", true},
		{"2.0.0", "1.99.99", true},
		{"1.7.0", "1.7.0-beta.1", true},
		{"1.7.0-beta.1", "1.7.0", false},
		{"1.7.0-beta.2", "1.7.0-beta.1", true},
		{"1.7.0+build.5", "1.6.9", true},
		{"latest", "1.6.1", false},
		{"1.7", "1.6.1", false},
	}
	for _, c := range cases {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestChecksOnlyReleaseBuilds(t *testing.T) {
	for v, want := range map[string]bool{"1.6.1": true, "v1.7.0-beta.1": true, "0.1.0-dev": false, "unknown": false, "": false} {
		if got := Checks(v); got != want {
			t.Errorf("Checks(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestCheckReadsTheLatestRelease(t *testing.T) {
	body := `{"version":"1.7.0","published_at":"2026-10-10T00:00:00Z","prerelease":false,"notes_url":"https://toskar.ai/whats-new","download_url":"https://toskar.ai/download"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.ContentLength > 0 {
			t.Errorf("request = %s with %d bytes; the check sends nothing", r.Method, r.ContentLength)
		}
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	var sent []string
	c := &Checker{URL: srv.URL, Current: "1.6.1", Sent: func(_ context.Context, host, detail string) { sent = append(sent, host+": "+detail) }}
	ctx := context.Background()

	if s := c.Status(ctx); s.Available || s.Latest != nil || !s.Enabled || !s.Supported {
		t.Fatalf("before a check = %+v", s)
	}
	if err := c.Check(ctx); err != nil {
		t.Fatal(err)
	}
	s := c.Status(ctx)
	if !s.Available || s.Latest == nil || s.Latest.Version != "1.7.0" || s.Latest.DownloadURL != "https://toskar.ai/download" || s.CheckedAt == nil {
		t.Fatalf("status = %+v", s)
	}
	if len(sent) != 1 {
		t.Fatalf("What left this computer = %v", sent)
	}

	// Up to date: nothing to offer.
	c.Current = "1.7.0"
	if s := c.Status(ctx); s.Available {
		t.Fatalf("up to date but offered: %+v", s)
	}
	// Turned off: nothing shown, though the build could check.
	c.Current = "1.6.1"
	c.On = func(context.Context) bool { return false }
	if s := c.Status(ctx); s.Enabled || !s.Supported || s.Available || s.Latest != nil {
		t.Fatalf("turned off but shown: %+v", s)
	}
	// A build that doesn't check says so, whatever the setting.
	c.On, c.Supported = nil, func() bool { return false }
	if s := c.Status(ctx); s.Enabled || s.Supported || s.Available {
		t.Fatalf("unsupported build = %+v", s)
	}
}

func TestPrereleasesAreOfferedOnlyToPrereleases(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"version":"1.8.0-beta.1","prerelease":true}`))
	}))
	defer srv.Close()
	ctx := context.Background()
	stable := &Checker{URL: srv.URL, Current: "1.7.0"}
	beta := &Checker{URL: srv.URL, Current: "1.7.0-beta.3"}
	for _, c := range []*Checker{stable, beta} {
		if err := c.Check(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if stable.Status(ctx).Available {
		t.Error("a beta was offered to a stable build")
	}
	if !beta.Status(ctx).Available {
		t.Error("a newer beta wasn't offered to a beta build")
	}
}

func TestCheckRejectsBadAnswers(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
	}{
		{http.StatusServiceUnavailable, `{"error":"unavailable"}`},
		{http.StatusOK, `not json`},
		{http.StatusOK, `{"version":"soon"}`},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		}))
		c := &Checker{URL: srv.URL, Current: "1.6.1"}
		if err := c.Check(context.Background()); err == nil {
			t.Errorf("%d %s: no error", tc.status, tc.body)
		}
		if c.Status(context.Background()).Latest != nil {
			t.Errorf("%d %s: kept a bad answer", tc.status, tc.body)
		}
		srv.Close()
	}
}
