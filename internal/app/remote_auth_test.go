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

	// The test reaches the daemon over loopback, which needs no key even
	// with network access on; a forwarding header stands in for a request
	// from another machine.
	base := "http://127.0.0.1:" + strconv.Itoa(apiPort)
	client := &http.Client{Timeout: 2 * time.Second}
	get := func(forwarded bool, key string) int {
		req, err := http.NewRequest(http.MethodGet, base+"/api/v1/health", nil)
		if err != nil {
			t.Fatal(err)
		}
		if forwarded {
			req.Header.Set("X-Forwarded-For", "192.168.1.20")
		}
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		resp, err := client.Do(req)
		if err != nil {
			return 0
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	deadline := time.Now().Add(10 * time.Second)
	for get(false, "") != http.StatusOK {
		select {
		case startErr := <-errCh:
			t.Fatalf("start: %v", startErr)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("health from this computer without a key never answered")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if code := get(true, ""); code != http.StatusUnauthorized {
		t.Fatalf("health from the network without a key: %d", code)
	}
	if code := get(true, secret); code != http.StatusOK {
		t.Fatalf("bearer health: %d", code)
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
