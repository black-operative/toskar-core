package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/internal/tools"
)

const secretToken = "tok_SUPERSECRET_12345678"

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

type memRegistry struct {
	mu       sync.Mutex
	tools    map[string]tools.Tool
	disabled map[string]bool
}

func (r *memRegistry) Register(t tools.Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.ID()] = t
}

func (r *memRegistry) Unregister(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tools, id)
}

func (r *memRegistry) IsDisabled(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.disabled[id]
}

func (r *memRegistry) get(id string) tools.Tool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.tools[id]
}

type harness struct {
	m   *Manager
	sec *memSecrets
	reg *memRegistry
	db  *store.DB
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	h := &harness{sec: &memSecrets{m: map[string]string{}}, reg: &memRegistry{tools: map[string]tools.Tool{}, disabled: map[string]bool{}}, db: db}
	h.m = NewManager(db.SQL, h.sec, h.reg, "test", nil)
	h.m.ConnectTimeout = 20 * time.Second
	t.Cleanup(func() {
		h.m.closeAll()
		for _, s := range h.m.List(context.Background()) {
			tools.SetConnected("mcp:"+s.ID, nil)
		}
	})
	return h
}

func TestStdioSourceAddsToolsAndKeepsSecretsOut(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	spec := fakeStdioSpec("Notes app", map[string]string{"API_TOKEN": secretToken, "REGION": "eu"})
	added, err := h.m.Add(ctx, AddRequest{Spec: &spec})
	if err != nil {
		t.Fatal(err)
	}
	v := added.Server
	if v.ID != "notes-app" || v.Status != StatusReady || !v.Running || v.ServerName != "fake-server" {
		t.Fatalf("view = %+v", v)
	}
	// Two pages of tools, plus the resource tools.
	if len(v.Tools) != 8 {
		t.Fatalf("tools = %d, want 8: %+v", len(v.Tools), v.Tools)
	}
	byID := map[string]ToolView{}
	for _, tv := range v.Tools {
		byID[tv.ID] = tv
	}
	if tv := byID["notes-app.get_weather"]; tv.Risk != tools.RiskRead || tv.Policy != tools.PolicyAllow {
		t.Fatalf("get_weather = %+v", tv)
	}
	if tv := byID["notes-app.create_note"]; tv.Risk != tools.RiskWrite || tv.Policy != tools.PolicyAsk {
		t.Fatalf("create_note = %+v", tv)
	}
	for _, e := range v.Env {
		if e.Key == "API_TOKEN" && (!e.Secret || strings.Contains(e.Value, "SUPERSECRET")) {
			t.Fatalf("token shown: %+v", e)
		}
		if e.Key == "REGION" && e.Value != "eu" {
			t.Fatalf("plain value hidden: %+v", e)
		}
	}

	// The catalog and the registry have the tools, with the compact schema.
	def, ok := tools.Lookup("notes-app.get_weather")
	if !ok || def.Source != "mcp:notes-app" || def.Schema != `{"city":"string","units?":"c|f"}` {
		t.Fatalf("catalog def = %+v %v", def, ok)
	}
	if !containsStr(def.Cues, "notes app") || !containsStr(def.Cues, "notes") {
		t.Fatalf("cues = %v", def.Cues)
	}

	// The secret is in the secret store, not SQLite.
	var row string
	if err := h.db.SQL.QueryRow(`SELECT spec FROM mcp_servers WHERE id = 'notes-app'`).Scan(&row); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(row, secretToken) {
		t.Fatalf("secret stored in SQLite: %s", row)
	}
	if !strings.Contains(h.sec.m[secretName("notes-app")], secretToken) {
		t.Fatal("secret not in the secret store")
	}

	// Calls run through the registry; results are scrubbed of secrets.
	out, err := h.reg.get("notes-app.get_weather").Execute(ctx, map[string]any{"city": "Juneau"})
	if err != nil || out["text"] != "Sunny in Juneau" {
		t.Fatalf("get_weather = %v, %v", out, err)
	}
	out, err = h.reg.get("notes-app.echo_secret").Execute(ctx, nil)
	if err != nil || strings.Contains(out["text"].(string), secretToken) || !strings.Contains(out["text"].(string), "[redacted]") {
		t.Fatalf("echo_secret = %v, %v", out, err)
	}
	out, err = h.reg.get("notes-app.create_note").Execute(ctx, map[string]any{"title": "Groceries"})
	if err != nil {
		t.Fatal(err)
	}
	if out["text"] != "Created Groceries" || out["note"] == nil {
		t.Fatalf("create_note = %v", out)
	}
	if links := out["results"].([]any); len(links) != 1 || links[0].(map[string]any)["url"] != "https://notes.example/1" {
		t.Fatalf("links = %v", out["results"])
	}
	if _, err := h.reg.get("notes-app.fail").Execute(ctx, nil); err == nil || err.Error() != "boom" {
		t.Fatalf("fail = %v", err)
	}
	out, err = h.reg.get("notes-app.read_resource").Execute(ctx, map[string]any{"uri": "note://1"})
	if err != nil || out["text"] != "hello resource" {
		t.Fatalf("read_resource = %v, %v", out, err)
	}
	text, err := h.m.GetPrompt(ctx, "notes-app", "summarize", map[string]string{"topic": "rivers"})
	if err != nil || text != "Summarize rivers" {
		t.Fatalf("prompt = %q, %v", text, err)
	}

	// The server's stderr and log messages reach the log.
	waitFor(t, func() bool {
		lines, _ := h.m.Logs("notes-app")
		return hasLog(lines, "fake server log line") && hasLog(lines, "ready to serve")
	})

	// Removing deletes the tools and the secret.
	if err := h.m.Remove(ctx, "notes-app"); err != nil {
		t.Fatal(err)
	}
	if _, ok := tools.Lookup("notes-app.get_weather"); ok || h.reg.get("notes-app.get_weather") != nil {
		t.Fatal("tools still registered")
	}
	if _, ok := h.sec.m[secretName("notes-app")]; ok {
		t.Fatal("secret not deleted")
	}
}

func TestSourceStartsWhenNeededAndStopsWhenIdle(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	spec := fakeStdioSpec("Weather", nil)
	if _, err := h.m.Add(ctx, AddRequest{Spec: &spec}); err != nil {
		t.Fatal(err)
	}
	h.m.IdleAfter = 0
	h.m.stopIdle()
	if v, _ := h.m.Get("weather"); v.Running {
		t.Fatal("still running after idle")
	}

	// A new manager registers the kept tool list without starting anything.
	m2 := NewManager(h.db.SQL, h.sec, h.reg, "test", nil)
	if err := m2.Load(ctx); err != nil {
		t.Fatal(err)
	}
	defer m2.closeAll()
	if v, _ := m2.Get("weather"); v.Running || len(v.Tools) != 8 {
		t.Fatalf("after load: running=%v tools=%d", v.Running, len(v.Tools))
	}
	// The first call starts it.
	out, err := m2.Call(ctx, "weather", "get_weather", map[string]any{"city": "Oslo"})
	if err != nil || out["text"] != "Sunny in Oslo" {
		t.Fatalf("call = %v, %v", out, err)
	}
	if v, _ := m2.Get("weather"); !v.Running {
		t.Fatal("not running after a call")
	}
}

func TestRootsAndSampling(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	dir := t.TempDir()
	spec := fakeStdioSpec("Roots", nil)
	spec.Args = append(spec.Args, dir)
	if _, err := h.m.Add(ctx, AddRequest{Spec: &spec}); err != nil {
		t.Fatal(err)
	}
	out, err := h.m.Call(ctx, "roots", "list_roots", nil)
	if err != nil || !strings.Contains(out["text"].(string), "file://") {
		t.Fatalf("roots = %v, %v", out, err)
	}
	// Sampling is off until the person allows it.
	if _, err := h.m.Call(ctx, "roots", "ask_ai", nil); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("sampling while off = %v", err)
	}
	h.m.Sample = func(ctx context.Context, system string, msgs []SampleMessage, maxTokens int) (string, string, error) {
		return "hi from " + msgs[0].Text, "tiny-model", nil
	}
	on := true
	if _, err := h.m.Update(ctx, "roots", Update{AllowSampling: &on}); err != nil {
		t.Fatal(err)
	}
	out, err = h.m.Call(ctx, "roots", "ask_ai", nil)
	if err != nil || out["text"] != "tiny-model: hi from Say hi" {
		t.Fatalf("sampling = %v, %v", out, err)
	}
}

func TestUpdatePoliciesAndOff(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	spec := fakeStdioSpec("Notes", nil)
	if _, err := h.m.Add(ctx, AddRequest{Spec: &spec}); err != nil {
		t.Fatal(err)
	}
	always := true
	v, err := h.m.Update(ctx, "notes", Update{Policies: map[string]string{"create_note": tools.PolicyAllow}, AlwaysOffer: &always})
	if err != nil {
		t.Fatal(err)
	}
	def, _ := tools.Lookup("notes.create_note")
	if def.DefaultPolicy != tools.PolicyAllow || !def.Always {
		t.Fatalf("def = %+v", def)
	}
	for _, tv := range v.Tools {
		if tv.Remote == "create_note" && !tv.Changed {
			t.Fatal("changed policy not marked")
		}
	}
	if _, err := h.m.Update(ctx, "notes", Update{Policies: map[string]string{"create_note": "deny"}}); err == nil {
		t.Fatal("deny accepted as a policy")
	}
	off := false
	if v, err = h.m.Update(ctx, "notes", Update{Enabled: &off}); err != nil || v.Status != StatusOff {
		t.Fatalf("off = %+v, %v", v.Status, err)
	}
	if _, ok := tools.Lookup("notes.get_weather"); ok {
		t.Fatal("tools of a source that is off are still in the catalog")
	}
	if _, err := h.m.Call(ctx, "notes", "get_weather", nil); err == nil {
		t.Fatal("a source that is off ran a tool")
	}
}

func TestAddFailsPlainlyAndStoresNothing(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	_, err := h.m.Add(ctx, AddRequest{Spec: &Spec{Name: "Missing", Command: "definitely-not-a-real-command-xyz"}})
	if err == nil || !strings.Contains(err.Error(), "not found on this computer") {
		t.Fatalf("err = %v", err)
	}
	_, err = h.m.Add(ctx, AddRequest{Spec: &Spec{Name: "Placeholder", Command: "npx", Env: map[string]string{"API_KEY": "<your-api-key>"}}})
	if err == nil || !strings.Contains(err.Error(), "still needed: API_KEY") {
		t.Fatalf("placeholder err = %v", err)
	}
	if len(h.m.List(ctx)) != 0 || len(h.sec.m) != 0 {
		t.Fatal("a failed add stored something")
	}
}

func TestUniqueIDsAvoidBuiltInNames(t *testing.T) {
	m := &Manager{servers: map[string]*server{}}
	if id := m.uniqueIDLocked(Spec{Name: "Filesystem"}); id != "filesystem-mcp" {
		t.Fatalf("id = %q", id)
	}
	m.servers["linear"] = &server{}
	if id := m.uniqueIDLocked(Spec{Name: "Linear", Preset: "linear"}); id != "linear-2" {
		t.Fatalf("id = %q", id)
	}
	if id := m.uniqueIDLocked(Spec{Name: "My Cool Server!"}); id != "my-cool-server" {
		t.Fatalf("id = %q", id)
	}
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func hasLog(lines []LogLine, text string) bool {
	for _, l := range lines {
		if strings.Contains(l.Text, text) {
			return true
		}
	}
	return false
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}
