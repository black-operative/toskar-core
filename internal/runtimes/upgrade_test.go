package runtimes

import (
	"context"
	"reflect"
	"testing"

	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// upgradableRuntime is installed, and may have a better build to install.
type upgradableRuntime struct {
	staticRuntime
	better   bool
	installs int
}

func (r *upgradableRuntime) Detect(context.Context) (pluginapi.RuntimeDetection, error) {
	return pluginapi.RuntimeDetection{Installed: true}, nil
}
func (r *upgradableRuntime) Install(context.Context, pluginapi.InstallOptions) error {
	r.installs++
	r.better = false
	return nil
}
func (r *upgradableRuntime) UpgradeAvailable(context.Context) bool { return r.better }
func (r *upgradableRuntime) UpgradeBuild(ctx context.Context) (bool, error) {
	if !r.better {
		return false, nil
	}
	return true, r.Install(ctx, pluginapi.InstallOptions{Force: true})
}

func TestInstallReinstallsWhenABetterBuildFits(t *testing.T) {
	ctx := context.Background()
	rt := &upgradableRuntime{staticRuntime: staticRuntime{id: "llamacpp"}}
	registry := NewRegistry()
	registry.Register(rt)
	m := NewManager(registry, nil, nil, nil)

	if err := m.Install(ctx, "llamacpp", InstallOptions{}); err != nil || rt.installs != 0 {
		t.Fatalf("up to date: installs=%d err=%v", rt.installs, err)
	}
	rt.better = true
	if err := m.Install(ctx, "llamacpp", InstallOptions{}); err != nil || rt.installs != 1 {
		t.Fatalf("better build: installs=%d err=%v", rt.installs, err)
	}
}

func TestUpgradeBuilds(t *testing.T) {
	ctx := context.Background()
	stale := &upgradableRuntime{staticRuntime: staticRuntime{id: "stale"}, better: true}
	fresh := &upgradableRuntime{staticRuntime: staticRuntime{id: "fresh"}}
	registry := NewRegistry()
	registry.Register(stale)
	registry.Register(fresh)
	registry.Register(&staticRuntime{id: "plain"})
	m := NewManager(registry, nil, nil, nil)

	done, err := m.UpgradeBuilds(ctx)
	if err != nil || !reflect.DeepEqual(done, []string{"stale"}) {
		t.Fatalf("done=%v err=%v", done, err)
	}
	if fresh.installs != 0 {
		t.Fatal("an up-to-date runtime was reinstalled")
	}
}
