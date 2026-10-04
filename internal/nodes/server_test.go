package nodes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/internal/discovery"
	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

func setupInternalServer(t *testing.T) (*InternalServer, *auth.NodeIdentity, *auth.PairingManager, *auth.NodeIdentity) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	local, err := auth.LoadOrCreateIdentity(auth.NewSecretStore(dir+"/local"), "local-node")
	if err != nil {
		t.Fatal(err)
	}
	peer, err := auth.LoadOrCreateIdentity(auth.NewSecretStore(dir+"/peer"), "peer-node")
	if err != nil {
		t.Fatal(err)
	}
	pm := auth.NewPairingManager(db.SQL, local)
	cfg := config.Config{NodeID: "local-node", NodeName: "Local"}

	srv := NewInternalServer(InternalDeps{
		Config:   cfg,
		Identity: local,
		Pairing:  pm,
		ListModels: func(ctx context.Context) ([]contracts.Model, error) {
			return []contracts.Model{{ID: "m1", Installed: true}}, nil
		},
		Chat: func(ctx context.Context, req RemoteChatRequest) (<-chan pluginapi.ChatChunk, error) {
			ch := make(chan pluginapi.ChatChunk, 1)
			ch <- pluginapi.ChatChunk{Content: "hi", Done: true}
			close(ch)
			return ch, nil
		},
		Tools: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("tools " + r.URL.Path))
		}),
	})
	return srv, local, pm, peer
}

func TestAuthMiddlewareRejectsUnsigned(t *testing.T) {
	srv, _, _, _ := setupInternalServer(t)
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/models", nil)
	rr := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestAuthMiddlewareRejectsWrongPeer(t *testing.T) {
	srv, _, pm, peer := setupInternalServer(t)
	// Trust a different node, not the peer signing the request.
	otherDir := t.TempDir()
	other, err := auth.LoadOrCreateIdentity(auth.NewSecretStore(otherDir), "other-node")
	if err != nil {
		t.Fatal(err)
	}
	if err := storeTrustHelper(t, pm, other); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+peer.AuthToken("local-node"))
	rr := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestAuthMiddlewareAcceptsTrustedPeer(t *testing.T) {
	srv, _, pm, peer := setupInternalServer(t)
	if err := storeTrustHelper(t, pm, peer); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+peer.AuthToken("local-node"))
	rr := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestAuthMiddlewareAllowsPublicRoutes(t *testing.T) {
	srv, _, _, _ := setupInternalServer(t)
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/health", nil)
	rr := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d", rr.Code)
	}
}

func TestAuthMiddlewareRejectsRevoked(t *testing.T) {
	srv, _, pm, peer := setupInternalServer(t)
	if err := storeTrustHelper(t, pm, peer); err != nil {
		t.Fatal(err)
	}
	if err := pm.RevokeTrust(context.Background(), peer.NodeID); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+peer.AuthToken("local-node"))
	rr := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

// Two computers pair over Bifrost: B fetches A's signed offer by code,
// approves it, and A accepts B's signed completion.
func TestPairingOfferCompleteMutualTrust(t *testing.T) {
	ctx := context.Background()
	a, b := newPairingNode(t, "node-a"), newPairingNode(t, "node-b")
	session, err := a.pm.StartPairing("node-b", "B", "127.0.0.1:7332", b.id.CertPEM)
	if err != nil {
		t.Fatal(err)
	}

	resp := a.do(t, http.MethodGet, "/internal/v1/pairing/outbound/"+session.Code, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("outbound: %d %s", resp.Code, resp.Body.String())
	}
	var offer auth.PairingOffer
	if err := json.NewDecoder(resp.Body).Decode(&offer); err != nil {
		t.Fatal(err)
	}
	if offer.ToNodeID != "node-b" || offer.Signature == "" {
		t.Fatalf("offer=%+v", offer)
	}
	if _, err := b.pm.ReceiveOffer(offer); err != nil {
		t.Fatal(err)
	}
	if _, err := b.pm.ApproveSession(ctx, session.ID); err != nil {
		t.Fatal(err)
	}
	if !b.pm.IsTrusted(ctx, "node-a") {
		t.Fatal("B should trust A")
	}

	complete := auth.PairingComplete{
		SessionID:   session.ID,
		FromNodeID:  "node-b",
		FromName:    "B",
		FromCertPEM: string(b.id.CertPEM),
	}
	// Unsigned, as an older version or another computer would send it.
	if resp := a.do(t, http.MethodPost, "/internal/v1/pairing/complete", complete); resp.Code == http.StatusOK {
		t.Fatal("unsigned completion accepted")
	}
	b.pm.SignComplete(&complete, "node-a", session.Code)
	if resp := a.do(t, http.MethodPost, "/internal/v1/pairing/complete", complete); resp.Code != http.StatusOK {
		t.Fatalf("complete: %d %s", resp.Code, resp.Body.String())
	} else if strings.Contains(resp.Body.String(), session.Code) {
		t.Fatal("completion response repeats the code")
	}
	if !a.pm.IsTrusted(ctx, "node-b") {
		t.Fatal("A should trust B")
	}
}

// The app's claim flow across two Bifrost servers: B enters A's code,
// approves, and both computers trust each other.
func TestPairingClaimFlowOverHTTP(t *testing.T) {
	ctx := context.Background()
	a, b := newPairingNode(t, "node-a"), newPairingNode(t, "node-b")
	httpA := httptest.NewServer(a.srv.http.Handler)
	defer httpA.Close()
	addrA := strings.TrimPrefix(httpA.URL, "http://")
	a.m.SetAdvertiseAddr(func() string { return addrA })
	b.m.discovered["node-a"] = discovery.DiscoveredNode{Node: contracts.Node{ID: "node-a", Address: addrA}}

	session, err := a.pm.StartPairing("node-b", "B", "127.0.0.1:9", b.id.CertPEM)
	if err != nil {
		t.Fatal(err)
	}
	// A wrong code is not tried here: a miss falls back to the control API
	// on :7331 of the same host, which may be a real daemon.
	claimed, err := b.m.ClaimPairing(ctx, "node-a", session.Code)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.m.ApprovePairing(ctx, claimed.ID, ""); err != nil {
		t.Fatal(err)
	}
	if !b.pm.IsTrusted(ctx, "node-a") || !a.pm.IsTrusted(ctx, "node-b") {
		t.Fatalf("trust: B→A %v, A→B %v", b.pm.IsTrusted(ctx, "node-a"), a.pm.IsTrusted(ctx, "node-b"))
	}
}

// Guessing codes is throttled per source address.
func TestPairingOutboundThrottlesGuessing(t *testing.T) {
	a, b := newPairingNode(t, "node-a"), newPairingNode(t, "node-b")
	session, err := a.pm.StartPairing("node-b", "B", "127.0.0.1:7332", b.id.CertPEM)
	if err != nil {
		t.Fatal(err)
	}
	last := 0
	for i := 0; i < 20; i++ {
		code := fmt.Sprintf("%06d", i)
		if code == session.Code {
			continue
		}
		last = a.do(t, http.MethodGet, "/internal/v1/pairing/outbound/"+code, nil).Code
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("last guess: %d", last)
	}
}

// Pending offers are not listed on Bifrost; the app reads them from the
// control API.
func TestPairingPendingIsNotPublic(t *testing.T) {
	srv, _, _, _ := setupInternalServer(t)
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/pairing/pending", nil)
	rr := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rr, req)
	if rr.Code == http.StatusOK {
		t.Fatalf("pending offers listed without auth: %s", rr.Body.String())
	}
}

// Only the exact public paths skip auth; a model named "node" or "health"
// does not.
func TestPublicPathsMatchExactly(t *testing.T) {
	for path, want := range map[string]bool{
		"/internal/v1/health":                 true,
		"/internal/v1/node":                   true,
		"/internal/v1/pairing/offer":          true,
		"/internal/v1/pairing/complete":       true,
		"/internal/v1/pairing/outbound/12345": true,
		"/internal/v1/models/node":            false,
		"/internal/v1/models/health":          false,
		"/internal/v1/pairing/pending":        false,
		"/internal/v1/x/pairing/outbound/1":   false,
		"/internal/v1/pairing/outbound/1/x":   false,
	} {
		if got := isPublicInternalPath(path); got != want {
			t.Errorf("%s: public=%v, want %v", path, got, want)
		}
	}
	srv, _, _, _ := setupInternalServer(t)
	srv.deps.DeleteModel = func(ctx context.Context, id string) error {
		t.Fatal("deleted a model without auth")
		return nil
	}
	srv = NewInternalServer(srv.deps)
	req := httptest.NewRequest(http.MethodDelete, "/internal/v1/models/node", nil)
	rr := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rr.Code)
	}
}

// A token is good for one request, to the computer it was made for.
func TestAuthMiddlewareRejectsReplayAndOtherAudience(t *testing.T) {
	srv, _, pm, peer := setupInternalServer(t)
	if err := storeTrustHelper(t, pm, peer); err != nil {
		t.Fatal(err)
	}
	call := func(token string) int {
		req := httptest.NewRequest(http.MethodGet, "/internal/v1/models", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		srv.http.Handler.ServeHTTP(rr, req)
		return rr.Code
	}
	token := peer.AuthToken("local-node")
	if code := call(token); code != http.StatusOK {
		t.Fatalf("first use: %d", code)
	}
	if code := call(token); code != http.StatusUnauthorized {
		t.Fatalf("replay: %d", code)
	}
	if code := call(peer.AuthToken("other-node")); code != http.StatusUnauthorized {
		t.Fatalf("other audience: %d", code)
	}
}

type pairingNode struct {
	id  *auth.NodeIdentity
	pm  *auth.PairingManager
	m   *Manager
	srv *InternalServer
}

func newPairingNode(t *testing.T, nodeID string) pairingNode {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	id, err := auth.LoadOrCreateIdentity(auth.NewSecretStore(dir), nodeID)
	if err != nil {
		t.Fatal(err)
	}
	pm := auth.NewPairingManager(db.SQL, id)
	m := NewManager(db.SQL, nil, pm, nodeID, nodeID, nil)
	srv := NewInternalServer(InternalDeps{
		Config:          config.Config{NodeID: nodeID, NodeName: nodeID},
		Identity:        id,
		Pairing:         pm,
		ReceiveOffer:    m.ReceiveOffer,
		CompletePairing: m.CompletePairing,
		LookupOutbound:  m.LookupOutbound,
	})
	return pairingNode{id: id, pm: pm, m: m, srv: srv}
}

func (n pairingNode) do(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var r *bytes.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		r = bytes.NewReader(raw)
	} else {
		r = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, r)
	req.RemoteAddr = "10.0.0.2:5555"
	rr := httptest.NewRecorder()
	n.srv.http.Handler.ServeHTTP(rr, req)
	return rr
}

func TestRemoteChatStream(t *testing.T) {
	srv, _, pm, peer := setupInternalServer(t)
	if err := storeTrustHelper(t, pm, peer); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(RemoteChatRequest{
		ModelID:  "m1",
		Messages: []pluginapi.ChatMessage{{Role: "user", Content: "hi"}},
	})
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/chat", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+peer.AuthToken("local-node"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var chunk pluginapi.ChatChunk
	if err := json.NewDecoder(rr.Body).Decode(&chunk); err != nil {
		t.Fatal(err)
	}
	if chunk.Content != "hi" || !chunk.Done {
		t.Fatalf("chunk=%+v", chunk)
	}
}

func storeTrustHelper(t *testing.T, pm *auth.PairingManager, peer *auth.NodeIdentity) error {
	t.Helper()
	s, err := pm.StartPairing(peer.NodeID, "Peer", "127.0.0.1:9", peer.CertPEM)
	if err != nil {
		return err
	}
	_, err = pm.ApproveByCode(context.Background(), s.Code)
	return err
}

// Manual two-machine checklist:
// 1. Enable discovery on A and B; InternalHost should be 0.0.0.0.
// 2. A discovers B with LAN host:port (not 127.0.0.1).
// 3. A pairs → B shows incoming offer → Approve; both trust each other.
// 4. Install a model only on B; pin Team worker→B (Profiles Edit roles).
// 5. Chat on A with Programming (Team) profile; timeline shows worker on B.

// Paired computers reach the remote tool protocol; others are refused.
func TestToolsRouteNeedsAPairedComputer(t *testing.T) {
	srv, _, pm, peer := setupInternalServer(t)
	call := func(token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/internal/v1/tools/providers", nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rr := httptest.NewRecorder()
		srv.http.Handler.ServeHTTP(rr, req)
		return rr
	}
	if rr := call(""); rr.Code != http.StatusUnauthorized {
		t.Fatalf("unsigned: %d", rr.Code)
	}
	if rr := call(peer.AuthToken("local-node")); rr.Code == http.StatusOK {
		t.Fatal("an unpaired computer reached the tools")
	}
	if err := storeTrustHelper(t, pm, peer); err != nil {
		t.Fatal(err)
	}
	if rr := call(peer.AuthToken("local-node")); rr.Code != http.StatusOK || rr.Body.String() != "tools /tools/providers" {
		t.Fatalf("paired: %d %q", rr.Code, rr.Body.String())
	}
}
