package join

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"strings"

	"github.com/yeixio/toskar-core/internal/auth"
)

// The handshake, over Bifrost (port 7332):
//
//  1. POST /internal/v1/join/hello {token_id, client_nonce}
//     → the issuer's node ID, name, public key, network ID, a server nonce,
//     and its signature over all of them and the client nonce. The joining
//     computer checks the key against the fingerprint in the command and
//     the signature, so it knows it reached the right computer before going
//     on. The answer is the same whether or not the token exists.
//  2. POST /internal/v1/join {token_id, both nonces, the joining computer's
//     ID, name, public key, and address, proof}
//     proof = HMAC(ProofKey(secret), nonces, token ID, the joining node's ID
//     and key, the issuer's fingerprint). The issuer checks it, uses the
//     token up, trusts the new key, and returns the signed acceptance.
//
// A relay can't swap in its own key (the proof covers it), replay the
// proof (the server nonce is single-use), or forge the acceptance (it is
// signed by the issuer's key).

// Bifrost paths.
const (
	HelloPath = "/internal/v1/join/hello"
	JoinPath  = "/internal/v1/join"
	LeavePath = "/internal/v1/join/leave"
)

// HelloRequest starts a join.
type HelloRequest struct {
	TokenID     string `json:"token_id"`
	ClientNonce string `json:"client_nonce"`
}

// HelloResponse is the issuer, signed.
type HelloResponse struct {
	NodeID      string `json:"node_id"`
	Name        string `json:"name"`
	PublicKey   string `json:"public_key"` // base64 raw ed25519
	NetworkID   string `json:"network_id"`
	ServerNonce string `json:"server_nonce"`
	Signature   string `json:"signature"`
}

func (h HelloResponse) message(tokenID, clientNonce string) []byte {
	return []byte(strings.Join([]string{"yggdrasil-join-hello-v1", tokenID, clientNonce, h.ServerNonce, h.NodeID, h.Name, h.PublicKey, h.NetworkID}, "\n"))
}

// JoiningNode is the computer asking to join.
type JoiningNode struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	PublicKeyPEM string `json:"public_key_pem"`
	// Address is where the issuer can reach its Bifrost, host:port.
	Address string `json:"address"`
	Version string `json:"version,omitempty"`
}

// JoinRequest proves the token and asks to join.
type JoinRequest struct {
	TokenID     string      `json:"token_id"`
	ClientNonce string      `json:"client_nonce"`
	ServerNonce string      `json:"server_nonce"`
	Node        JoiningNode `json:"node"`
	Proof       string      `json:"proof"`
}

func proofMessage(r JoinRequest, serverFingerprint string) []byte {
	key := sha256.Sum256([]byte(r.Node.PublicKeyPEM))
	return []byte(strings.Join([]string{"yggdrasil-join-proof-v1", r.TokenID, r.ClientNonce, r.ServerNonce,
		r.Node.ID, r.Node.Name, hex.EncodeToString(key[:]), r.Node.Address, serverFingerprint}, "\n"))
}

// Prove is the joining computer's proof that it holds the token's secret.
func Prove(secret string, r JoinRequest, serverFingerprint string) string {
	m := hmac.New(sha256.New, ProofKey(secret))
	m.Write(proofMessage(r, serverFingerprint))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func checkProof(key []byte, r JoinRequest, serverFingerprint string) bool {
	want := hmac.New(sha256.New, key)
	want.Write(proofMessage(r, serverFingerprint))
	got, err := base64.RawURLEncoding.DecodeString(r.Proof)
	return err == nil && hmac.Equal(got, want.Sum(nil))
}

// Peer is a computer in the network.
type Peer struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Address      string `json:"address"`
	PublicKeyPEM string `json:"public_key_pem"`
}

// Accepted is the issuer's answer to a successful join.
type Accepted struct {
	NetworkID string `json:"network_id"`
	Server    Peer   `json:"server"`
	// Name is what the issuer calls the joining computer, which differs
	// from the name it asked for when another computer already has it.
	Name        string `json:"name"`
	ClientNonce string `json:"client_nonce"`
}

// JoinResponse is Accepted, signed by the issuer.
type JoinResponse struct {
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
}

// ErrorResponse is a refused join.
type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Fingerprint is a public key's fingerprint as join commands show it:
// sha256 of the raw ed25519 key.
func Fingerprint(pub ed25519.PublicKey) string {
	return auth.KeyFingerprint(pub)
}

// NormalizeFingerprint accepts a fingerprint with or without "sha256:", in
// any case.
func NormalizeFingerprint(fp string) (string, error) {
	fp = strings.ToLower(strings.TrimSpace(fp))
	fp = strings.TrimPrefix(fp, "sha256:")
	if b, err := hex.DecodeString(fp); err != nil || len(b) != sha256.Size {
		return "", errors.New("the fingerprint must be sha256: and 64 hex characters, as the join command gives it")
	}
	return "sha256:" + fp, nil
}

// PublicKeyFromPEM reads an ed25519 public key in PEM, as node identities
// store them.
func PublicKeyFromPEM(s string) (ed25519.PublicKey, error) {
	return auth.PublicKeyFromPEM([]byte(s))
}

// publicKeyPEM is an ed25519 public key in the PEM form node identities
// use.
func publicKeyPEM(pub ed25519.PublicKey) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "ED25519 PUBLIC KEY", Bytes: pub}))
}
