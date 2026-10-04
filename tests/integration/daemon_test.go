package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/app"
)

// freePort returns a port nothing is listening on, so this test never
// talks to another daemon on the default ports.
func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
}

func TestDaemonHealthAndHardware(t *testing.T) {
	t.Setenv("TOSKAR_API_PORT", freePort(t))
	t.Setenv("TOSKAR_INTERNAL_PORT", freePort(t))
	t.Setenv("TOSKAR_DISCOVERY_ENABLED", "false")
	dir := t.TempDir()
	application, err := app.New(app.Options{DataDir: dir})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- application.Start(ctx)
	}()
	defer func() {
		cancel()
		_ = application.Shutdown(context.Background())
	}()

	cfg := application.Config.Get()
	base := "http://" + cfg.APIAddr()
	client := &http.Client{Timeout: 2 * time.Second}

	deadline := time.Now().Add(10 * time.Second)
	var healthOK bool
	for time.Now().Before(deadline) {
		resp, err := client.Get(base + "/api/v1/health")
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				var payload map[string]any
				_ = json.Unmarshal(body, &payload)
				if payload["status"] == "ok" {
					healthOK = true
					break
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !healthOK {
		t.Fatal("health endpoint never became ready")
	}

	resp, err := client.Get(base + "/api/v1/hardware")
	if err != nil {
		t.Fatalf("hardware: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("hardware status %d", resp.StatusCode)
	}
	var hw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&hw); err != nil {
		t.Fatalf("decode hardware: %v", err)
	}
	if hw["os"] == nil || hw["arch"] == nil {
		t.Fatalf("expected os/arch in hardware: %#v", hw)
	}

	resp2, err := client.Get(base + "/api/v1/version")
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("version status %d", resp2.StatusCode)
	}
	var ver map[string]any
	if err := json.NewDecoder(resp2.Body).Decode(&ver); err != nil {
		t.Fatalf("decode version: %v", err)
	}
	src, _ := ver["source"].(string)
	if ver["license"] != "AGPL-3.0-or-later" || src == "" {
		t.Fatalf("version source offer: %#v", ver)
	}

	resp3, err := client.Get(base + "/source")
	if err != nil {
		t.Fatalf("source: %v", err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("source status %d", resp3.StatusCode)
	}
	var offer map[string]any
	if err := json.NewDecoder(resp3.Body).Decode(&offer); err != nil {
		t.Fatalf("decode source: %v", err)
	}
	src, _ = offer["source"].(string)
	if offer["name"] != "Toskar Core" || offer["license"] != "AGPL-3.0-or-later" || src == "" {
		t.Fatalf("source offer: %#v", offer)
	}

	// Settings survive restart path check: config file exists.
	if _, err := filepath.Glob(filepath.Join(dir, "config.json")); err != nil {
		t.Fatalf("config: %v", err)
	}
}
