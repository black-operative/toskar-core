package app

import (
	"context"
	"sync"
	"testing"

	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/internal/discovery"
)

// fakeAdvertise replaces mDNS for the test and records the names announced.
func fakeAdvertise(t *testing.T) func() []string {
	t.Helper()
	var mu sync.Mutex
	var names []string
	prev := startAdvertise
	startAdvertise = func(cfg config.Config, _ bool) (*discovery.Advertiser, error) {
		mu.Lock()
		defer mu.Unlock()
		names = append(names, cfg.NodeName)
		return &discovery.Advertiser{}, nil
	}
	t.Cleanup(func() { startAdvertise = prev })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), names...)
	}
}

func newRenameTestApp(t *testing.T) *App {
	t.Helper()
	t.Setenv("YGGDRASIL_DISCOVERY_ENABLED", "true")
	a, err := New(Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Shutdown(context.Background()) })
	return a
}

// A rename, from a join or from Settings, re-announces the new name and
// shows it in the computer list.
func TestRenameAnnouncesTheNewName(t *testing.T) {
	announced := fakeAdvertise(t)
	a := newRenameTestApp(t)
	if err := a.rename("gpu-box"); err != nil {
		t.Fatal(err)
	}
	if err := a.applySettingsPatch(context.Background(), map[string]any{"node_name": "studio"}); err != nil {
		t.Fatal(err)
	}
	names := announced()
	if len(names) < 2 || names[len(names)-2] != "gpu-box" || names[len(names)-1] != "studio" {
		t.Fatalf("announced %v", names)
	}
	if got, _ := a.Nodes.List(context.Background()); got[0].Name != "studio" {
		t.Fatalf("computer list shows %q", got[0].Name)
	}
}

// Renames from different requests at once leave one announcement running.
func TestConcurrentRenamesShareOneAdvertiser(t *testing.T) {
	fakeAdvertise(t)
	a := newRenameTestApp(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				a.renamed("box")
			} else {
				a.restartAdvertiser()
			}
		}(i)
	}
	wg.Wait()
	a.discoveryMu.Lock()
	defer a.discoveryMu.Unlock()
	if a.advertiser == nil {
		t.Fatal("no advertiser after renames")
	}
}

func TestJoinedNameTakesTheIssuersName(t *testing.T) {
	for _, c := range []struct{ requested, issued, want string }{
		{"worker-01", "worker-01-2", "worker-01-2"},
		{"worker-01", "worker-01", "worker-01"},
		{"worker-01", "", "worker-01"},
		{"worker-01", "bad\x00name", "worker-01"},
	} {
		if got := joinedName(c.requested, c.issued); got != c.want {
			t.Errorf("joinedName(%q, %q) = %q, want %q", c.requested, c.issued, got, c.want)
		}
	}
}
