package screenshot

import (
	"encoding/json"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestHandlerDemoAPIAndUI(t *testing.T) {
	web := fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("<!doctype html><title>ui</title>")},
		"assets/app.js": &fstest.MapFile{Data: []byte("console.log(1)")},
	}
	server := httptest.NewServer(Handler(web))
	t.Cleanup(server.Close)

	health, err := http.Get(server.URL + "/api/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(health.Body)
	health.Body.Close()
	if health.StatusCode != http.StatusOK || !strings.Contains(string(body), `"status":"ok"`) {
		t.Fatalf("health: %d %s", health.StatusCode, body)
	}

	page, err := http.Get(server.URL + "/chat")
	if err != nil {
		t.Fatal(err)
	}
	pageBody, _ := io.ReadAll(page.Body)
	page.Body.Close()
	if !strings.Contains(string(pageBody), "<title>ui</title>") {
		t.Fatalf("spa fallback: %s", pageBody)
	}

	asset, err := http.Get(server.URL + "/assets/app.js")
	if err != nil {
		t.Fatal(err)
	}
	assetBody, _ := io.ReadAll(asset.Body)
	asset.Body.Close()
	if string(assetBody) != "console.log(1)" {
		t.Fatalf("asset: %s", assetBody)
	}
}

// The Diagnostics page reads the capability inventory; an empty list in its
// place blanked the page in the release screenshots.
func TestCapabilitiesFixtureHasTheInventoryShape(t *testing.T) {
	body, ok := screenshotGET("/api/v1/capabilities")
	if !ok {
		t.Fatal("no capabilities fixture")
	}
	var snap map[string]any
	if err := json.Unmarshal([]byte(body), &snap); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"models", "nodes", "tools", "connectors", "providers", "abilities"} {
		if _, isList := snap[key].([]any); !isList {
			t.Errorf("%s is not a list", key)
		}
	}
}

func TestMCPShareFixtureHasTheShareShape(t *testing.T) {
	body, ok := screenshotGET("/api/v1/mcp/share")
	if !ok {
		t.Fatal("no MCP share fixture")
	}
	var share struct {
		URL     string   `json:"url"`
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}
	if err := json.Unmarshal([]byte(body), &share); err != nil || share.URL == "" || share.Command == "" || len(share.Args) == 0 {
		t.Fatalf("share = %+v, %v", share, err)
	}
}

// The Performance page reads live figures and running models' acceleration
// in the contract's shapes, so the screenshots show the GPU card and pill.
func TestGPUFixturesHaveTheContractShape(t *testing.T) {
	body, ok := screenshotGET("/api/v1/performance/live")
	if !ok {
		t.Fatal("no live fixture")
	}
	var live []contracts.LiveFigures
	if err := json.Unmarshal([]byte(body), &live); err != nil {
		t.Fatal(err)
	}
	if len(live) != 1 || live[0].NodeID != "local" || len(live[0].Recent) != 60 || len(live[0].Current.GPUs) != 1 || live[0].Current.GPUs[0].BusyPercent == nil {
		t.Errorf("live: %+v", live)
	}
	body, _ = screenshotGET("/api/v1/models/running")
	var running []contracts.RunningModelView
	if err := json.Unmarshal([]byte(body), &running); err != nil {
		t.Fatal(err)
	}
	if len(running) == 0 || running[0].Acceleration == nil || running[0].Acceleration.State != contracts.AccelerationGPU {
		t.Errorf("running: %+v", running)
	}
}
