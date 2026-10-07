package auth_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/store"
)

func newPairer(t *testing.T) (*auth.DevicePairer, *auth.APIKeyManager, *time.Time) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	keys := auth.NewAPIKeyManager(db.SQL, auth.NewSecretStore(dir))
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	return &auth.DevicePairer{CreateKey: keys.CreateDevice, Now: func() time.Time { return now }}, keys, &now
}

// A phone sends the code shown and gets a key of its own, once (#216).
func TestDevicePairing(t *testing.T) {
	p, keys, _ := newPairer(t)
	ctx := context.Background()
	if _, _, err := p.Pair(ctx, "123456", "Phone", "192.168.1.20"); !errors.Is(err, auth.ErrNoDeviceCode) {
		t.Fatalf("no code shown: %v", err)
	}
	shown, err := p.Start()
	if err != nil || len(shown.Code) != 6 || shown.State != "waiting" {
		t.Fatalf("start = %+v %v", shown, err)
	}
	if st := p.Status(); st.Code != "" || st.State != "waiting" {
		t.Fatalf("status must not repeat the code: %+v", st)
	}
	rec, secret, err := p.Pair(ctx, shown.Code[:3]+" "+shown.Code[3:], "  Mike's\niPhone  ", "192.168.1.20")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Kind != auth.KindDevice || rec.Name != "Mike's iPhone" || !strings.HasPrefix(secret, "ygg_") {
		t.Fatalf("key = %+v %q", rec, secret)
	}
	verified, err := keys.Verify(ctx, secret)
	if err != nil || verified.Kind != auth.KindDevice {
		t.Fatalf("verify = %+v %v", verified, err)
	}
	if st := p.Status(); st.State != "connected" || st.Device == nil || st.Device.ID != rec.ID {
		t.Fatalf("status after = %+v", st)
	}
	if _, _, err := p.Pair(ctx, shown.Code, "Another", "192.168.1.21"); !errors.Is(err, auth.ErrNoDeviceCode) {
		t.Fatalf("a code works once: %v", err)
	}
	// Rotating a phone's key keeps it a phone's key.
	rotated, _, err := keys.Rotate(ctx, rec.ID)
	if err != nil || rotated.Kind != auth.KindDevice {
		t.Fatalf("rotate = %+v %v", rotated, err)
	}
}

func TestDevicePairingLimits(t *testing.T) {
	p, _, now := newPairer(t)
	ctx := context.Background()
	shown, _ := p.Start()
	wrong := "000000"
	if shown.Code == wrong {
		wrong = "111111"
	}
	for i := 0; i < 5; i++ {
		if _, _, err := p.Pair(ctx, wrong, "x", "192.168.1.30"); !errors.Is(err, auth.ErrWrongDeviceCode) {
			t.Fatalf("miss %d: %v", i, err)
		}
	}
	if _, _, err := p.Pair(ctx, shown.Code, "x", "192.168.1.31"); !errors.Is(err, auth.ErrNoDeviceCode) {
		t.Fatalf("five misses cancel the code: %v", err)
	}
	if p.Status().State != "cancelled" {
		t.Fatalf("state = %q", p.Status().State)
	}

	// A code expires after ten minutes.
	shown, _ = p.Start()
	*now = now.Add(11 * time.Minute)
	if _, _, err := p.Pair(ctx, shown.Code, "x", "192.168.1.32"); !errors.Is(err, auth.ErrDeviceCodeExpired) {
		t.Fatalf("expired: %v", err)
	}

	// One address gets ten tries in ten minutes, right or wrong.
	for i := 0; i < 10; i++ {
		_, _ = p.Start()
		_, _, _ = p.Pair(ctx, wrong, "x", "192.168.1.40")
	}
	shown, _ = p.Start()
	if _, _, err := p.Pair(ctx, shown.Code, "x", "192.168.1.40"); !errors.Is(err, auth.ErrDeviceThrottled) {
		t.Fatalf("throttled: %v", err)
	}
	if _, _, err := p.Pair(ctx, shown.Code, "x", "192.168.1.41"); err != nil {
		t.Fatalf("another address: %v", err)
	}
}

func TestCleanDeviceName(t *testing.T) {
	if got := auth.CleanDeviceName(""); got != "Device" {
		t.Errorf("empty = %q", got)
	}
	if got := auth.CleanDeviceName("a\x00b\tc"); got != "ab c" {
		t.Errorf("controls = %q", got)
	}
	if got := auth.CleanDeviceName(strings.Repeat("é", 80)); len([]rune(got)) != 60 {
		t.Errorf("long = %d runes", len([]rune(got)))
	}
}

func TestFromLocalNetwork(t *testing.T) {
	for addr, want := range map[string]bool{
		"192.168.1.20:5000":   true,
		"10.0.0.5:5000":       true,
		"172.20.1.1:5000":     true,
		"100.101.102.103:1":   true,
		"127.0.0.1:5000":      true,
		"[fe80::1]:5000":      true,
		"[fd12::1]:5000":      true,
		"[::ffff:10.0.0.5]:1": true,
		"8.8.8.8:5000":        false,
		"[2001:db8::1]:5000":  false,
	} {
		r := httptest.NewRequest("POST", "/api/v1/devices/pair", nil)
		r.RemoteAddr = addr
		if got := auth.FromLocalNetwork(r); got != want {
			t.Errorf("%s = %v, want %v", addr, got, want)
		}
	}
	r := httptest.NewRequest("POST", "/api/v1/devices/pair", nil)
	r.RemoteAddr = "127.0.0.1:5000"
	r.Header.Set("X-Forwarded-For", "8.8.8.8")
	if auth.FromLocalNetwork(r) {
		t.Error("a request through a proxy isn't local")
	}
}

func TestDeviceMayReach(t *testing.T) {
	for route, want := range map[string]bool{
		"POST /api/v1/chat":                       true,
		"GET /api/v1/conversations/{id}/messages": true,
		"get /api/v1/health":                      true,
		"GET /api/v1/api-keys":                    false,
		"PATCH /api/v1/settings":                  false,
		"POST /api/v1/devices/pairing":            false,
		"POST /api/v1/models/{id}/install":        false,
	} {
		method, path, _ := strings.Cut(route, " ")
		if got := auth.DeviceMayReach(method, path); got != want {
			t.Errorf("%s = %v, want %v", route, got, want)
		}
	}
}
