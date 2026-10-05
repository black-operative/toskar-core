package api

import (
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/config"
)

// desktopOrigins are the origins the Toskar desktop webview sends: Wails
// serves its pages from wails://wails on macOS and Linux, and from
// http(s)://wails.localhost on Windows. The desktop proxies /api/ and /v1/
// to this daemon and passes the webview's Origin through.
var desktopOrigins = map[string]bool{
	"wails://wails":           true,
	"http://wails.localhost":  true,
	"https://wails.localhost": true,
}

// browserCheck decides whether a request may reach the API. Websites open in
// a browser on this computer can send requests to 127.0.0.1; this keeps their
// JavaScript from using or reading the API.
//
//   - Host must name this computer: a loopback name or IP, or, with network
//     access on, any IP (and any name from another device). A website's own
//     hostname over loopback is a DNS rebinding attempt.
//   - Origin, when present, must be the daemon's own web UI, the desktop
//     webview, or a loopback origin on the API port.
//
// A request with a valid API key passes both checks: no website holds one.
// Clients that are not browsers, such as toskarctl, send no Origin.
type browserCheck struct {
	originAllowed bool
	refused       refusal
}

type refusal int

const (
	notRefused refusal = iota
	refusedHost
	refusedOrigin
)

// writeRefusal answers a refused request; it reports false when the request
// may go on.
func (c browserCheck) writeRefusal(w http.ResponseWriter) bool {
	switch c.refused {
	case refusedHost:
		writeErr(w, http.StatusForbidden, "HOST_NOT_ALLOWED", "This address is not one of this computer's. Open Toskar at http://127.0.0.1 or http://localhost, or send an API key.", nil)
	case refusedOrigin:
		writeErr(w, http.StatusForbidden, "ORIGIN_NOT_ALLOWED", "Websites cannot use Toskar without an API key.", nil)
	default:
		return false
	}
	return true
}

func (s *Server) checkBrowser(r *http.Request) browserCheck {
	origin := r.Header.Get("Origin")
	hostOK := s.hostAllowed(r)
	originOK := origin == "" || (hostOK && s.originAllowed(r, origin))
	if hostOK && originOK {
		return browserCheck{originAllowed: origin != ""}
	}
	if r.Method == http.MethodOptions && hostOK && preflightSendsAuthorization(r) {
		// A preflight carries no key. Let it through when the request it
		// asks about will carry one; that request is checked on arrival.
		return browserCheck{originAllowed: true}
	}
	if s.hasValidKey(r) {
		return browserCheck{originAllowed: origin != ""}
	}
	if !hostOK {
		return browserCheck{refused: refusedHost}
	}
	return browserCheck{refused: refusedOrigin}
}

func (s *Server) hostAllowed(r *http.Request) bool {
	host := hostOnly(r.Host)
	if isLoopbackName(host) {
		return true
	}
	if !s.listensBeyondLoopback() {
		return false
	}
	if net.ParseIP(host) != nil {
		return true
	}
	// Another device may use this computer's name. Over loopback, a name we
	// do not know means a website's DNS now points at 127.0.0.1.
	return !remoteIsLoopback(r)
}

func (s *Server) originAllowed(r *http.Request, origin string) bool {
	if desktopOrigins[origin] {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || u.Path != "" || u.RawQuery != "" {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	// Same origin: the daemon's own web UI.
	if strings.EqualFold(u.Host, r.Host) {
		return true
	}
	if !isLoopbackName(u.Hostname()) {
		return false
	}
	port := u.Port()
	if port == "" {
		return false
	}
	if _, reqPort, err := net.SplitHostPort(r.Host); err == nil && port == reqPort {
		return true
	}
	if s.deps.Config != nil && port == strconv.Itoa(s.deps.Config.Get().APIPort) {
		return true
	}
	return false
}

func (s *Server) listensBeyondLoopback() bool {
	return s.deps.Config != nil && config.ListensBeyondLoopback(s.deps.Config.Get().APIHost)
}

func (s *Server) hasValidKey(r *http.Request) bool {
	if s.deps.VerifyAPIKey == nil {
		return false
	}
	token, err := auth.BearerToken(r)
	if err != nil {
		return false
	}
	_, err = s.deps.VerifyAPIKey(r.Context(), token)
	return err == nil
}

func preflightSendsAuthorization(r *http.Request) bool {
	if r.Header.Get("Access-Control-Request-Method") == "" {
		return false
	}
	for _, h := range strings.Split(r.Header.Get("Access-Control-Request-Headers"), ",") {
		if strings.EqualFold(strings.TrimSpace(h), "authorization") {
			return true
		}
	}
	return false
}

// hostOnly strips the port and IPv6 brackets from a Host header.
func hostOnly(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		hostport = h
	}
	return strings.ToLower(strings.Trim(hostport, "[]"))
}

func isLoopbackName(host string) bool {
	host = strings.ToLower(strings.Trim(host, "[]"))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func remoteIsLoopback(r *http.Request) bool {
	host := r.RemoteAddr
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
