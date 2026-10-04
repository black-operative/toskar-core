package app

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/config"
)

func TestStartRefusesNonLoopbackWithoutAPIKey(t *testing.T) {
	t.Setenv("YGGDRASIL_API_HOST", "0.0.0.0")
	t.Setenv("YGGDRASIL_DISCOVERY_ENABLED", "false")
	application, err := New(Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Shutdown(context.Background()) })
	err = application.Start(context.Background())
	if !errors.Is(err, auth.ErrAPIKeyRequired) {
		t.Fatalf("start: %v", err)
	}
}

func TestLANEnableRequiresAPIKey(t *testing.T) {
	t.Setenv("YGGDRASIL_API_HOST", "127.0.0.1")
	t.Setenv("YGGDRASIL_DISCOVERY_ENABLED", "false")
	application, err := New(Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Shutdown(context.Background()) })
	err = application.applySettingsPatch(context.Background(), map[string]any{"lan_api_enabled": true})
	if !errors.Is(err, auth.ErrAPIKeyRequired) {
		t.Fatalf("lan enable: %v", err)
	}
}

func TestStartAcceptsBootstrapKey(t *testing.T) {
	apiPort := freePort(t)
	internalPort := freePort(t)
	const secret = "ygg_bootstrap_test_key"
	t.Setenv("YGGDRASIL_API_HOST", "0.0.0.0")
	t.Setenv("YGGDRASIL_API_PORT", strconv.Itoa(apiPort))
	t.Setenv("YGGDRASIL_INTERNAL_HOST", "127.0.0.1")
	t.Setenv("YGGDRASIL_INTERNAL_PORT", strconv.Itoa(internalPort))
	t.Setenv("YGGDRASIL_DISCOVERY_ENABLED", "false")
	t.Setenv("YGGDRASIL_API_KEY", secret)

	application, err := New(Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- application.Start(ctx) }()
	t.Cleanup(func() {
		cancel()
		_ = application.Shutdown(context.Background())
	})

	base := "http://127.0.0.1:" + strconv.Itoa(apiPort)
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(10 * time.Second)
	var denied bool
	for time.Now().Before(deadline) {
		resp, err := client.Get(base + "/api/v1/health")
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusUnauthorized {
				denied = true
				break
			}
		}
		select {
		case startErr := <-errCh:
			t.Fatalf("start: %v", startErr)
		default:
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !denied {
		t.Fatal("expected health without a key to be rejected")
	}

	req, err := http.NewRequest(http.MethodGet, base+"/api/v1/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bearer health: %d", resp.StatusCode)
	}
	if host := application.Config.Get().APIHost; !config.ListensBeyondLoopback(host) {
		t.Fatalf("api host %q", host)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}
