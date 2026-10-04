package external

import (
	"context"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

func TestUnconfiguredRuntimeRefusesWork(t *testing.T) {
	rt := New(Config{})
	det, err := rt.Detect(context.Background())
	if err != nil || det.Installed {
		t.Fatalf("detect=%+v err=%v", det, err)
	}
	if err := rt.Health(context.Background()); err == nil {
		t.Fatal("health succeeded without a base URL")
	}
	if _, err := rt.StartModel(context.Background(), pluginapi.ModelStartConfig{ModelID: "m"}); err == nil {
		t.Fatal("start succeeded without a base URL")
	}
	if err := rt.Install(context.Background(), pluginapi.InstallOptions{}); err == nil || !strings.Contains(err.Error(), "base_url") {
		t.Fatalf("install=%v", err)
	}
}

func TestConfiguredRuntimeStaysRemote(t *testing.T) {
	rt := New(Config{BaseURL: "http://127.0.0.1:9/v1", APIKey: "local"})
	det, err := rt.Detect(context.Background())
	if err != nil || !det.Installed || det.Path != "http://127.0.0.1:9/v1" {
		t.Fatalf("detect=%+v err=%v", det, err)
	}
	if err := rt.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := rt.Update(context.Background()); err != nil {
		t.Fatal(err)
	}
	running, err := rt.StartModel(context.Background(), pluginapi.ModelStartConfig{ModelID: "remote-model"})
	if err != nil {
		t.Fatal(err)
	}
	if running.Endpoint != "http://127.0.0.1:9/v1" || running.Status != "remote" || running.RuntimeID != rt.ID() {
		t.Fatalf("running=%+v", running)
	}
	if err := rt.StopModel(context.Background(), running.ID); err != nil {
		t.Fatal(err)
	}
	listed, err := rt.ListRunning(context.Background())
	if err != nil || listed != nil {
		t.Fatalf("listed=%v err=%v", listed, err)
	}
	caps, err := rt.Capabilities(context.Background())
	if err != nil || !caps.SupportsStreaming || caps.SupportsGPU {
		t.Fatalf("caps=%+v err=%v", caps, err)
	}
}
