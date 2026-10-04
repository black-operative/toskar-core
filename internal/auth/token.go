package auth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	authTokenTTL = 5 * time.Minute
	// authTokenVersion starts every token. Version 2 adds the audience and
	// a single-use nonce; tokens without them are refused.
	authTokenVersion = "v2"
)

// AuthClaims are what a verified node token says.
type AuthClaims struct {
	NodeID    string
	Audience  string
	Nonce     string
	ExpiresAt time.Time
}

// AuthToken creates a short-lived Bearer token signed by this node for the
// paired computer audience. It is good for one request to that computer.
// Format (base64url of): v2|nodeID|audience|unixExp|nonce|hex(sig)
func (id *NodeIdentity) AuthToken(audience string) string {
	exp := time.Now().UTC().Add(authTokenTTL).Unix()
	nonce := make([]byte, 16)
	_, _ = rand.Read(nonce)
	n := hex.EncodeToString(nonce)
	sig := id.Sign(authTokenMessage(id.NodeID, audience, exp, n))
	raw := strings.Join([]string{authTokenVersion, id.NodeID, audience, strconv.FormatInt(exp, 10), n, hex.EncodeToString(sig)}, "|")
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func authTokenMessage(nodeID, audience string, exp int64, nonce string) []byte {
	return []byte(fmt.Sprintf("yggdrasil-node-token-%s|%s|%s|%d|%s", authTokenVersion, nodeID, audience, exp, nonce))
}

func splitAuthToken(token string) ([]string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(token))
	if err != nil {
		return nil, fmt.Errorf("invalid token encoding")
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 6 || parts[0] != authTokenVersion || parts[1] == "" {
		return nil, fmt.Errorf("invalid token format; update Toskar on both computers")
	}
	return parts, nil
}

// PeekAuthTokenNodeID extracts the claimed node id without verifying the signature.
func PeekAuthTokenNodeID(token string) (string, error) {
	parts, err := splitAuthToken(token)
	if err != nil {
		return "", err
	}
	return parts[1], nil
}

// ParseAndVerifyAuthToken validates a Bearer token using peerCertPEM and
// checks that it was made for audience, this computer's node ID. The
// caller still checks the nonce has not been used (ReplayGuard).
func ParseAndVerifyAuthToken(token string, peerCertPEM []byte, audience string) (AuthClaims, error) {
	parts, err := splitAuthToken(token)
	if err != nil {
		return AuthClaims{}, err
	}
	c := AuthClaims{NodeID: parts[1], Audience: parts[2], Nonce: parts[4]}
	exp, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		return AuthClaims{}, fmt.Errorf("invalid token expiry")
	}
	c.ExpiresAt = time.Unix(exp, 0).UTC()
	if time.Now().UTC().Unix() > exp {
		return AuthClaims{}, fmt.Errorf("token expired")
	}
	if exp > time.Now().UTC().Add(authTokenTTL+time.Minute).Unix() {
		return AuthClaims{}, fmt.Errorf("token expiry too far ahead")
	}
	if audience == "" || c.Audience != audience {
		return AuthClaims{}, fmt.Errorf("token is for another computer")
	}
	if len(c.Nonce) < 16 {
		return AuthClaims{}, fmt.Errorf("invalid token nonce")
	}
	sig, err := hex.DecodeString(parts[5])
	if err != nil {
		return AuthClaims{}, fmt.Errorf("invalid token signature")
	}
	if !VerifyPeer(peerCertPEM, authTokenMessage(c.NodeID, c.Audience, exp, c.Nonce), sig) {
		return AuthClaims{}, fmt.Errorf("bad signature")
	}
	return c, nil
}

// ReplayGuard remembers token nonces until their tokens expire, so each
// token is accepted once.
type ReplayGuard struct {
	mu     sync.Mutex
	seen   map[string]time.Time
	pruned time.Time
}

func NewReplayGuard() *ReplayGuard {
	return &ReplayGuard{seen: make(map[string]time.Time)}
}

// Use records the claims' nonce and reports whether it was new.
func (g *ReplayGuard) Use(c AuthClaims) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if now := time.Now(); now.Sub(g.pruned) > 30*time.Second {
		for k, exp := range g.seen {
			if now.After(exp) {
				delete(g.seen, k)
			}
		}
		g.pruned = now
	}
	key := c.NodeID + "|" + c.Nonce
	if _, ok := g.seen[key]; ok {
		return false
	}
	g.seen[key] = c.ExpiresAt
	return true
}
