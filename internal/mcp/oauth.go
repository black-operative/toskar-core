package mcp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OAuth is a remote server's sign-in: the app registration and the tokens.
// It is stored with the server's other secrets, never in SQLite.
type OAuth struct {
	Issuer       string    `json:"issuer,omitempty"`
	AuthorizeURL string    `json:"authorize_url,omitempty"`
	TokenURL     string    `json:"token_url,omitempty"`
	Resource     string    `json:"resource,omitempty"`
	Scope        string    `json:"scope,omitempty"`
	RedirectURI  string    `json:"redirect_uri,omitempty"`
	ClientID     string    `json:"client_id,omitempty"`
	ClientSecret string    `json:"client_secret,omitempty"`
	AccessToken  string    `json:"access_token,omitempty"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitzero"`
	Account      string    `json:"account,omitempty"`
}

// SignedIn reports whether there is a token to use.
func (o *OAuth) SignedIn() bool { return o != nil && o.AccessToken != "" }

type protectedResource struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
	ScopesSupported      []string `json:"scopes_supported"`
}

type authServerMeta struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	RegistrationEndpoint  string   `json:"registration_endpoint"`
	ScopesSupported       []string `json:"scopes_supported"`
	CodeChallengeMethods  []string `json:"code_challenge_methods_supported"`
}

// discover finds where to sign in for the server at serverURL: the
// protected resource metadata first (RFC 9728), then the authorization
// server's metadata (RFC 8414), and the 2025-03-26 defaults last.
func discover(ctx context.Context, c *http.Client, serverURL string, hint *AuthRequiredError) (*OAuth, string, error) {
	u, err := url.Parse(serverURL)
	if err != nil {
		return nil, "", err
	}
	origin := u.Scheme + "://" + u.Host
	o := &OAuth{Resource: canonicalResource(u)}
	var prm protectedResource
	var prmURLs []string
	if hint != nil && hint.ResourceMetadata != "" {
		prmURLs = append(prmURLs, hint.ResourceMetadata)
	}
	if p := strings.TrimRight(u.Path, "/"); p != "" {
		prmURLs = append(prmURLs, origin+"/.well-known/oauth-protected-resource"+p)
	}
	prmURLs = append(prmURLs, origin+"/.well-known/oauth-protected-resource")
	issuer := origin
	for _, p := range prmURLs {
		if getJSON(ctx, c, p, &prm) == nil && len(prm.AuthorizationServers) > 0 {
			issuer = strings.TrimRight(prm.AuthorizationServers[0], "/")
			if prm.Resource != "" {
				o.Resource = prm.Resource
			}
			break
		}
	}
	o.Issuer = issuer
	if hint != nil && hint.Scope != "" {
		o.Scope = hint.Scope
	} else if len(prm.ScopesSupported) > 0 {
		o.Scope = strings.Join(prm.ScopesSupported, " ")
	}
	var meta authServerMeta
	found := false
	for _, m := range metadataURLs(issuer) {
		if getJSON(ctx, c, m, &meta) == nil && meta.AuthorizationEndpoint != "" && meta.TokenEndpoint != "" {
			found = true
			break
		}
	}
	if !found {
		// Servers from before RFC 9728 support use these paths.
		meta = authServerMeta{AuthorizationEndpoint: issuer + "/authorize", TokenEndpoint: issuer + "/token", RegistrationEndpoint: issuer + "/register"}
	}
	o.AuthorizeURL, o.TokenURL = meta.AuthorizationEndpoint, meta.TokenEndpoint
	if o.Scope == "" && len(meta.ScopesSupported) > 0 && len(meta.ScopesSupported) <= 8 {
		o.Scope = strings.Join(meta.ScopesSupported, " ")
	}
	return o, meta.RegistrationEndpoint, nil
}

// metadataURLs are where an issuer's metadata may be, path-aware first.
func metadataURLs(issuer string) []string {
	u, err := url.Parse(issuer)
	if err != nil {
		return nil
	}
	origin := u.Scheme + "://" + u.Host
	p := strings.TrimRight(u.Path, "/")
	if p == "" {
		return []string{origin + "/.well-known/oauth-authorization-server", origin + "/.well-known/openid-configuration"}
	}
	return []string{
		origin + "/.well-known/oauth-authorization-server" + p,
		origin + "/.well-known/openid-configuration" + p,
		origin + p + "/.well-known/openid-configuration",
	}
}

// canonicalResource is the server URL as RFC 8707 resource indicators use it.
func canonicalResource(u *url.URL) string {
	c := *u
	c.Scheme = strings.ToLower(c.Scheme)
	c.Host = strings.ToLower(c.Host)
	c.Fragment, c.RawQuery = "", ""
	return strings.TrimRight(c.String(), "/")
}

// register registers Yggdrasil with the authorization server (RFC 7591),
// unless the person gave a client id.
func register(ctx context.Context, c *http.Client, o *OAuth, registrationURL, clientID, clientSecret string) error {
	if clientID != "" {
		o.ClientID, o.ClientSecret = clientID, clientSecret
		return nil
	}
	if registrationURL == "" {
		return errors.New("this service does not let apps register themselves. Add a client ID under Advanced")
	}
	body := map[string]any{
		"client_name":                "Toskar",
		"client_uri":                 "https://github.com/yeixio/yggdrasil-core",
		"redirect_uris":              []string{o.RedirectURI},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	}
	if o.Scope != "" {
		body["scope"] = o.Scope
	}
	var res struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	if err := postJSON(ctx, c, registrationURL, body, &res); err != nil {
		return fmt.Errorf("could not register Toskar with the service: %w", err)
	}
	if res.ClientID == "" {
		return errors.New("the service did not give Toskar a client ID")
	}
	o.ClientID, o.ClientSecret = res.ClientID, res.ClientSecret
	return nil
}

// authorizeURL starts a sign-in with PKCE. It returns the address to open
// and the verifier the exchange needs.
func (o *OAuth) authorizeURL(state string) (string, string) {
	verifier := randomString(48)
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {o.ClientID},
		"redirect_uri":          {o.RedirectURI},
		"state":                 {state},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
	}
	if o.Resource != "" {
		q.Set("resource", o.Resource)
	}
	if o.Scope != "" {
		q.Set("scope", o.Scope)
	}
	sep := "?"
	if strings.Contains(o.AuthorizeURL, "?") {
		sep = "&"
	}
	return o.AuthorizeURL + sep + q.Encode(), verifier
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Error        string `json:"error"`
	Description  string `json:"error_description"`
}

// exchange trades an authorization code for tokens.
func (o *OAuth) exchange(ctx context.Context, c *http.Client, code, verifier string) error {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {o.RedirectURI},
		"client_id":     {o.ClientID},
		"code_verifier": {verifier},
	}
	return o.token(ctx, c, form)
}

// refresh gets a new access token with the refresh token.
func (o *OAuth) refresh(ctx context.Context, c *http.Client) error {
	if o.RefreshToken == "" {
		return errors.New("the sign-in expired. Sign in again")
	}
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {o.RefreshToken}, "client_id": {o.ClientID}}
	return o.token(ctx, c, form)
}

func (o *OAuth) token(ctx context.Context, c *http.Client, form url.Values) error {
	if o.Resource != "" {
		form.Set("resource", o.Resource)
	}
	if o.ClientSecret != "" {
		form.Set("client_secret", o.ClientSecret)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach the sign-in service: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var tr tokenResponse
	if err := json.Unmarshal(raw, &tr); err != nil {
		// Some servers answer form-encoded.
		if v, perr := url.ParseQuery(string(raw)); perr == nil {
			tr.AccessToken, tr.RefreshToken, tr.Error = v.Get("access_token"), v.Get("refresh_token"), v.Get("error")
		}
	}
	if tr.Error != "" || tr.AccessToken == "" {
		msg := firstNonEmpty(tr.Description, tr.Error, fmt.Sprintf("HTTP %d", resp.StatusCode))
		return fmt.Errorf("sign-in was not accepted: %s", msg)
	}
	o.AccessToken = tr.AccessToken
	if tr.RefreshToken != "" {
		o.RefreshToken = tr.RefreshToken
	}
	o.ExpiresAt = time.Time{}
	if tr.ExpiresIn > 0 {
		o.ExpiresAt = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	}
	return nil
}

// expiring reports whether the access token ends within a minute.
func (o *OAuth) expiring() bool {
	return !o.ExpiresAt.IsZero() && time.Until(o.ExpiresAt) < time.Minute
}

func getJSON(ctx context.Context, c *http.Client, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}

func postJSON(ctx context.Context, c *http.Client, u string, body, out any) error {
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(string(raw)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return httpErr(resp.StatusCode, data)
	}
	return json.Unmarshal(data, out)
}

func randomString(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)[:n]
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
