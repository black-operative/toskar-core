package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
)

// CallbackPath is where a sign-in comes back to, on Yggdrasil's own
// address. It is outside /api/v1 because the browser arrives there from
// the service, without an API key; the state value is what proves it.
const CallbackPath = "/mcp/oauth/callback"

// tokenMu serializes refreshing, so two calls do not both spend the
// refresh token.
var tokenMu sync.Mutex

// SignIn starts signing in to a source's service and returns the address
// to open in the browser.
func (m *Manager) SignIn(ctx context.Context, id, redirectBase string) (string, error) {
	m.mu.Lock()
	srv, ok := m.servers[id]
	m.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("%w %q", ErrUnknown, id)
	}
	if !srv.spec.Remote() {
		return "", errors.New("only tool sources on the web have a sign-in")
	}
	return m.startSignIn(ctx, srv, redirectBase, nil)
}

func (m *Manager) startSignIn(ctx context.Context, srv *server, redirectBase string, hint *AuthRequiredError) (string, error) {
	redirect, err := redirectURI(redirectBase)
	if err != nil {
		return "", err
	}
	o, registration, err := discover(ctx, m.http, srv.spec.URL, hint)
	if err != nil {
		return "", fmt.Errorf("could not find where to sign in: %w", err)
	}
	o.RedirectURI = redirect
	sec, _ := m.readSecrets(srv.id)
	// Keep an earlier registration for the same address, so the service
	// does not list Yggdrasil as a new app each time.
	if prev := sec.OAuth; prev != nil && prev.ClientID != "" && prev.RedirectURI == redirect && prev.TokenURL == o.TokenURL {
		o.ClientID, o.ClientSecret = prev.ClientID, prev.ClientSecret
	} else if err := register(ctx, m.http, o, registration, srv.spec.ClientID, sec.ClientSecret); err != nil {
		return "", err
	}
	state := randomString(32)
	link, verifier := o.authorizeURL(state)
	m.mu.Lock()
	for k, p := range m.signins {
		if m.now().After(p.expires) {
			delete(m.signins, k)
		}
	}
	m.signins[state] = &pendingSignIn{serverID: srv.id, verifier: verifier, oauth: *o, expires: m.now().Add(15 * time.Minute)}
	srv.logs.add("info", "Waiting for you to sign in.")
	m.mu.Unlock()
	return link, nil
}

// redirectURI is the callback on the address the browser uses.
func redirectURI(base string) (string, error) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		base = "http://127.0.0.1:7331"
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("%q is not Toskar's address", base)
	}
	return u.Scheme + "://" + u.Host + CallbackPath, nil
}

// FinishSignIn completes a sign-in when the browser comes back with a
// code, then connects and lists the tools. It returns the source's name.
func (m *Manager) FinishSignIn(ctx context.Context, state, code, errCode, errDescription string) (string, error) {
	m.mu.Lock()
	p, ok := m.signins[state]
	delete(m.signins, state)
	m.mu.Unlock()
	if !ok || m.now().After(p.expires) {
		return "", errors.New("this sign-in link expired. Start again from Tools")
	}
	m.mu.Lock()
	srv, ok := m.servers[p.serverID]
	m.mu.Unlock()
	if !ok {
		return "", errors.New("that tool source was removed")
	}
	if errCode != "" {
		if errCode == "access_denied" {
			return srv.spec.Name, errors.New("sign-in was cancelled")
		}
		return srv.spec.Name, fmt.Errorf("sign-in failed: %s", firstNonEmpty(errDescription, errCode))
	}
	o := p.oauth
	if err := o.exchange(ctx, m.http, code, p.verifier); err != nil {
		return srv.spec.Name, err
	}
	sec, _ := m.readSecrets(srv.id)
	sec.OAuth = &o
	if err := m.writeSecrets(srv.id, sec); err != nil {
		return srv.spec.Name, err
	}
	srv.logs.add("info", "Signed in.")
	v, err := m.Check(ctx, srv.id)
	if err != nil {
		return srv.spec.Name, err
	}
	if v.Status != StatusReady {
		return srv.spec.Name, errors.New(firstNonEmpty(v.Error, "signed in, but the tool source did not connect"))
	}
	return srv.spec.Name, nil
}

// SignOut forgets a source's sign-in.
func (m *Manager) SignOut(ctx context.Context, id string) (View, error) {
	m.mu.Lock()
	srv, ok := m.servers[id]
	m.mu.Unlock()
	if !ok {
		return View{}, fmt.Errorf("%w %q", ErrUnknown, id)
	}
	sec, _ := m.readSecrets(id)
	if sec.OAuth != nil {
		sec.OAuth.AccessToken, sec.OAuth.RefreshToken = "", ""
	}
	if err := m.writeSecrets(id, sec); err != nil {
		return View{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if srv.client != nil {
		go srv.client.Close()
		srv.client = nil
	}
	srv.status = StatusSignIn
	if err := m.saveLocked(ctx, srv); err != nil {
		return View{}, err
	}
	return m.viewLocked(srv), nil
}

// token returns a source's access token, refreshing it first when it is
// about to expire.
func (m *Manager) token(ctx context.Context, id string) (string, error) {
	tokenMu.Lock()
	defer tokenMu.Unlock()
	sec, err := m.readSecrets(id)
	if err != nil || !sec.OAuth.SignedIn() {
		return "", nil
	}
	if sec.OAuth.expiring() {
		if err := sec.OAuth.refresh(ctx, m.http); err != nil {
			return "", &AuthRequiredError{}
		}
		if err := m.writeSecrets(id, sec); err != nil {
			return "", err
		}
	}
	return sec.OAuth.AccessToken, nil
}

// refreshNow refreshes a source's access token whatever its expiry.
func (m *Manager) refreshNow(ctx context.Context, id string) error {
	tokenMu.Lock()
	defer tokenMu.Unlock()
	sec, err := m.readSecrets(id)
	if err != nil || sec.OAuth == nil {
		return errors.New("not signed in")
	}
	if err := sec.OAuth.refresh(ctx, m.http); err != nil {
		return err
	}
	return m.writeSecrets(id, sec)
}
