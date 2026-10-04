package join

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/store"
)

type issuer struct {
	acc      *Acceptor
	srv      *httptest.Server
	tokens   *Tokens
	pub      ed25519.PublicKey
	mu       sync.Mutex
	admitted []JoiningNode
	refused  []string
}

func newIssuer(t *testing.T) *issuer {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	is := &issuer{tokens: &Tokens{DB: db.SQL}, pub: pub}
	is.acc = &Acceptor{
		Tokens: is.tokens, NodeID: "issuer", Name: func() string { return "studio" }, Key: priv,
		NetworkID: func(context.Context) (string, error) { return "net-1", nil },
		Address:   func() string { return "192.168.1.10:7332" },
		Admit: func(_ context.Context, n JoiningNode) (string, error) {
			is.mu.Lock()
			defer is.mu.Unlock()
			is.admitted = append(is.admitted, n)
			return n.Name, nil
		},
		Refused: func(_ context.Context, reason, _, _ string) {
			is.mu.Lock()
			defer is.mu.Unlock()
			is.refused = append(is.refused, reason)
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+HelloPath, is.acc.Hello)
	mux.HandleFunc("POST "+JoinPath, is.acc.Join)
	is.srv = httptest.NewServer(mux)
	t.Cleanup(is.srv.Close)
	return is
}

func joiner(t *testing.T, is *issuer, token string) *Client {
	t.Helper()
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	return &Client{
		Server: strings.TrimPrefix(is.srv.URL, "http://"), Token: token, Fingerprint: Fingerprint(is.pub),
		Node: JoiningNode{ID: "gpu-box-id", Name: "gpu-box", PublicKeyPEM: publicKeyPEM(pub), Address: "192.168.1.22:7332"},
	}
}

func TestJoin(t *testing.T) {
	is := newIssuer(t)
	ctx := context.Background()
	raw, tok, err := is.tokens.Create(ctx, 0)
	if err != nil || !strings.HasPrefix(raw, TokenPrefix) || tok.Status != "active" || !tok.ExpiresAt.Equal(tok.CreatedAt.Add(DefaultTTL)) {
		t.Fatalf("Create = %q, %+v, %v", raw, tok, err)
	}
	var stored string
	_ = is.tokens.DB.QueryRow(`SELECT proof_key FROM join_tokens`).Scan(&stored)
	if strings.Contains(raw, stored) || strings.Contains(stored, raw[len(TokenPrefix)+9:]) {
		t.Fatal("the token's secret is stored")
	}

	c := joiner(t, is, raw)
	acc, hello, err := c.Join(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if acc.NetworkID != "net-1" || acc.Server.ID != "issuer" || acc.Server.Address != "192.168.1.10:7332" || acc.Name != "gpu-box" || hello.Name != "studio" {
		t.Fatalf("accepted %+v hello %+v", acc, hello)
	}
	if len(is.admitted) != 1 || is.admitted[0].PublicKeyPEM != c.Node.PublicKeyPEM {
		t.Fatalf("admitted %+v", is.admitted)
	}
	list, _ := is.tokens.List(ctx)
	if len(list) != 1 || list[0].Status != "used" || list[0].UsedBy != "gpu-box" {
		t.Fatalf("list = %+v", list)
	}
	// Once used, the token says so, and nobody else gets in.
	_, _, err = joiner(t, is, raw).Join(ctx)
	if !errors.Is(err, ErrUsed) || len(is.admitted) != 1 {
		t.Fatalf("second use err = %v, admitted %d", err, len(is.admitted))
	}
}

func TestJoinRefusals(t *testing.T) {
	is := newIssuer(t)
	ctx := context.Background()

	// Another computer at the address: stopped before the proof.
	raw, _, _ := is.tokens.Create(ctx, 0)
	c := joiner(t, is, raw)
	other, _, _ := ed25519.GenerateKey(rand.Reader)
	c.Fingerprint = Fingerprint(other)
	var wrong *WrongServerError
	if _, _, err := c.Join(ctx); !errors.As(err, &wrong) || !strings.Contains(err.Error(), "nothing was sent that proves the token") {
		t.Fatalf("wrong server err = %v", err)
	}
	if len(is.admitted) != 0 {
		t.Fatal("joined the wrong server")
	}

	// A wrong secret with a real ID is just invalid.
	id, _, _ := ParseToken(raw)
	forged := TokenPrefix + id + "_" + strings.Repeat("a", 32)
	if _, _, err := joiner(t, is, forged).Join(ctx); !errors.Is(err, ErrInvalid) {
		t.Fatalf("forged err = %v", err)
	}
	// So is an unknown ID, with the same answer.
	if _, _, err := joiner(t, is, TokenPrefix+"bbbbbbbb_"+strings.Repeat("c", 32)).Join(ctx); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown err = %v", err)
	}

	// Revoked and expired tokens, once proven, say why.
	revoked, rt, _ := is.tokens.Create(ctx, 0)
	if _, err := is.tokens.Revoke(ctx, rt.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := joiner(t, is, revoked).Join(ctx); !errors.Is(err, ErrRevoked) {
		t.Fatalf("revoked err = %v", err)
	}
	if _, err := is.tokens.Revoke(ctx, rt.ID); !errors.Is(err, ErrRevoked) {
		t.Fatalf("revoking twice err = %v", err)
	}
	now := time.Now()
	is.tokens.Now = func() time.Time { return now.Add(-time.Hour) }
	expired, _, _ := is.tokens.Create(ctx, time.Minute)
	is.tokens.Now = nil
	if _, _, err := joiner(t, is, expired).Join(ctx); !errors.Is(err, ErrExpired) {
		t.Fatalf("expired err = %v", err)
	}
	if len(is.admitted) != 0 {
		t.Fatalf("admitted %+v", is.admitted)
	}
	if _, _, err := is.tokens.Create(ctx, 25*time.Hour); err == nil {
		t.Fatal("a token longer than a day was made")
	}
}

// relay changes the joining computer's key on the way.
type relay struct {
	target string
	key    string
}

func (r relay) RoundTrip(req *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	if strings.HasSuffix(req.URL.Path, JoinPath) {
		var j JoinRequest
		_ = json.Unmarshal(body, &j)
		j.Node.PublicKeyPEM = r.key
		body, _ = json.Marshal(j)
	}
	out, _ := http.NewRequest(req.Method, r.target+req.URL.Path, bytes.NewReader(body))
	out.Header = req.Header
	return http.DefaultTransport.RoundTrip(out)
}

func TestRelayCannotSwapTheKey(t *testing.T) {
	is := newIssuer(t)
	ctx := context.Background()
	raw, _, _ := is.tokens.Create(ctx, 0)
	c := joiner(t, is, raw)
	mallory, _, _ := ed25519.GenerateKey(rand.Reader)
	c.HTTP = &http.Client{Transport: relay{target: is.srv.URL, key: publicKeyPEM(mallory)}}
	if _, _, err := c.Join(ctx); !errors.Is(err, ErrInvalid) {
		t.Fatalf("relayed err = %v", err)
	}
	if len(is.admitted) != 0 {
		t.Fatal("the relay's key was trusted")
	}
	// The token was not used up, so the real join still works.
	if _, _, err := joiner(t, is, raw).Join(ctx); err != nil {
		t.Fatalf("join after the relay: %v", err)
	}
}

func TestOneTokenOneComputer(t *testing.T) {
	is := newIssuer(t)
	ctx := context.Background()
	raw, _, _ := is.tokens.Create(ctx, 0)
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, errs[i] = joiner(t, is, raw).Join(ctx)
		}(i)
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		if err == nil {
			ok++
		}
	}
	if ok != 1 || len(is.admitted) != 1 {
		t.Fatalf("%d joins succeeded, %d admitted: %v", ok, len(is.admitted), errs)
	}
}

func TestRateLimit(t *testing.T) {
	is := newIssuer(t)
	ctx := context.Background()
	var err error
	for range failLimit + 1 {
		_, _, err = joiner(t, is, TokenPrefix+"bbbbbbbb_"+strings.Repeat("c", 32)).Join(ctx)
	}
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("after %d failures err = %v", failLimit+1, err)
	}
	// Even a good token waits now.
	raw, _, _ := is.tokens.Create(ctx, 0)
	if _, _, err := joiner(t, is, raw).Join(ctx); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("good token while limited err = %v", err)
	}
}

func TestParsing(t *testing.T) {
	for _, s := range []string{"", "ygj_", "ygj_abc", "abc_def", "ygj_bbbbbbbb_" + strings.Repeat("1", 32)} {
		if _, _, err := ParseToken(s); err == nil {
			t.Errorf("ParseToken(%q) accepted", s)
		}
	}
	for in, want := range map[string]string{"10.0.0.5": "10.0.0.5:7332", "http://10.0.0.5:7332/": "10.0.0.5:7332", "gpu.lan:9000": "gpu.lan:9000", "::1": "[::1]:7332"} {
		if got, err := NormalizeServer(in); err != nil || got != want {
			t.Errorf("NormalizeServer(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	fp := strings.Repeat("AB", 32)
	if got, err := NormalizeFingerprint(fp); err != nil || got != "sha256:"+strings.ToLower(fp) {
		t.Errorf("NormalizeFingerprint = %q, %v", got, err)
	}
	if _, err := NormalizeFingerprint("sha256:abc"); err == nil {
		t.Error("a short fingerprint was accepted")
	}
}
