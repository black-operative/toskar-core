package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/internal/store"
)

// A phone connects with the code the computer shows, from the local
// network and without a key, and its key reaches only what a phone uses
// (#216).
func TestConnectAPhone(t *testing.T) {
	dir := t.TempDir()
	mgr, err := config.NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Update(func(c *config.Config) { c.APIHost = "0.0.0.0" }); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	keys := auth.NewAPIKeyManager(db.SQL, auth.NewSecretStore(dir))
	srv := NewServer(Dependencies{
		Config:       mgr,
		VerifyAPIKey: keys.Verify,
		ListAPIKeys:  keys.List,
		Devices:      &auth.DevicePairer{CreateKey: keys.CreateDevice},
		PhoneAddress: func() (string, bool) { return "192.168.1.10:7331", true },
	})
	do := func(r *http.Request) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, r)
		return rec
	}

	// The computer's own UI shows a code.
	start := do(localRequest(http.MethodPost, "/api/v1/devices/pairing", nil))
	if start.Code != http.StatusCreated {
		t.Fatalf("start: %d %s", start.Code, start.Body.String())
	}
	var shown struct {
		Code      string `json:"code"`
		Address   string `json:"address"`
		Reachable bool   `json:"reachable"`
	}
	_ = json.Unmarshal(start.Body.Bytes(), &shown)
	if len(shown.Code) != 6 || shown.Address != "192.168.1.10:7331" || !shown.Reachable {
		t.Fatalf("shown = %+v", shown)
	}

	// Nobody on the network can make a code without a key.
	remoteStart := httptest.NewRequest(http.MethodPost, "/api/v1/devices/pairing", nil)
	remoteStart.RemoteAddr = "192.168.1.20:5000"
	if got := do(remoteStart); got.Code != http.StatusUnauthorized {
		t.Fatalf("remote start without a key: %d", got.Code)
	}

	pair := func(addr, code string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/devices/pair", strings.NewReader(`{"code":"`+code+`","device_name":"Mike's iPhone"}`))
		r.RemoteAddr = addr
		return do(r)
	}
	if got := pair("8.8.8.8:5000", shown.Code); got.Code != http.StatusForbidden || !strings.Contains(got.Body.String(), "PAIRING_NOT_LOCAL") {
		t.Fatalf("from the internet: %d %s", got.Code, got.Body.String())
	}
	wrong := "000000"
	if shown.Code == wrong {
		wrong = "111111"
	}
	if got := pair("192.168.1.20:5000", wrong); got.Code != http.StatusUnauthorized || !strings.Contains(got.Body.String(), "PAIRING_WRONG_CODE") {
		t.Fatalf("wrong code: %d %s", got.Code, got.Body.String())
	}
	paired := pair("192.168.1.20:5000", shown.Code)
	if paired.Code != http.StatusCreated {
		t.Fatalf("pair: %d %s", paired.Code, paired.Body.String())
	}
	var got struct {
		APIKey string            `json:"api_key"`
		Key    auth.APIKeyRecord `json:"key"`
	}
	_ = json.Unmarshal(paired.Body.Bytes(), &got)
	if got.Key.Kind != auth.KindDevice || got.Key.Name != "Mike's iPhone" || got.APIKey == "" {
		t.Fatalf("paired = %+v", got)
	}

	// The computer sees who connected, without the code.
	status := do(localRequest(http.MethodGet, "/api/v1/devices/pairing", nil))
	if !strings.Contains(status.Body.String(), `"state":"connected"`) || !strings.Contains(status.Body.String(), "Mike's iPhone") || strings.Contains(status.Body.String(), shown.Code) {
		t.Fatalf("status = %s", status.Body.String())
	}

	phone := func(method, path string) int {
		r := httptest.NewRequest(method, path, nil)
		r.RemoteAddr = "192.168.1.20:5000"
		r.Header.Set("Authorization", "Bearer "+got.APIKey)
		return do(r).Code
	}
	if code := phone(http.MethodGet, "/api/v1/health"); code != http.StatusOK {
		t.Fatalf("phone health: %d", code)
	}
	for _, path := range []string{"/api/v1/api-keys", "/api/v1/devices/pairing"} {
		if code := phone(http.MethodGet, path); code != http.StatusForbidden {
			t.Fatalf("phone %s: %d", path, code)
		}
	}
	if code := phone(http.MethodPost, "/api/v1/devices/pairing"); code != http.StatusForbidden {
		t.Fatalf("a phone can't make codes: %d", code)
	}

	list, _ := keys.List(context.Background())
	if len(list) != 1 || list[0].Kind != auth.KindDevice {
		t.Fatalf("keys = %+v", list)
	}
}
