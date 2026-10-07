package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Connecting a phone (#216): the computer shows a 6-digit code, and the
// phone sends it with its name to get a key of its own, limited to what the
// phone uses. Only the code's hash is kept, in memory: a code lives ten
// minutes, so a restart may as well end it.

const (
	deviceCodeTTL = 10 * time.Minute
	// maxDeviceMisses wrong codes cancel the code being shown; the computer
	// then shows a new one.
	maxDeviceMisses = 5
	// maxDeviceTriesPerAddress attempts from one address in
	// deviceTryWindow are allowed, right or wrong.
	maxDeviceTriesPerAddress = 10
	deviceTryWindow          = 10 * time.Minute
	maxDeviceNameRunes       = 60
)

// Errors the pairing endpoint returns, with stable codes for the phone.
var (
	ErrNoDeviceCode      = contracts.NewError("PAIRING_NOT_STARTED", nil, errors.New("this computer isn't showing a code; choose Connect a device on it first"))
	ErrWrongDeviceCode   = contracts.NewError("PAIRING_WRONG_CODE", nil, errors.New("that code isn't the one this computer shows"))
	ErrDeviceCodeExpired = contracts.NewError("PAIRING_EXPIRED", nil, errors.New("the code has expired; choose Connect a device on the computer again"))
	ErrDeviceThrottled   = contracts.NewError("PAIRING_THROTTLED", nil, errors.New("too many tries; wait a few minutes, then try again"))
	ErrNotLocalNetwork   = contracts.NewError("PAIRING_NOT_LOCAL", nil, errors.New("a device can connect only from this computer's local network"))
)

// DevicePairing is the code being shown, and what became of it.
type DevicePairing struct {
	// Code is the 6 digits, returned only when the code is made.
	Code      string    `json:"code,omitempty"`
	ExpiresAt time.Time `json:"expires_at"`
	// State is waiting, connected, expired, or cancelled.
	State string `json:"state"`
	// Device names the phone that connected with the code.
	Device *APIKeyRecord `json:"device,omitempty"`
}

// DevicePairer makes pairing codes and exchanges them for device keys.
type DevicePairer struct {
	// CreateKey makes the phone's key; *APIKeyManager.CreateDevice.
	CreateKey func(ctx context.Context, name string) (APIKeyRecord, string, error)
	// Now is the clock; tests replace it.
	Now func() time.Time

	mu      sync.Mutex
	hash    [32]byte
	expires time.Time
	misses  int
	state   string
	device  *APIKeyRecord
	tries   map[string][]time.Time
}

func (p *DevicePairer) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

// Start shows a new code, replacing any other.
func (p *DevicePairer) Start() (DevicePairing, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return DevicePairing{}, err
	}
	code := fmt.Sprintf("%06d", n.Int64())
	p.mu.Lock()
	defer p.mu.Unlock()
	p.hash = sha256.Sum256([]byte(code))
	p.expires = p.now().Add(deviceCodeTTL)
	p.misses, p.state, p.device = 0, "waiting", nil
	return DevicePairing{Code: code, ExpiresAt: p.expires, State: p.state}, nil
}

// Status is the code's state: waiting, connected, expired, cancelled, or
// "" when none was made.
func (p *DevicePairer) Status() DevicePairing {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state == "waiting" && !p.now().Before(p.expires) {
		p.state = "expired"
	}
	return DevicePairing{ExpiresAt: p.expires, State: p.state, Device: p.device}
}

// Cancel stops showing the code.
func (p *DevicePairer) Cancel() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state == "waiting" {
		p.state = "cancelled"
	}
}

// Pair exchanges a code for a key named after the phone. address is where
// the request came from, for the per-address limit.
func (p *DevicePairer) Pair(ctx context.Context, code, deviceName, address string) (APIKeyRecord, string, error) {
	code = strings.Map(func(r rune) rune {
		if unicode.IsDigit(r) {
			return r
		}
		return -1
	}, code)
	name := CleanDeviceName(deviceName)

	p.mu.Lock()
	now := p.now()
	if !p.allowTry(address, now) {
		p.mu.Unlock()
		return APIKeyRecord{}, "", ErrDeviceThrottled
	}
	switch {
	case p.state != "waiting":
		p.mu.Unlock()
		if p.state == "expired" {
			return APIKeyRecord{}, "", ErrDeviceCodeExpired
		}
		return APIKeyRecord{}, "", ErrNoDeviceCode
	case !now.Before(p.expires):
		p.state = "expired"
		p.mu.Unlock()
		return APIKeyRecord{}, "", ErrDeviceCodeExpired
	}
	got := sha256.Sum256([]byte(code))
	if len(code) != 6 || subtle.ConstantTimeCompare(got[:], p.hash[:]) != 1 {
		p.misses++
		if p.misses >= maxDeviceMisses {
			p.state = "cancelled"
		}
		p.mu.Unlock()
		return APIKeyRecord{}, "", ErrWrongDeviceCode
	}
	// Single use: the code is spent before the key is made.
	p.state = "connecting"
	p.mu.Unlock()

	rec, secret, err := p.CreateKey(ctx, name)
	p.mu.Lock()
	defer p.mu.Unlock()
	if err != nil {
		p.state = "cancelled"
		return APIKeyRecord{}, "", err
	}
	p.state, p.device = "connected", &rec
	return rec, secret, nil
}

// allowTry counts an attempt from address; false once it has made too many.
func (p *DevicePairer) allowTry(address string, now time.Time) bool {
	if p.tries == nil {
		p.tries = map[string][]time.Time{}
	}
	var recent []time.Time
	for _, t := range p.tries[address] {
		if now.Sub(t) < deviceTryWindow {
			recent = append(recent, t)
		}
	}
	if len(recent) >= maxDeviceTriesPerAddress {
		p.tries[address] = recent
		return false
	}
	p.tries[address] = append(recent, now)
	return true
}

// CleanDeviceName is a phone's name as a key's name: printable, trimmed, and
// at most 60 characters; "Device" when it gives none.
func CleanDeviceName(name string) string {
	name = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsSpace(r):
			return ' '
		case unicode.IsControl(r):
			return -1
		}
		return r
	}, name)
	name = strings.Join(strings.Fields(name), " ")
	if r := []rune(name); len(r) > maxDeviceNameRunes {
		name = string(r[:maxDeviceNameRunes])
	}
	if name == "" {
		name = "Device"
	}
	return name
}

// FromLocalNetwork reports whether a request comes from this computer or a
// private address on its network: 10/8, 172.16/12, 192.168/16, 100.64/10
// (carrier-grade and Tailscale), link-local, or IPv6 unique-local. A request
// through a proxy isn't, since the proxy's address says nothing about the
// caller, as in FromThisComputer.
func FromLocalNetwork(r *http.Request) bool {
	for _, h := range []string{"Forwarded", "X-Forwarded-For", "X-Real-IP"} {
		if r.Header.Get(h) != "" {
			return false
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	return addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || cgnat.Contains(addr)
}

var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// deviceRoutes are what a phone's key may reach: chat, its conversations,
// the live events, answering a tool's question, and the lists the phone
// reads. Everything else, such as API keys, settings changes, models,
// computers, and files on disk, needs a key made in API Access.
var deviceRoutes = map[string]bool{
	"GET /api/v1/health":                      true,
	"GET /api/v1/version":                     true,
	"GET /api/v1/settings":                    true,
	"GET /api/v1/profiles":                    true,
	"GET /api/v1/models":                      true,
	"GET /api/v1/models/running":              true,
	"GET /api/v1/nodes":                       true,
	"GET /api/v1/events":                      true,
	"POST /api/v1/chat":                       true,
	"POST /api/v1/chat/stop":                  true,
	"POST /api/v1/tools/decide":               true,
	"GET /api/v1/conversations":               true,
	"POST /api/v1/conversations":              true,
	"PATCH /api/v1/conversations/{id}":        true,
	"DELETE /api/v1/conversations/{id}":       true,
	"GET /api/v1/conversations/{id}/messages": true,
	"GET /api/v1/artifacts/{id}":              true,
	"GET /api/v1/artifacts/{id}/content":      true,
}

// DeviceMayReach reports whether a phone's key may make a request, by its
// method and route template, such as "GET /api/v1/conversations/{id}".
func DeviceMayReach(method, route string) bool {
	return deviceRoutes[strings.ToUpper(method)+" "+route]
}
