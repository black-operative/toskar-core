package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/store"
)

type pairingPeer struct {
	id *NodeIdentity
	pm *PairingManager
	db *store.DB
}

func newPairingPeer(t *testing.T, nodeID string) pairingPeer {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	id, err := LoadOrCreateIdentity(NewSecretStore(dir), nodeID)
	if err != nil {
		t.Fatal(err)
	}
	return pairingPeer{id: id, pm: NewPairingManager(db.SQL, id), db: db}
}

// offerFrom starts pairing from a toward b and returns a's signed offer.
func offerFrom(t *testing.T, a, b pairingPeer) (*PairingSession, PairingOffer) {
	t.Helper()
	s, err := a.pm.StartPairing(b.id.NodeID, "B", "127.0.0.1:7332", b.id.CertPEM)
	if err != nil {
		t.Fatal(err)
	}
	o := PairingOffer{
		SessionID:   s.ID,
		FromNodeID:  a.id.NodeID,
		ToNodeID:    b.id.NodeID,
		FromName:    "A",
		FromCertPEM: string(a.id.CertPEM),
		Code:        s.Code,
		ExpiresAt:   s.ExpiresAt.Format(time.RFC3339),
	}
	a.pm.SignOffer(&o)
	return s, o
}

func completeFrom(b pairingPeer, toNodeID, sessionID, code string) PairingComplete {
	c := PairingComplete{
		SessionID:   sessionID,
		FromNodeID:  b.id.NodeID,
		FromName:    "B",
		FromCertPEM: string(b.id.CertPEM),
	}
	b.pm.SignComplete(&c, toNodeID, code)
	return c
}

func TestPairingStateMachine(t *testing.T) {
	a, b := newPairingPeer(t, "local-node"), newPairingPeer(t, "remote-1")
	s, err := a.pm.StartPairing("remote-1", "Remote PC", "192.168.1.10:7332", b.id.CertPEM)
	if err != nil {
		t.Fatal(err)
	}
	if s.State != PairingPending {
		t.Fatalf("state=%q", s.State)
	}
	if !validCode(s.Code) {
		t.Fatalf("code=%q", s.Code)
	}

	approved, err := a.pm.ApproveByCode(context.Background(), s.Code)
	if err != nil {
		t.Fatal(err)
	}
	if approved.State != PairingApproved {
		t.Fatalf("state=%q", approved.State)
	}

	if err := a.pm.RevokeTrust(context.Background(), "remote-1"); err != nil {
		t.Fatal(err)
	}
}

func TestPairingRejectInvalidCode(t *testing.T) {
	a := newPairingPeer(t, "local")
	if _, err := a.pm.ApproveByCode(context.Background(), "000000"); err == nil {
		t.Fatal("expected error for invalid code")
	}
}

// The full exchange: a signed offer, approval, and a signed completion.
func TestPairingSignedOfferAndCompletion(t *testing.T) {
	a, b := newPairingPeer(t, "node-a"), newPairingPeer(t, "node-b")
	ctx := context.Background()
	s, offer := offerFrom(t, a, b)
	if _, err := b.pm.ReceiveOffer(offer); err != nil {
		t.Fatal(err)
	}
	if _, err := b.pm.ApproveSession(ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	if !b.pm.IsTrusted(ctx, "node-a") {
		t.Fatal("B should trust A")
	}
	if _, err := a.pm.CompleteFromPeer(ctx, "10.0.0.2", completeFrom(b, "node-a", s.ID, s.Code)); err != nil {
		t.Fatal(err)
	}
	if !a.pm.IsTrusted(ctx, "node-b") {
		t.Fatal("A should trust B")
	}
	// A completed session cannot be completed again.
	if _, err := a.pm.CompleteFromPeer(ctx, "10.0.0.2", completeFrom(b, "node-a", s.ID, s.Code)); err == nil {
		t.Fatal("second completion accepted")
	}
}

func TestReceiveOfferRefusesUnsignedOrMisdirected(t *testing.T) {
	a, b, c := newPairingPeer(t, "node-a"), newPairingPeer(t, "node-b"), newPairingPeer(t, "node-c")
	_, offer := offerFrom(t, a, b)

	unsigned := offer
	unsigned.Signature = ""
	if _, err := b.pm.ReceiveOffer(unsigned); err == nil {
		t.Fatal("unsigned offer accepted")
	}
	swapped := offer
	swapped.FromCertPEM = string(c.id.CertPEM)
	if _, err := b.pm.ReceiveOffer(swapped); err == nil {
		t.Fatal("offer with a substituted key accepted")
	}
	recode := offer
	recode.Code = "123456"
	if recode.Code == offer.Code {
		recode.Code = "654321"
	}
	if _, err := b.pm.ReceiveOffer(recode); err == nil {
		t.Fatal("offer with a changed code accepted")
	}
	if _, err := c.pm.ReceiveOffer(offer); err == nil {
		t.Fatal("offer for B accepted by C")
	}
	if _, err := b.pm.ReceiveOffer(offer); err != nil {
		t.Fatalf("genuine offer: %v", err)
	}
	// Delivered twice (Bifrost and the control API) is fine.
	if _, err := b.pm.ReceiveOffer(offer); err != nil {
		t.Fatalf("repeat delivery: %v", err)
	}
}

// Another computer cannot complete a session it was not invited to, even
// knowing the session and the code.
func TestCompleteRequiresTheInvitedKeyAndCode(t *testing.T) {
	a, b, m := newPairingPeer(t, "node-a"), newPairingPeer(t, "node-b"), newPairingPeer(t, "node-m")
	ctx := context.Background()
	s, _ := offerFrom(t, a, b)

	// Signed by another key, claiming to be B.
	forged := completeFrom(m, "node-a", s.ID, s.Code)
	forged.FromNodeID = "node-b"
	m.pm.SignComplete(&forged, "node-a", s.Code)
	if _, err := a.pm.CompleteFromPeer(ctx, "10.0.0.9", forged); err == nil {
		t.Fatal("completion signed by another key accepted")
	}
	// B's key, wrong code.
	wrong := "000000"
	if s.Code == wrong {
		wrong = "111111"
	}
	if _, err := a.pm.CompleteFromPeer(ctx, "10.0.0.2", completeFrom(b, "node-a", s.ID, wrong)); err == nil {
		t.Fatal("completion with the wrong code accepted")
	}
	// B's key, signed for a different computer.
	if _, err := a.pm.CompleteFromPeer(ctx, "10.0.0.2", completeFrom(b, "node-x", s.ID, s.Code)); err == nil {
		t.Fatal("completion signed for another computer accepted")
	}
	if a.pm.IsTrusted(ctx, "node-b") || a.pm.IsTrusted(ctx, "node-m") {
		t.Fatal("trust stored after failed completions")
	}
	if _, err := a.pm.CompleteFromPeer(ctx, "10.0.0.2", completeFrom(b, "node-a", s.ID, s.Code)); err != nil {
		t.Fatalf("genuine completion: %v", err)
	}
}

func TestCompleteFailuresExpireTheSession(t *testing.T) {
	a, b := newPairingPeer(t, "node-a"), newPairingPeer(t, "node-b")
	ctx := context.Background()
	s, _ := offerFrom(t, a, b)
	bad := completeFrom(b, "node-a", s.ID, s.Code)
	bad.Signature = "AAAA"
	for i := 0; i < maxCompleteFailures; i++ {
		// A different source each time, so only the session limit applies.
		_, _ = a.pm.CompleteFromPeer(ctx, string(rune('a'+i)), bad)
	}
	if _, err := a.pm.CompleteFromPeer(ctx, "10.0.0.2", completeFrom(b, "node-a", s.ID, s.Code)); err == nil {
		t.Fatal("session survived repeated failed completions")
	}
	if got, _ := a.pm.GetSession(s.ID); got.State != PairingExpired {
		t.Fatalf("state=%q", got.State)
	}
}

func TestOutboundByCodeLimitsGuessing(t *testing.T) {
	a, b := newPairingPeer(t, "node-a"), newPairingPeer(t, "node-b")
	s, _ := offerFrom(t, a, b)
	wrong := func(i int) string {
		c := []byte("100000")
		c[5] = byte('0' + i%10)
		c[4] = byte('0' + i/10)
		if string(c) == s.Code {
			c[0] = '2'
		}
		return string(c)
	}

	// One source is throttled after repeated misses.
	for i := 0; i < 9; i++ {
		_, _ = a.pm.OutboundByCode("10.0.0.66", wrong(i))
	}
	if got, err := a.pm.OutboundByCode("10.0.0.2", s.Code); err != nil || got.ID != s.ID {
		t.Fatalf("right code from another source: %v", err)
	}
	_, _ = a.pm.OutboundByCode("10.0.0.66", wrong(9))
	if _, err := a.pm.OutboundByCode("10.0.0.66", s.Code); !errors.Is(err, ErrPairingThrottled) {
		t.Fatalf("throttled source: %v", err)
	}
}

func TestOutboundByCodeMissesExpirePendingSessions(t *testing.T) {
	a, b := newPairingPeer(t, "node-a"), newPairingPeer(t, "node-b")
	s, _ := offerFrom(t, a, b)
	misses := 0
	for i := 0; misses < maxCodeMisses; i++ {
		code := []byte("900000")
		code[5] = byte('0' + i)
		if string(code) == s.Code {
			continue
		}
		// Each guess from its own source, as a spread-out guesser would.
		_, _ = a.pm.OutboundByCode(string(code), string(code))
		misses++
	}
	if _, err := a.pm.OutboundByCode("10.0.0.2", s.Code); err == nil {
		t.Fatal("pending session survived the miss limit")
	}
	// A new pairing gets a fresh code and works.
	s2, _ := offerFrom(t, a, b)
	if _, err := a.pm.OutboundByCode("10.0.0.2", s2.Code); err != nil {
		t.Fatalf("new session: %v", err)
	}
}

func TestReceiveOfferCapsPendingOffers(t *testing.T) {
	b := newPairingPeer(t, "node-b")
	for i := 0; i < maxIncoming; i++ {
		a := newPairingPeer(t, "node-a"+string(rune('0'+i)))
		_, o := offerFrom(t, a, b)
		if _, err := b.pm.ReceiveOffer(o); err != nil {
			t.Fatalf("offer %d: %v", i, err)
		}
	}
	a := newPairingPeer(t, "node-z")
	_, o := offerFrom(t, a, b)
	if _, err := b.pm.ReceiveOffer(o); err == nil {
		t.Fatal("offer past the cap accepted")
	}
}

func TestFingerprintsAreCanonical(t *testing.T) {
	a, b := newPairingPeer(t, "node-a"), newPairingPeer(t, "node-b")
	ctx := context.Background()
	sum := sha256.Sum256(b.id.PublicKey)
	want := "sha256:" + hex.EncodeToString(sum[:])
	if got := b.id.Fingerprint(); got != want {
		t.Fatalf("identity fingerprint %q, want %q", got, want)
	}
	if err := a.pm.Trust(ctx, "node-b", "B", "", b.id.CertPEM); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := a.db.SQL.QueryRowContext(ctx, `SELECT fingerprint FROM node_trust WHERE node_id = 'node-b'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != want {
		t.Fatalf("stored %q, want %q", stored, want)
	}

	// Rows from older versions hashed the PEM text; they are rewritten.
	if _, err := a.db.SQL.ExecContext(ctx, `UPDATE node_trust SET fingerprint = ? WHERE node_id = 'node-b'`, Fingerprint(string(b.id.CertPEM))); err != nil {
		t.Fatal(err)
	}
	if err := a.pm.RecomputeFingerprints(ctx); err != nil {
		t.Fatal(err)
	}
	if err := a.db.SQL.QueryRowContext(ctx, `SELECT fingerprint FROM node_trust WHERE node_id = 'node-b'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != want {
		t.Fatalf("recomputed %q, want %q", stored, want)
	}
}

func TestTrustRefusesANonKey(t *testing.T) {
	a := newPairingPeer(t, "node-a")
	if err := a.pm.Trust(context.Background(), "x", "X", "", []byte("cert-bytes")); err == nil {
		t.Fatal("trusted something that is not a key")
	}
}

// Pairing never replaces a paired computer's key. Removing the computer
// first allows pairing again, and re-pairing with the same key is fine.
func TestPairingRefusesAChangedKey(t *testing.T) {
	ctx := context.Background()
	a, b := newPairingPeer(t, "node-a"), newPairingPeer(t, "node-b")
	impostor := newPairingPeer(t, "node-x")
	if err := a.pm.Trust(ctx, "node-b", "B", "", b.id.CertPEM); err != nil {
		t.Fatal(err)
	}
	if err := b.pm.Trust(ctx, "node-a", "A", "", a.id.CertPEM); err != nil {
		t.Fatal(err)
	}

	// An offer claiming to be A, with another key, is refused by B.
	s, err := impostor.pm.StartPairing("node-b", "B", "", b.id.CertPEM)
	if err != nil {
		t.Fatal(err)
	}
	o := PairingOffer{SessionID: s.ID, FromNodeID: "node-a", ToNodeID: "node-b", FromName: "A",
		FromCertPEM: string(impostor.id.CertPEM), Code: s.Code, ExpiresAt: s.ExpiresAt.Format(time.RFC3339)}
	impostor.pm.SignOffer(&o)
	if _, err := b.pm.ReceiveOffer(o); !errors.Is(err, ErrPairedWithAnotherKey) {
		t.Fatalf("offer with a changed key: %v", err)
	}

	// A starting pairing toward "B" at a key that is not B's is refused.
	if _, err := a.pm.StartPairing("node-b", "B", "", impostor.id.CertPEM); !errors.Is(err, ErrPairedWithAnotherKey) {
		t.Fatalf("start with a changed key: %v", err)
	}

	// The same key pairs again.
	_, same := offerFrom(t, a, b)
	if _, err := b.pm.ReceiveOffer(same); err != nil {
		t.Fatalf("same key: %v", err)
	}

	// After removing A, B accepts the new key.
	if err := b.pm.RevokeTrust(ctx, "node-a"); err != nil {
		t.Fatal(err)
	}
	o.SessionID = randomID()
	o.Code = "123456"
	if o.Code == same.Code {
		o.Code = "654321"
	}
	impostor.pm.SignOffer(&o)
	got, err := b.pm.ReceiveOffer(o)
	if err != nil {
		t.Fatalf("after removal: %v", err)
	}
	if _, err := b.pm.ApproveSession(ctx, got.ID); err != nil {
		t.Fatal(err)
	}
	trusted, _ := b.pm.TrustedCertPEM(ctx, "node-a")
	if string(trusted) != string(impostor.id.CertPEM) {
		t.Fatal("new key not stored after removal")
	}
}

// Approval re-checks the key, in case the computer was paired another way
// while the offer waited.
func TestApproveRefusesAKeyThatChangedWhileWaiting(t *testing.T) {
	ctx := context.Background()
	a, b, other := newPairingPeer(t, "node-a"), newPairingPeer(t, "node-b"), newPairingPeer(t, "node-o")
	s, o := offerFrom(t, a, b)
	if _, err := b.pm.ReceiveOffer(o); err != nil {
		t.Fatal(err)
	}
	if err := b.pm.Trust(ctx, "node-a", "A", "", other.id.CertPEM); err != nil {
		t.Fatal(err)
	}
	if _, err := b.pm.ApproveSession(ctx, s.ID); !errors.Is(err, ErrPairedWithAnotherKey) {
		t.Fatalf("approve: %v", err)
	}
}
