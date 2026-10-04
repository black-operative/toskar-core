package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// streamableFake serves the fake over Streamable HTTP. Tool calls answer
// as an event stream; everything else as JSON.
type streamableFake struct {
	t        *testing.T
	f        *fake
	mu       sync.Mutex
	sessions int
	expire   bool
	deleted  bool
	auth     func(r *http.Request) bool
}

func (s *streamableFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.auth != nil && !s.auth(r) {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="http://`+r.Host+`/.well-known/oauth-protected-resource/mcp"`)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if r.Method == http.MethodDelete {
		s.mu.Lock()
		s.deleted = true
		s.mu.Unlock()
		return
	}
	if !strings.Contains(r.Header.Get("Accept"), "text/event-stream") || !strings.Contains(r.Header.Get("Accept"), "application/json") {
		s.t.Errorf("accept = %q", r.Header.Get("Accept"))
	}
	body, _ := io.ReadAll(r.Body)
	var m message
	if err := json.Unmarshal(body, &m); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	session := fmt.Sprintf("sess-%d", s.sessions)
	if m.Method == "initialize" {
		s.sessions++
		session = fmt.Sprintf("sess-%d", s.sessions)
		w.Header().Set("Mcp-Session-Id", session)
	} else {
		if r.Header.Get("Mcp-Session-Id") != session {
			s.mu.Unlock()
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if m.Method != "notifications/initialized" && r.Header.Get("MCP-Protocol-Version") != ProtocolVersion {
			s.t.Errorf("protocol header = %q", r.Header.Get("MCP-Protocol-Version"))
		}
	}
	if s.expire && m.Method == "tools/call" {
		// The server restarted and forgot every session.
		s.expire = false
		s.sessions++
		s.mu.Unlock()
		w.WriteHeader(http.StatusNotFound)
		return
	}
	s.mu.Unlock()
	if !m.isRequest() {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	res, rerr := s.f.handle(m)
	raw, _ := json.Marshal(rpcResponse(m.ID, res, rerr))
	if m.Method == "tools/call" {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, ": keep-alive\n\nevent: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{}}\n\n")
		fmt.Fprintf(w, "data: %s\n\n", raw)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)
}

func TestStreamableHTTPSource(t *testing.T) {
	sf := &streamableFake{t: t, f: &fake{}}
	srv := httptest.NewServer(sf)
	defer srv.Close()
	h := newHarness(t)
	ctx := context.Background()
	added, err := h.m.Add(ctx, AddRequest{Spec: &Spec{Name: "Remote notes", URL: srv.URL + "/mcp", Headers: map[string]string{"X-Api-Key": secretToken}}})
	if err != nil {
		t.Fatal(err)
	}
	v := added.Server
	if v.Where != "remote" || v.Status != StatusReady || len(v.Tools) != 8 {
		t.Fatalf("view = %+v", v)
	}
	if len(v.Headers) != 1 || strings.Contains(v.Headers[0].Value, "SUPERSECRET") {
		t.Fatalf("headers = %+v", v.Headers)
	}
	out, err := h.m.Call(ctx, "remote-notes", "get_weather", map[string]any{"city": "Bergen"})
	if err != nil || out["text"] != "Sunny in Bergen" {
		t.Fatalf("call = %v, %v", out, err)
	}
	// A server that forgot the session gets a new one, and the call runs.
	sf.mu.Lock()
	sf.expire = true
	sf.mu.Unlock()
	out, err = h.m.Call(ctx, "remote-notes", "get_weather", map[string]any{"city": "Tromsø"})
	if err != nil || out["text"] != "Sunny in Tromsø" {
		t.Fatalf("call after expiry = %v, %v", out, err)
	}
	if err := h.m.Remove(ctx, "remote-notes"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		sf.mu.Lock()
		defer sf.mu.Unlock()
		return sf.deleted
	})
}

// legacyFake is a 2024-11-05 HTTP+SSE server.
type legacyFake struct {
	f      *fake
	stream chan []byte
}

func (s *legacyFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/sse":
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "event: endpoint\ndata: /messages?session=1\n\n")
		w.(http.Flusher).Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case msg := <-s.stream:
				fmt.Fprintf(w, "event: message\ndata: %s\n\n", msg)
				w.(http.Flusher).Flush()
			}
		}
	case r.Method == http.MethodPost && r.URL.Path == "/messages":
		body, _ := io.ReadAll(r.Body)
		var m message
		_ = json.Unmarshal(body, &m)
		w.WriteHeader(http.StatusAccepted)
		if m.isRequest() {
			res, rerr := s.f.handle(m)
			raw, _ := json.Marshal(rpcResponse(m.ID, res, rerr))
			go func() { s.stream <- raw }()
		}
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func TestLegacySSESource(t *testing.T) {
	srv := httptest.NewServer(&legacyFake{f: &fake{}, stream: make(chan []byte, 8)})
	// Cleanups run last first: the manager hangs up the event stream
	// before the server closes.
	t.Cleanup(srv.Close)
	h := newHarness(t)
	ctx := context.Background()
	if _, err := h.m.Add(ctx, AddRequest{Spec: &Spec{Name: "Old server", URL: srv.URL + "/sse"}}); err != nil {
		t.Fatal(err)
	}
	out, err := h.m.Call(ctx, "old-server", "get_weather", map[string]any{"city": "Nome"})
	if err != nil || out["text"] != "Sunny in Nome" {
		t.Fatalf("call = %v, %v", out, err)
	}
}

func TestSignInWithOAuth(t *testing.T) {
	var mu sync.Mutex
	var challenge, gotResource string
	mux := http.NewServeMux()
	sf := &streamableFake{t: t, f: &fake{}, auth: func(r *http.Request) bool {
		return r.Header.Get("Authorization") == "Bearer good-token"
	}}
	mux.Handle("/mcp", sf)
	mux.HandleFunc("/.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"resource": "http://" + r.Host + "/mcp", "authorization_servers": []string{"http://" + r.Host + "/auth"}})
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server/auth", func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host + "/auth"
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": base, "authorization_endpoint": base + "/authorize",
			"token_endpoint": base + "/token", "registration_endpoint": base + "/register", "code_challenge_methods_supported": []string{"S256"}})
	})
	mux.HandleFunc("/auth/register", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if uris, _ := body["redirect_uris"].([]any); len(uris) != 1 || uris[0] != "http://127.0.0.1:7331"+CallbackPath {
			t.Errorf("redirect_uris = %v", body["redirect_uris"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"client_id": "cid-1"})
	})
	mux.HandleFunc("/auth/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		mu.Lock()
		ok := base64.RawURLEncoding.EncodeToString(sum[:]) == challenge
		gotResource = r.Form.Get("resource")
		mu.Unlock()
		if r.Form.Get("code") != "the-code" || r.Form.Get("client_id") != "cid-1" || !ok {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_grant"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "good-token", "refresh_token": "r-1", "expires_in": 3600})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	h := newHarness(t)
	ctx := context.Background()
	added, err := h.m.Add(ctx, AddRequest{Spec: &Spec{Name: "Cloud notes", URL: srv.URL + "/mcp"}, RedirectBase: "http://127.0.0.1:7331"})
	if err != nil {
		t.Fatal(err)
	}
	if added.Server.Status != StatusSignIn || added.SignInURL == "" {
		t.Fatalf("added = %+v", added)
	}
	link, _ := url.Parse(added.SignInURL)
	q := link.Query()
	if link.Path != "/auth/authorize" || q.Get("client_id") != "cid-1" || q.Get("code_challenge_method") != "S256" || q.Get("resource") != srv.URL+"/mcp" {
		t.Fatalf("sign-in link = %s", added.SignInURL)
	}
	mu.Lock()
	challenge = q.Get("code_challenge")
	mu.Unlock()

	if _, err := h.m.FinishSignIn(ctx, "wrong-state", "the-code", "", ""); err == nil {
		t.Fatal("a wrong state was accepted")
	}
	name, err := h.m.FinishSignIn(ctx, q.Get("state"), "the-code", "", "")
	if err != nil || name != "Cloud notes" {
		t.Fatalf("finish = %q, %v", name, err)
	}
	if gotResource != srv.URL+"/mcp" {
		t.Fatalf("token resource = %q", gotResource)
	}
	v, _ := h.m.Get("cloud-notes")
	if v.Status != StatusReady || !v.SignedIn || len(v.Tools) != 8 {
		t.Fatalf("after sign-in = %+v", v)
	}
	out, err := h.m.Call(ctx, "cloud-notes", "get_weather", map[string]any{"city": "Sitka"})
	if err != nil || out["text"] != "Sunny in Sitka" {
		t.Fatalf("call = %v, %v", out, err)
	}
	var row string
	_ = h.db.SQL.QueryRow(`SELECT spec || COALESCE(info, '') FROM mcp_servers WHERE id = 'cloud-notes'`).Scan(&row)
	if strings.Contains(row, "good-token") || strings.Contains(row, "r-1") {
		t.Fatal("token stored in SQLite")
	}
	// The state is single use.
	if _, err := h.m.FinishSignIn(ctx, q.Get("state"), "the-code", "", ""); err == nil {
		t.Fatal("a state was used twice")
	}
	v, err = h.m.SignOut(ctx, "cloud-notes")
	if err != nil || v.SignedIn || v.Status != StatusSignIn {
		t.Fatalf("sign out = %+v, %v", v, err)
	}
}

func TestYggdrasilServerOverOwnClient(t *testing.T) {
	var askedModel string
	s := NewServer(Backend{
		Ask: func(ctx context.Context, prompt, model string) (string, error) {
			askedModel = model
			return "Local answer to: " + prompt, nil
		},
		Models: func(ctx context.Context) ([]ModelInfo, error) {
			return []ModelInfo{{ID: "auto", Name: "Auto"}, {ID: "qwen3-8b", Name: "Qwen3 8B"}}, nil
		},
		Search: func(ctx context.Context, q string, limit int) ([]Passage, error) {
			return []Passage{{Source: "Handbook", Title: "Leave", Text: "20 days a year."}}, nil
		},
		Authorize: func(r *http.Request) (context.Context, error) {
			if r.Header.Get("Authorization") == "Bearer bad" {
				return nil, errors.New("invalid key")
			}
			return r.Context(), nil
		},
		Version: "9.9.9",
	})
	srv := httptest.NewServer(s)
	defer srv.Close()
	ctx := context.Background()
	c, err := DialHTTP(ctx, srv.URL, HTTPOptions{}, Hooks{}, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.Server.Name != "toskar" || c.Server.Version != "9.9.9" || !c.HasTools {
		t.Fatalf("server = %+v", c.Server)
	}
	list, err := c.ListTools(ctx)
	if err != nil || len(list) != 3 {
		t.Fatalf("tools = %v, %v", list, err)
	}
	res, err := c.CallTool(ctx, "ask_local_ai", map[string]any{"prompt": "What is 2+2?", "model": "qwen3-8b"})
	if err != nil || res.IsError || res.Content[0].Text != "Local answer to: What is 2+2?" || askedModel != "qwen3-8b" {
		t.Fatalf("ask = %+v, %v", res, err)
	}
	res, _ = c.CallTool(ctx, "search_my_knowledge", map[string]any{"query": "leave"})
	if !strings.Contains(res.Content[0].Text, "Handbook — Leave") {
		t.Fatalf("search = %+v", res)
	}
	res, _ = c.CallTool(ctx, "ask_local_ai", map[string]any{})
	if !res.IsError {
		t.Fatal("a call without a prompt did not fail")
	}
	if _, err := c.CallTool(ctx, "nope", nil); err == nil {
		t.Fatal("an unknown tool did not fail")
	}

	// A web page may not reach it through a browser.
	req, _ := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	req.Header.Set("Origin", "https://evil.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign origin status = %d", resp.StatusCode)
	}
	req, _ = http.NewRequest(http.MethodPost, srv.URL, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	req.Header.Set("Authorization", "Bearer bad")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad key status = %d", resp.StatusCode)
	}
	resp, _ = http.Post(srv.URL, "application/json", strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("notification status = %d", resp.StatusCode)
	}
}
