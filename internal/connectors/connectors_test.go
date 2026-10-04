package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/internal/tools"
)

const token = "github_pat_SECRET123456789"

type memSecrets struct {
	mu sync.Mutex
	m  map[string]string
}

func (s *memSecrets) Write(name, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[name] = value
	return nil
}
func (s *memSecrets) Read(name string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[name]
	if !ok {
		return "", os.ErrNotExist
	}
	return v, nil
}
func (s *memSecrets) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, name)
	return nil
}

type memRegistry struct{ tools map[string]tools.Tool }

func (r *memRegistry) Register(t tools.Tool) { r.tools[t.ID()] = t }
func (r *memRegistry) Unregister(id string)  { delete(r.tools, id) }

func fakeGitHub(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
			return
		}
		switch {
		case r.URL.Path == "/user":
			_, _ = w.Write([]byte(`{"login":"octo"}`))
		case r.URL.Path == "/search/issues":
			if r.URL.Query().Get("q") != "repo:o/r is:open" {
				t.Errorf("query = %q", r.URL.Query().Get("q"))
			}
			// A hostile result that echoes the token must not reach the model.
			_, _ = w.Write([]byte(`{"total_count":1,"items":[{"number":7,"title":"Crash with ` + token + `","state":"open","html_url":"https://github.com/o/r/issues/7","repository_url":"https://api.github.com/repos/o/r","user":{"login":"amy"}}]}`))
		case r.URL.Path == "/repos/o/r/issues/7":
			_, _ = w.Write([]byte(`{"number":7,"title":"Crash","state":"open","body":"It crashes","html_url":"https://github.com/o/r/issues/7","repository_url":"https://api.github.com/repos/o/r","pull_request":{"url":"x"},"user":{"login":"amy"}}`))
		case r.URL.Path == "/repos/o/r/issues/7/comments" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`[{"body":"Same here","user":{"login":"bo"}}]`))
		case r.URL.Path == "/repos/o/r/issues/7/comments" && r.Method == http.MethodPost:
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["body"] != "Thanks" {
				t.Errorf("comment = %+v", body)
			}
			_, _ = w.Write([]byte(`{"html_url":"https://github.com/o/r/issues/7#c1"}`))
		case r.URL.Path == "/repos/o/r/issues/8":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"token ` + token + ` lacks scope"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func newManager(t *testing.T) (*Manager, *memSecrets, *memRegistry) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	t.Cleanup(func() { tools.SetConnected("github", nil); tools.SetConnected("homeassistant", nil) })
	sec := &memSecrets{m: map[string]string{}}
	reg := &memRegistry{tools: map[string]tools.Tool{}}
	return NewManager(db.SQL, sec, reg, GitHub{}, HomeAssistant{}), sec, reg
}

func TestGitHubConnectRunAndDisconnect(t *testing.T) {
	srv := fakeGitHub(t)
	defer srv.Close()
	m, sec, reg := newManager(t)
	ctx := context.Background()

	if _, err := m.Connect(ctx, "github", map[string]string{"token": "wrong-token-value", "api_url": srv.URL}); err == nil || !strings.Contains(err.Error(), "not accepted") {
		t.Fatalf("bad token: %v", err)
	}
	if len(sec.m) != 0 || len(reg.tools) != 0 {
		t.Fatal("a failed connection stored something")
	}

	st, err := m.Connect(ctx, "github", map[string]string{"token": token, "api_url": srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if !st.Connected || st.Account != "@octo" || st.Values["token"] != "••••6789" || st.Values["api_url"] != srv.URL {
		t.Fatalf("status = %+v", st)
	}
	raw, _ := json.Marshal(st)
	if strings.Contains(string(raw), token) {
		t.Fatal("status contains the token")
	}
	if len(reg.tools) != 3 {
		t.Fatalf("registered %d tools", len(reg.tools))
	}
	def, ok := tools.Lookup("github.comment")
	if !ok || def.Source != "connector:github" || def.DefaultPolicy != tools.PolicyAsk || def.Risk != tools.RiskWrite {
		t.Fatalf("catalog = %+v %v", def, ok)
	}

	res, err := reg.tools["github.search"].Execute(ctx, map[string]any{"query": "repo:o/r is:open"})
	if err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(res)
	if strings.Contains(string(out), token) || !strings.Contains(string(out), "Crash with [redacted]") {
		t.Fatalf("search result = %s", out)
	}
	issue, err := reg.tools["github.issue"].Execute(ctx, map[string]any{"repo": "o/r", "number": float64(7)})
	if err != nil || issue["kind"] != "pull request" || issue["body"] != "It crashes" {
		t.Fatalf("issue = %+v, %v", issue, err)
	}
	if _, err := reg.tools["github.issue"].Execute(ctx, map[string]any{"repo": "o/r", "number": "#8"}); err == nil || strings.Contains(err.Error(), token) || !strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("error = %v", err)
	}
	if _, err := reg.tools["github.issue"].Execute(ctx, map[string]any{"repo": "not a repo", "number": 1}); err == nil {
		t.Fatal("bad repo accepted")
	}
	c, err := reg.tools["github.comment"].Execute(ctx, map[string]any{"repo": "o/r", "number": 7, "body": "Thanks"})
	if err != nil || c["url"] != "https://github.com/o/r/issues/7#c1" {
		t.Fatalf("comment = %+v, %v", c, err)
	}

	// Reconnecting with the token left blank keeps it.
	if st, err = m.Connect(ctx, "github", map[string]string{"api_url": srv.URL}); err != nil || !st.Connected {
		t.Fatalf("reconnect: %+v %v", st, err)
	}

	// After a restart, the tools come back.
	m2 := NewManager(m.db, sec, &memRegistry{tools: map[string]tools.Tool{}}, GitHub{})
	tools.SetConnected("github", nil)
	if err := m2.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok := tools.Lookup("github.search"); !ok {
		t.Fatal("tools not loaded after restart")
	}

	if err := m.Disconnect(ctx, "github"); err != nil {
		t.Fatal(err)
	}
	if len(sec.m) != 0 || len(reg.tools) != 0 {
		t.Fatal("disconnect left the credential or tools")
	}
	if _, ok := tools.Lookup("github.search"); ok {
		t.Fatal("tools still in the catalog")
	}
	list, _ := m.List(ctx)
	if len(list) != 2 || list[0].ID != "github" || list[0].Connected {
		t.Fatalf("list = %+v", list)
	}
	if _, err := m.Connect(ctx, "nope", nil); !errors.Is(err, ErrUnknown) {
		t.Fatalf("unknown = %v", err)
	}
}

func TestHomeAssistant(t *testing.T) {
	const haToken = "ha-long-lived-token-xyz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+haToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/config":
			_, _ = w.Write([]byte(`{"location_name":"Home"}`))
		case "/api/states":
			_, _ = w.Write([]byte(`[{"entity_id":"light.kitchen","state":"off","attributes":{"friendly_name":"Kitchen light"}},
				{"entity_id":"sensor.porch_temp","state":"12.5","attributes":{"friendly_name":"Porch","unit_of_measurement":"°C"}}]`))
		case "/api/services/light/turn_on":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["entity_id"] != "light.kitchen" {
				t.Errorf("body = %+v", body)
			}
			_, _ = w.Write([]byte(`[{"entity_id":"light.kitchen","state":"on","attributes":{"friendly_name":"Kitchen light"}}]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	m, _, reg := newManager(t)
	ctx := context.Background()
	if _, err := m.Connect(ctx, "homeassistant", map[string]string{"url": "homeassistant.local", "token": haToken}); err == nil {
		t.Fatal("address without a scheme accepted")
	}
	st, err := m.Connect(ctx, "homeassistant", map[string]string{"url": srv.URL + "/", "token": haToken})
	if err != nil || st.Account != "Home" {
		t.Fatalf("connect = %+v, %v", st, err)
	}
	// Connecting learned the device names, for tool selection.
	def, _ := tools.Lookup("homeassistant.states")
	if strings.Join(def.Cues, ",") != "kitchen light,porch" || !def.Prefetch {
		t.Fatalf("cues = %v", def.Cues)
	}
	// They survive a restart.
	tools.SetConnected("homeassistant", nil)
	if err := NewManager(m.db, m.secrets, &memRegistry{tools: map[string]tools.Tool{}}, HomeAssistant{}).Load(ctx); err != nil {
		t.Fatal(err)
	}
	if def, _ := tools.Lookup("homeassistant.call"); len(def.Cues) != 2 {
		t.Fatalf("cues after restart = %v", def.Cues)
	}
	res, err := reg.tools["homeassistant.states"].Execute(ctx, map[string]any{"filter": "kitchen"})
	if err != nil || res["matched"] != 1 {
		t.Fatalf("states = %+v, %v", res, err)
	}
	all, _ := reg.tools["homeassistant.states"].Execute(ctx, map[string]any{})
	if all["matched"] != 2 {
		t.Fatalf("all = %+v", all)
	}
	if _, err := reg.tools["homeassistant.call"].Execute(ctx, map[string]any{"domain": "light/../x", "service": "turn_on", "entity_id": "light.kitchen"}); err == nil {
		t.Fatal("bad domain accepted")
	}
	for _, args := range []map[string]any{
		{"domain": "light", "service": "turn_on", "entity_id": "light.kitchen"},
		// As small models write it.
		{"domain": "homeassistant", "service": "light.turn_on", "entity_id": "light.kitchen"},
		{"service": "turn_on", "entity_id": "light.kitchen"},
	} {
		called, err := reg.tools["homeassistant.call"].Execute(ctx, args)
		if err != nil || called["called"] != "light.turn_on" {
			t.Fatalf("call %v = %+v, %v", args, called, err)
		}
	}
}

// Only secret fields are redacted from results: the user's own address or
// server stays readable.
func TestSecretsOnly(t *testing.T) {
	cred := Credential{"address": "me@example.org", "password": "hunter2-app", "imap": "imap.example.org"}
	got := sanitize(map[string]any{"to": "me@example.org", "note": "signed in with hunter2-app"}, secretsOnly(Email{}, cred)).(map[string]any)
	if got["to"] != "me@example.org" || got["note"] != "signed in with [redacted]" {
		t.Fatalf("sanitized %v", got)
	}
}
