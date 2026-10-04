package browser

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/store"
)

func TestGuard(t *testing.T) {
	g := &Guard{}
	ctx := context.Background()
	for _, host := range []string{"127.0.0.1", "10.1.2.3", "192.168.1.1", "172.16.0.5", "169.254.169.254", "100.64.0.1", "::1", "[fe80::1]", "0.0.0.0", "localhost", "printer.local", "db.internal"} {
		if err := g.CheckHost(ctx, host); !errors.Is(err, ErrPrivate) {
			t.Errorf("%s allowed: %v", host, err)
		}
	}
	for _, host := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if err := g.CheckHost(ctx, host); err != nil {
			t.Errorf("%s refused: %v", host, err)
		}
	}
	for _, raw := range []string{"file:///etc/passwd", "javascript:alert(1)", "ftp://example.com", "chrome://settings"} {
		if err := g.CheckURL(ctx, raw); err == nil {
			t.Errorf("%s allowed", raw)
		}
	}
}

const site = `<!doctype html><html><head><title>Fern Shop</title></head><body><main>
<h1>Fern Shop</h1><p>Ferns for shady gardens.</p>
<a href="/care">Care guide</a>
<form action="/search"><input name="q" placeholder="Search ferns"><input type="password" name="pw" placeholder="Password"><button>Search</button></form>
<table><tr><th>Fern</th><th>Price</th></tr><tr><td>Maidenhair</td><td>$12</td></tr><tr><td>Sword</td><td>$9</td></tr></table>
<a href="/prices.csv">Price list</a>
<a id="away" href="%s">Router admin</a>
</main></body></html>`

// shop is a small site on this computer, which the tests let the browser
// open; secret is a server it must not reach.
func shop(t *testing.T) (*Manager, string, *atomic.Int32, *artifacts.Store, context.Context) {
	t.Helper()
	m := &Manager{WorkDir: t.TempDir()}
	if ok, why := m.Available(); !ok {
		t.Skip(why)
	}
	// go test ./... runs packages side by side, and a first start of the
	// browser with a new profile can then take longer than it would alone.
	limit := startLimit
	startLimit = 2 * time.Minute
	t.Cleanup(func() { startLimit = limit })
	var secretHits atomic.Int32
	secret := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { secretHits.Add(1) }))
	t.Cleanup(secret.Close)
	su, _ := url.Parse(secret.URL)
	away := "http://localhost:" + su.Port() + "/admin"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "abc123", Path: "/"})
			fmt.Fprintf(w, site, away)
		case "/care":
			fmt.Fprint(w, `<html><head><title>Care</title></head><body><main><p>Keep the soil moist.</p></main></body></html>`)
		case "/search":
			fmt.Fprintf(w, `<html><head><title>Results</title></head><body><main><p>Results for %s: 2 ferns</p></main></body></html>`, r.URL.Query().Get("q"))
		case "/prices.csv":
			c, err := r.Cookie("session")
			if err != nil || c.Value != "abc123" {
				http.Error(w, "no session", http.StatusForbidden)
				return
			}
			w.Header().Set("Content-Disposition", `attachment; filename="fern-prices.csv"`)
			fmt.Fprint(w, "fern,price\nMaidenhair,12\n")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	m.Guard = &Guard{Allow: func(host string) bool { return host == "127.0.0.1" }}
	t.Cleanup(m.CloseAll)
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.SQL.Exec(`INSERT INTO conversations (id, title, created_at, updated_at) VALUES ('c1', 't', '2026-01-01', '2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	st := artifacts.NewStore(db.SQL, filepath.Join(dir, "artifacts"))
	return m, srv.URL, &secretHits, st, artifacts.WithConversation(context.Background(), "c1")
}

func refOf(t *testing.T, s Snapshot, label string) int {
	t.Helper()
	for _, e := range s.Elements {
		if e.Label == label {
			return e.Ref
		}
	}
	t.Fatalf("no element %q in %+v", label, s.Elements)
	return 0
}

// The browser opens a page, follows a link, searches, and refuses to type a
// password (Gungnir §26).
func TestBrowseAndAct(t *testing.T) {
	m, base, secretHits, st, ctx := shop(t)
	var opened []string
	m.Opened = func(_ context.Context, host, raw string) { opened = append(opened, raw) }
	k := key(ctx)
	s, err := m.Open(ctx, k, base+"/")
	if err != nil {
		t.Fatal(err)
	}
	if s.Title != "Fern Shop" || !strings.Contains(s.Text, "Ferns for shady gardens.") {
		t.Fatalf("snapshot %q %q", s.Title, s.Text)
	}
	var pw Element
	for _, e := range s.Elements {
		if e.Type == "password" {
			pw = e
		}
	}
	if !pw.Sensitive {
		t.Fatalf("the password field is not marked: %+v", s.Elements)
	}
	if _, err := m.Type(ctx, k, pw.Ref, "hunter2", false); !errors.Is(err, ErrSensitive) {
		t.Fatalf("typed into a password field: %v", err)
	}
	s, err = m.Type(ctx, k, refOf(t, s, "Search ferns"), "maidenhair", true)
	if err != nil {
		t.Fatal(err)
	}
	if s.Title != "Results" || !strings.Contains(s.Text, "Results for maidenhair") {
		t.Fatalf("after search %q %q", s.Title, s.Text)
	}
	s, _ = m.Open(ctx, k, base+"/")
	s, err = m.Click(ctx, k, refOf(t, s, "Care guide"))
	if err != nil || !strings.Contains(s.Text, "Keep the soil moist.") {
		t.Fatalf("after click %q %v", s.Text, err)
	}
	if len(opened) < 3 {
		t.Fatalf("opened %v", opened)
	}

	// A link to this computer by name is blocked inside the browser.
	s, _ = m.Open(ctx, k, base+"/")
	_, _ = m.Click(ctx, k, refOf(t, s, "Router admin"))
	if secretHits.Load() != 0 {
		t.Fatal("the browser reached a server on this computer")
	}
	if _, err := m.Open(ctx, k, "http://localhost:1/"); !errors.Is(err, ErrPrivate) {
		t.Fatalf("opened localhost: %v", err)
	}

	// Reading, downloading with the page's session, and a screenshot.
	s, _ = m.Open(ctx, k, base+"/")
	tables, err := m.Extract(ctx, k, "tables")
	if err != nil || !strings.Contains(fmt.Sprint(tables["tables"]), "Maidenhair $12") {
		t.Fatalf("tables %v %v", tables, err)
	}
	dl := DownloadTool{tool{m}, st}
	res, err := dl.Execute(ctx, map[string]any{"ref": float64(refOf(t, s, "Price list"))})
	if err != nil {
		t.Fatal(err)
	}
	a, data, _ := st.Read(ctx, res["id"].(string))
	if a.Name != "fern-prices.csv" || !bytes.Contains(data, []byte("Maidenhair,12")) {
		t.Fatalf("download %+v %q", a, data)
	}
	shot, err := ScreenshotTool{tool{m}, st}.Execute(ctx, nil)
	if err != nil || shot["kind"] != "image" {
		t.Fatalf("screenshot %v %v", shot, err)
	}
	if out, _ := (CloseTool{tool{m}}).Execute(ctx, nil); out["closed"] != true {
		t.Fatal("not closed")
	}
	if left, _ := filepath.Glob(filepath.Join(m.WorkDir, "profile-*")); len(left) != 0 {
		t.Fatalf("profiles left: %v", left)
	}
}
