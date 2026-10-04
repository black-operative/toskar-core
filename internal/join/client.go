package join

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Errors on the joining computer.
var (
	// ErrWrongServer is a server whose key does not match the fingerprint
	// in the command; nothing that proves the token was sent.
	ErrWrongServer = errors.New("the computer at that address isn't the one the join command was made on")
	// ErrBadSignature is an answer not signed by the server's key.
	ErrBadSignature = errors.New("the answer wasn't signed by the computer the join command was made on")
)

// WrongServerError says which key answered.
type WrongServerError struct{ Expected, Received string }

func (e *WrongServerError) Error() string {
	return fmt.Sprintf("%s (expected %s, received %s); nothing was sent that proves the token", ErrWrongServer, e.Expected, e.Received)
}
func (e *WrongServerError) Unwrap() error { return ErrWrongServer }

// RefusedError is the issuer refusing the join.
type RefusedError struct {
	Code    string
	Message string
}

func (e *RefusedError) Error() string { return e.Message }

// Is lets errors.Is match the token errors by code.
func (e *RefusedError) Is(target error) bool {
	switch target {
	case ErrInvalid:
		return e.Code == "JOIN_TOKEN_INVALID"
	case ErrExpired:
		return e.Code == "JOIN_TOKEN_EXPIRED"
	case ErrUsed:
		return e.Code == "JOIN_TOKEN_USED"
	case ErrRevoked:
		return e.Code == "JOIN_TOKEN_REVOKED"
	case ErrRateLimited:
		return e.Code == "JOIN_RATE_LIMITED"
	}
	return false
}

// UnreachableError is a server that could not be reached.
type UnreachableError struct {
	Server string
	Err    error
}

func (e *UnreachableError) Error() string {
	return fmt.Sprintf("could not reach Toskar at %s: %v", e.Server, e.Err)
}
func (e *UnreachableError) Unwrap() error { return e.Err }

// Client joins this computer to a network.
type Client struct {
	// Server is the issuer's Bifrost address, host:port.
	Server string
	// Token and Fingerprint come from the join command.
	Token       string
	Fingerprint string
	// Node is this computer, with its own key; its private key never
	// leaves it.
	Node JoiningNode
	HTTP *http.Client
	// Check is called with the verified issuer before the token is proven,
	// to stop a join that should not happen (already joined, another
	// network).
	Check func(h HelloResponse) error
}

func (c *Client) post(ctx context.Context, path string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+c.Server+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return &UnreachableError{Server: c.Server, Err: err}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return &UnreachableError{Server: c.Server, Err: err}
	}
	if resp.StatusCode == http.StatusNotFound {
		return &UnreachableError{Server: c.Server, Err: errors.New("it answered, but not as a Toskar that can take joins; update Toskar there")}
	}
	if resp.StatusCode != http.StatusOK {
		var e ErrorResponse
		if json.Unmarshal(raw, &e) == nil && e.Code != "" {
			return &RefusedError{Code: e.Code, Message: e.Message}
		}
		return &UnreachableError{Server: c.Server, Err: fmt.Errorf("it answered %d", resp.StatusCode)}
	}
	return json.Unmarshal(raw, out)
}

// NormalizeServer accepts host, host:port, or an http(s) URL, and returns
// host:port, with Bifrost's port 7332 when none is given.
func NormalizeServer(s string) (string, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(strings.TrimPrefix(s, "http://"), "https://")
	s = strings.TrimSuffix(s, "/")
	if s == "" || strings.ContainsAny(s, "/?# ") {
		return "", errors.New("the server must be an address such as 192.168.1.10:7332")
	}
	if _, _, err := net.SplitHostPort(s); err != nil {
		s = net.JoinHostPort(strings.Trim(s, "[]"), "7332")
	}
	return s, nil
}

// Hello reaches the issuer and verifies it against the fingerprint.
func (c *Client) hello(ctx context.Context, tokenID string) (HelloResponse, string, error) {
	cn, err := nonce()
	if err != nil {
		return HelloResponse{}, "", err
	}
	var h HelloResponse
	if err := c.post(ctx, HelloPath, HelloRequest{TokenID: tokenID, ClientNonce: cn}, &h); err != nil {
		return HelloResponse{}, "", err
	}
	want, err := NormalizeFingerprint(c.Fingerprint)
	if err != nil {
		return HelloResponse{}, "", err
	}
	key, err := base64.StdEncoding.DecodeString(h.PublicKey)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return HelloResponse{}, "", ErrBadSignature
	}
	if got := Fingerprint(key); got != want {
		return HelloResponse{}, "", &WrongServerError{Expected: want, Received: got}
	}
	sig, err := base64.StdEncoding.DecodeString(h.Signature)
	if err != nil || !ed25519.Verify(key, h.message(tokenID, cn), sig) {
		return HelloResponse{}, "", ErrBadSignature
	}
	return h, cn, nil
}

// Join runs the handshake and returns the verified acceptance and the
// issuer.
func (c *Client) Join(ctx context.Context) (Accepted, HelloResponse, error) {
	tokenID, secret, err := ParseToken(c.Token)
	if err != nil {
		return Accepted{}, HelloResponse{}, err
	}
	h, cn, err := c.hello(ctx, tokenID)
	if err != nil {
		return Accepted{}, HelloResponse{}, err
	}
	if c.Check != nil {
		if err := c.Check(h); err != nil {
			return Accepted{}, h, err
		}
	}
	fp, _ := NormalizeFingerprint(c.Fingerprint)
	req := JoinRequest{TokenID: tokenID, ClientNonce: cn, ServerNonce: h.ServerNonce, Node: c.Node}
	req.Proof = Prove(secret, req, fp)
	var resp JoinResponse
	if err := c.post(ctx, JoinPath, req, &resp); err != nil {
		return Accepted{}, h, err
	}
	key, _ := base64.StdEncoding.DecodeString(h.PublicKey)
	sig, err := base64.StdEncoding.DecodeString(resp.Signature)
	if err != nil || !ed25519.Verify(key, append([]byte("yggdrasil-join-accept-v1\n"), resp.Payload...), sig) {
		return Accepted{}, h, ErrBadSignature
	}
	var acc Accepted
	if err := json.Unmarshal([]byte(resp.Payload), &acc); err != nil || acc.ClientNonce != cn || acc.Server.ID != h.NodeID {
		return Accepted{}, h, ErrBadSignature
	}
	// The issuer's key in the acceptance must be the one checked against
	// the fingerprint.
	if pub, err := PublicKeyFromPEM(acc.Server.PublicKeyPEM); err != nil || !bytes.Equal(pub, key) {
		return Accepted{}, h, ErrBadSignature
	}
	return acc, h, nil
}
