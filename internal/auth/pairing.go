package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// PairingState represents pairing session states.
type PairingState string

const (
	PairingPending  PairingState = "pending"
	PairingApproved PairingState = "approved"
	PairingRejected PairingState = "rejected"
	PairingExpired  PairingState = "expired"
)

// PairingSession is an in-flight pairing attempt.
type PairingSession struct {
	ID           string       `json:"id"`
	LocalNodeID  string       `json:"local_node_id"`
	RemoteNodeID string       `json:"remote_node_id"`
	RemoteName   string       `json:"remote_name"`
	RemoteAddr   string       `json:"remote_address,omitempty"`
	Code         string       `json:"code"`
	State        PairingState `json:"state"`
	RemoteCert   []byte       `json:"-"`
	CreatedAt    time.Time    `json:"created_at"`
	ExpiresAt    time.Time    `json:"expires_at"`
	Incoming     bool         `json:"incoming,omitempty"`

	failures int // rejected completions
}

// Limits on guessing. A code has a million values; these keep the chance of
// guessing one before it is shown to a person negligible.
const (
	pairingTTL = 10 * time.Minute
	// maxCompleteFailures rejected completions expire a session.
	maxCompleteFailures = 5
	// maxCodeMisses wrong codes asked for, across all sources, expire every
	// pending outbound session; pairing starts over with a new code.
	maxCodeMisses = 10
	// maxIncoming pending offers are kept; more are refused.
	maxIncoming = 8
)

// ErrPairingThrottled is returned when a source has failed too often.
var ErrPairingThrottled = errors.New("too many pairing attempts; wait a few minutes and start pairing again")

// PairingOffer is sent from initiator to the peer over Bifrost. Signature
// is the initiator's node key over offerMessage, so the offer is bound to
// that key, the code, and the computer it is for.
type PairingOffer struct {
	SessionID   string `json:"session_id"`
	FromNodeID  string `json:"from_node_id"`
	ToNodeID    string `json:"to_node_id"`
	FromName    string `json:"from_name"`
	FromCertPEM string `json:"from_cert_pem"`
	FromAddress string `json:"from_address,omitempty"`
	Code        string `json:"code"`
	ExpiresAt   string `json:"expires_at"`
	Signature   string `json:"signature"`
}

// PairingComplete finishes mutual trust on the initiator. Signature is the
// approving computer's node key over completeMessage, which includes the
// pairing code; the code itself is not sent.
type PairingComplete struct {
	SessionID   string `json:"session_id"`
	FromNodeID  string `json:"from_node_id"`
	FromName    string `json:"from_name"`
	FromCertPEM string `json:"from_cert_pem"`
	FromAddress string `json:"from_address,omitempty"`
	Signature   string `json:"signature"`
}

func offerMessage(o PairingOffer) []byte {
	return []byte(strings.Join([]string{"yggdrasil-pairing-offer-v1",
		o.SessionID, o.FromNodeID, o.ToNodeID, o.Code, o.ExpiresAt, pemFingerprint(o.FromCertPEM)}, "\n"))
}

func completeMessage(c PairingComplete, toNodeID, code string) []byte {
	return []byte(strings.Join([]string{"yggdrasil-pairing-complete-v1",
		c.SessionID, c.FromNodeID, toNodeID, code, pemFingerprint(c.FromCertPEM)}, "\n"))
}

func pemFingerprint(certPEM string) string {
	pub, err := PublicKeyFromPEM([]byte(certPEM))
	if err != nil {
		return ""
	}
	return KeyFingerprint(pub)
}

func verifySignature(certPEM, sigB64 string, msg []byte) bool {
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil || len(sig) == 0 {
		return false
	}
	return VerifyPeer([]byte(certPEM), msg, sig)
}

// PairingManager implements the pairing state machine.
type PairingManager struct {
	db        *sql.DB
	identity  *NodeIdentity
	mu        sync.Mutex
	sessions  map[string]*PairingSession
	codeIndex map[string]string // code -> session id
	incoming  map[string]*PairingSession
	codeMiss  int // wrong codes asked for since the last reset
	limiter   *attemptLimiter
}

func NewPairingManager(db *sql.DB, identity *NodeIdentity) *PairingManager {
	return &PairingManager{
		db:        db,
		identity:  identity,
		sessions:  make(map[string]*PairingSession),
		codeIndex: make(map[string]string),
		incoming:  make(map[string]*PairingSession),
		limiter:   newAttemptLimiter(10, 10*time.Minute),
	}
}

// Identity returns the local node identity.
func (p *PairingManager) Identity() *NodeIdentity { return p.identity }

// StartPairing initiates pairing with a discovered node.
func (p *PairingManager) StartPairing(remoteNodeID, remoteName, remoteAddr string, remoteCert []byte) (*PairingSession, error) {
	if err := p.checkSameKey(context.Background(), remoteNodeID, remoteCert); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	code, err := p.unusedCode()
	if err != nil {
		return nil, err
	}
	p.codeMiss = 0
	s := &PairingSession{
		ID:           randomID(),
		LocalNodeID:  p.identity.NodeID,
		RemoteNodeID: remoteNodeID,
		RemoteName:   remoteName,
		RemoteAddr:   remoteAddr,
		Code:         code,
		State:        PairingPending,
		RemoteCert:   append([]byte(nil), remoteCert...),
		CreatedAt:    time.Now().UTC(),
		ExpiresAt:    time.Now().UTC().Add(pairingTTL),
	}
	p.sessions[s.ID] = s
	p.codeIndex[code] = s.ID
	return cloneSession(s), nil
}

// SignOffer signs an offer this computer sends.
func (p *PairingManager) SignOffer(o *PairingOffer) {
	o.Signature = base64.StdEncoding.EncodeToString(p.identity.Sign(offerMessage(*o)))
}

// SignComplete signs the completion of an incoming offer this computer
// approved, binding it to that offer's code.
func (p *PairingManager) SignComplete(c *PairingComplete, toNodeID, code string) {
	c.Signature = base64.StdEncoding.EncodeToString(p.identity.Sign(completeMessage(*c, toNodeID, code)))
}

// ReceiveOffer stores an incoming pairing offer from a peer after checking
// it was made for this computer and signed by the key it carries.
func (p *PairingManager) ReceiveOffer(offer PairingOffer) (*PairingSession, error) {
	if offer.ToNodeID != p.identity.NodeID {
		return nil, fmt.Errorf("pairing offer is for another computer")
	}
	if offer.SessionID == "" || offer.FromNodeID == "" || offer.FromNodeID == p.identity.NodeID {
		return nil, fmt.Errorf("invalid pairing offer")
	}
	if !validCode(offer.Code) {
		return nil, fmt.Errorf("invalid pairing code")
	}
	if !verifySignature(offer.FromCertPEM, offer.Signature, offerMessage(offer)) {
		return nil, fmt.Errorf("pairing offer is not signed by its computer; update Toskar on both computers")
	}
	if err := p.checkSameKey(context.Background(), offer.FromNodeID, []byte(offer.FromCertPEM)); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	exp, _ := time.Parse(time.RFC3339, offer.ExpiresAt)
	if exp.IsZero() || exp.After(now.Add(pairingTTL)) {
		exp = now.Add(pairingTTL)
	}
	if now.After(exp) {
		return nil, fmt.Errorf("pairing offer expired")
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	p.pruneIncomingLocked(now)
	if prev, ok := p.incoming[offer.SessionID]; ok {
		// The same offer delivered twice (Bifrost and the control API).
		if prev.Code == offer.Code && string(prev.RemoteCert) == offer.FromCertPEM {
			return cloneSession(prev), nil
		}
		return nil, fmt.Errorf("pairing session already exists")
	}
	if _, ok := p.sessions[offer.SessionID]; ok {
		return nil, fmt.Errorf("pairing session already exists")
	}
	if _, ok := p.codeIndex[offer.Code]; ok {
		return nil, fmt.Errorf("pairing code already in use; start pairing again")
	}
	if len(p.incoming) >= maxIncoming {
		return nil, fmt.Errorf("too many pairing requests are waiting on this computer")
	}
	s := &PairingSession{
		ID:           offer.SessionID,
		LocalNodeID:  p.identity.NodeID,
		RemoteNodeID: offer.FromNodeID,
		RemoteName:   offer.FromName,
		RemoteAddr:   offer.FromAddress,
		Code:         offer.Code,
		State:        PairingPending,
		RemoteCert:   []byte(offer.FromCertPEM),
		CreatedAt:    now,
		ExpiresAt:    exp,
		Incoming:     true,
	}
	p.incoming[s.ID] = s
	p.codeIndex[s.Code] = s.ID
	return cloneSession(s), nil
}

func (p *PairingManager) pruneIncomingLocked(now time.Time) {
	for id, s := range p.incoming {
		if s.State != PairingPending || now.After(s.ExpiresAt) {
			delete(p.incoming, id)
			if p.codeIndex[s.Code] == id {
				delete(p.codeIndex, s.Code)
			}
		}
	}
}

// ListIncoming returns pending incoming offers.
func (p *PairingManager) ListIncoming() []PairingSession {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pruneIncomingLocked(time.Now())
	out := make([]PairingSession, 0, len(p.incoming))
	for _, s := range p.incoming {
		out = append(out, *cloneSession(s))
	}
	return out
}

// ApproveByCode approves pairing when codes match (incoming or local).
func (p *PairingManager) ApproveByCode(ctx context.Context, code string) (*PairingSession, error) {
	p.mu.Lock()
	sessionID, ok := p.codeIndex[code]
	if !ok {
		p.mu.Unlock()
		return nil, fmt.Errorf("invalid or expired pairing code")
	}
	s := p.sessions[sessionID]
	if s == nil {
		s = p.incoming[sessionID]
	}
	if s == nil || s.State != PairingPending || time.Now().After(s.ExpiresAt) {
		p.mu.Unlock()
		return nil, fmt.Errorf("pairing session expired")
	}
	s.State = PairingApproved
	cp := cloneSession(s)
	p.mu.Unlock()

	if err := p.storePairedTrust(ctx, cp.RemoteNodeID, cp.RemoteName, cp.RemoteAddr, cp.RemoteCert); err != nil {
		return nil, err
	}
	return cp, nil
}

// ApproveSession approves by session ID (local UI / incoming).
func (p *PairingManager) ApproveSession(ctx context.Context, sessionID string) (*PairingSession, error) {
	p.mu.Lock()
	s := p.sessions[sessionID]
	if s == nil {
		s = p.incoming[sessionID]
	}
	if s == nil || s.State != PairingPending {
		p.mu.Unlock()
		return nil, fmt.Errorf("session not found")
	}
	s.State = PairingApproved
	cp := cloneSession(s)
	p.mu.Unlock()
	if err := p.storePairedTrust(ctx, cp.RemoteNodeID, cp.RemoteName, cp.RemoteAddr, cp.RemoteCert); err != nil {
		return nil, err
	}
	return cp, nil
}

// CompleteFromPeer finalizes initiator trust after peer approval. The
// completion must come from the computer this one started pairing with: it
// must carry that computer's key, as fetched when pairing started, and be
// signed by it over the session and the code. source is the caller's
// address, for rate limiting.
func (p *PairingManager) CompleteFromPeer(ctx context.Context, source string, complete PairingComplete) (*PairingSession, error) {
	if p.limiter.blocked(source) {
		return nil, ErrPairingThrottled
	}
	p.mu.Lock()
	s := p.sessions[complete.SessionID]
	if s == nil || s.State != PairingPending || time.Now().After(s.ExpiresAt) {
		p.mu.Unlock()
		p.limiter.fail(source)
		return nil, fmt.Errorf("pairing session not found or expired")
	}
	ok := complete.FromNodeID == s.RemoteNodeID &&
		pemFingerprint(complete.FromCertPEM) != "" &&
		pemFingerprint(complete.FromCertPEM) == pemFingerprint(string(s.RemoteCert)) &&
		verifySignature(complete.FromCertPEM, complete.Signature, completeMessage(complete, p.identity.NodeID, s.Code))
	if !ok {
		s.failures++
		if s.failures >= maxCompleteFailures {
			p.expireLocked(s)
		}
		p.mu.Unlock()
		p.limiter.fail(source)
		return nil, fmt.Errorf("pairing could not be verified")
	}
	s.State = PairingApproved
	delete(p.codeIndex, s.Code)
	s.RemoteName = complete.FromName
	if complete.FromAddress != "" {
		s.RemoteAddr = complete.FromAddress
	}
	cp := cloneSession(s)
	p.mu.Unlock()
	if err := p.storePairedTrust(ctx, cp.RemoteNodeID, cp.RemoteName, complete.FromAddress, cp.RemoteCert); err != nil {
		return nil, err
	}
	return cp, nil
}

func (p *PairingManager) expireLocked(s *PairingSession) {
	s.State = PairingExpired
	if p.codeIndex[s.Code] == s.ID {
		delete(p.codeIndex, s.Code)
	}
}

// Reject cancels a pairing session.
func (p *PairingManager) Reject(sessionID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.sessions[sessionID]
	if s == nil {
		s = p.incoming[sessionID]
	}
	if s == nil {
		return fmt.Errorf("session not found")
	}
	s.State = PairingRejected
	delete(p.codeIndex, s.Code)
	delete(p.incoming, sessionID)
	return nil
}

// OutboundByCode returns a pending outbound session for a peer that claims
// it by code. source is the caller's address. Wrong codes count against the
// source, and enough of them, from anywhere, expire every pending outbound
// session.
func (p *PairingManager) OutboundByCode(source, code string) (*PairingSession, error) {
	if p.limiter.blocked(source) {
		return nil, ErrPairingThrottled
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	var s *PairingSession
	if id, ok := p.codeIndex[code]; ok && validCode(code) {
		s = p.sessions[id]
	}
	if s == nil || s.Incoming || s.State != PairingPending || time.Now().After(s.ExpiresAt) {
		p.limiter.fail(source)
		p.codeMiss++
		if p.codeMiss >= maxCodeMisses {
			for _, o := range p.sessions {
				if o.State == PairingPending {
					p.expireLocked(o)
				}
			}
			p.codeMiss = 0
		}
		return nil, fmt.Errorf("unknown or expired code")
	}
	return cloneSession(s), nil
}

// GetSession returns a local outbound session.
func (p *PairingManager) GetSession(sessionID string) (*PairingSession, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s, ok := p.sessions[sessionID]
	if !ok {
		return nil, false
	}
	return cloneSession(s), true
}

// GetIncoming returns an incoming session.
func (p *PairingManager) GetIncoming(sessionID string) (*PairingSession, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s, ok := p.incoming[sessionID]
	if !ok {
		return nil, false
	}
	return cloneSession(s), true
}

// RevokeTrust removes trust for a node.
func (p *PairingManager) RevokeTrust(ctx context.Context, nodeID string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := p.db.ExecContext(ctx, `UPDATE node_trust SET revoked_at = ? WHERE node_id = ?`, now, nodeID)
	return err
}

// TrustedCertPEM returns the stored cert for a paired node.
func (p *PairingManager) TrustedCertPEM(ctx context.Context, nodeID string) ([]byte, error) {
	var pem string
	err := p.db.QueryRowContext(ctx, `
		SELECT cert_pem FROM node_trust WHERE node_id = ? AND revoked_at IS NULL`, nodeID).Scan(&pem)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("node %q not trusted", nodeID)
	}
	if err != nil {
		return nil, err
	}
	return []byte(pem), nil
}

// IsTrusted reports whether nodeID is paired and not revoked.
func (p *PairingManager) IsTrusted(ctx context.Context, nodeID string) bool {
	_, err := p.TrustedCertPEM(ctx, nodeID)
	return err == nil
}

// Trust pairs a computer whose key was verified another way, such as by a
// one-line join token (#40).
func (p *PairingManager) Trust(ctx context.Context, nodeID, name, address string, cert []byte) error {
	return p.storeTrust(ctx, nodeID, name, address, cert)
}

// ErrPairedWithAnotherKey refuses pairing a computer that is already paired
// under a different key. Removing it first allows pairing again.
var ErrPairedWithAnotherKey = errors.New("that computer is already paired with a different key; remove it on the Computers page, then pair again")

// checkSameKey refuses cert for nodeID when nodeID is paired, and not
// removed, under another key. Pairing never replaces a paired computer's
// key; the person removes it first.
func (p *PairingManager) checkSameKey(ctx context.Context, nodeID string, cert []byte) error {
	var trusted string
	err := p.db.QueryRowContext(ctx, `
		SELECT cert_pem FROM node_trust WHERE node_id = ? AND revoked_at IS NULL`, nodeID).Scan(&trusted)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // not paired, or removed
	}
	if err != nil {
		return err
	}
	have, err1 := PublicKeyFromPEM([]byte(trusted))
	want, err2 := PublicKeyFromPEM(cert)
	if err1 == nil && err2 == nil && have.Equal(want) {
		return nil
	}
	return ErrPairedWithAnotherKey
}

// storePairedTrust stores trust from a pairing, refusing a changed key.
func (p *PairingManager) storePairedTrust(ctx context.Context, nodeID, name, address string, cert []byte) error {
	if err := p.checkSameKey(ctx, nodeID, cert); err != nil {
		return err
	}
	return p.storeTrust(ctx, nodeID, name, address, cert)
}

func (p *PairingManager) storeTrust(ctx context.Context, nodeID, name, address string, cert []byte) error {
	pub, err := PublicKeyFromPEM(cert)
	if err != nil {
		return fmt.Errorf("peer key: %w", err)
	}
	fp := KeyFingerprint(pub)
	_, err = p.db.ExecContext(ctx, `
		INSERT INTO nodes (id, name, status, is_local, address)
		VALUES (?, ?, 'online', 0, ?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name, status='online', address=COALESCE(NULLIF(excluded.address,''), nodes.address), updated_at=datetime('now')`,
		nodeID, name, address)
	if err != nil {
		return err
	}
	_, err = p.db.ExecContext(ctx, `
		INSERT INTO node_trust (node_id, fingerprint, cert_pem, paired_at)
		VALUES (?, ?, ?, datetime('now'))
		ON CONFLICT(node_id) DO UPDATE SET fingerprint=excluded.fingerprint, cert_pem=excluded.cert_pem, revoked_at=NULL, paired_at=datetime('now')`,
		nodeID, fp, string(cert))
	return err
}

// RecomputeFingerprints rewrites node_trust fingerprints in the canonical
// form (KeyFingerprint). Older versions hashed the PEM text instead.
func (p *PairingManager) RecomputeFingerprints(ctx context.Context) error {
	rows, err := p.db.QueryContext(ctx, `SELECT node_id, fingerprint, cert_pem FROM node_trust`)
	if err != nil {
		return err
	}
	type fix struct{ id, fp string }
	var fixes []fix
	for rows.Next() {
		var id, fp, certPEM string
		if err := rows.Scan(&id, &fp, &certPEM); err != nil {
			rows.Close()
			return err
		}
		if pub, err := PublicKeyFromPEM([]byte(certPEM)); err == nil {
			if want := KeyFingerprint(pub); want != fp {
				fixes = append(fixes, fix{id, want})
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, f := range fixes {
		if _, err := p.db.ExecContext(ctx, `UPDATE node_trust SET fingerprint = ? WHERE node_id = ?`, f.fp, f.id); err != nil {
			return err
		}
	}
	return nil
}

// SetNodeAddress updates a paired node's reachable address.
func (p *PairingManager) SetNodeAddress(ctx context.Context, nodeID, address string) error {
	_, err := p.db.ExecContext(ctx, `UPDATE nodes SET address = ?, updated_at=datetime('now') WHERE id = ?`, address, nodeID)
	return err
}

// SetNodeStatus persists online/offline (and similar) for a paired node.
func (p *PairingManager) SetNodeStatus(ctx context.Context, nodeID, status string) error {
	_, err := p.db.ExecContext(ctx, `
		UPDATE nodes SET status = ?, last_seen_at = CASE WHEN ? = 'online' THEN datetime('now') ELSE last_seen_at END, updated_at=datetime('now')
		WHERE id = ?`, status, status, nodeID)
	return err
}

// SetNodeHardware stores the latest hardware snapshot for a paired node.
func (p *PairingManager) SetNodeHardware(ctx context.Context, nodeID string, inv contracts.HardwareInventory) error {
	raw, err := json.Marshal(inv)
	if err != nil {
		return err
	}
	_, err = p.db.ExecContext(ctx, `
		UPDATE nodes SET hardware_json = ?, os = ?, arch = ?, updated_at=datetime('now')
		WHERE id = ?`, string(raw), inv.OS, inv.Arch, nodeID)
	return err
}

func cloneSession(s *PairingSession) *PairingSession {
	if s == nil {
		return nil
	}
	cp := *s
	cp.RemoteCert = append([]byte(nil), s.RemoteCert...)
	return &cp
}

// unusedCode is a uniformly random 6-digit code not already in use.
func (p *PairingManager) unusedCode() (string, error) {
	for {
		n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
		if err != nil {
			return "", err
		}
		code := fmt.Sprintf("%06d", n.Int64())
		if _, used := p.codeIndex[code]; !used {
			return code, nil
		}
	}
}

func validCode(code string) bool {
	if len(code) != 6 {
		return false
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func randomID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
