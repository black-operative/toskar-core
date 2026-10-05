package quality

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// installWithin bounds how long the first run on a new runner may spend
// downloading the recommended models; later runs find them installed.
const installWithin = 90 * time.Minute

// installRecommended installs the models the daemon recommends for its
// hardware, and the runtimes they run on, as the setup screen does, and
// waits until they are installed.
// A runner that keeps its models between runs pays for this once.
func (d realDriver) installRecommended(t *testing.T) {
	t.Helper()
	var rec contracts.Recommendation
	d.do(t, http.MethodGet, "/api/v1/models/recommend", nil, &rec)
	d.installModels(t, rec.Models)
}

// installModel installs one catalog model, such as qwen2.5-32b-q4, and its
// runtime, for a run that tests that model (TOSKAR_QUALITY_MODEL).
func (d realDriver) installModel(t *testing.T, id string) {
	t.Helper()
	var models []contracts.Model
	d.do(t, http.MethodGet, "/api/v1/models", nil, &models)
	for _, m := range models {
		if m.ID == id {
			d.installModels(t, []contracts.Model{m})
			return
		}
	}
	t.Fatalf("%s is not in the daemon's catalog", id)
}

// installModels installs models and their runtimes and waits until they
// are installed.
func (d realDriver) installModels(t *testing.T, models []contracts.Model) {
	t.Helper()
	d.installRuntimes(t, models)
	var waiting []string
	for _, m := range models {
		if m.Installed {
			continue
		}
		t.Logf("installing %s", m.ID)
		d.do(t, http.MethodPost, "/api/v1/models/"+m.ID+"/install", nil, nil)
		waiting = append(waiting, m.ID)
	}
	deadline := time.Now().Add(installWithin)
	for len(waiting) > 0 {
		var models []contracts.Model
		d.do(t, http.MethodGet, "/api/v1/models", nil, &models)
		byID := map[string]contracts.Model{}
		for _, m := range models {
			byID[m.ID] = m
		}
		var left []string
		for _, id := range waiting {
			switch m := byID[id]; {
			case m.Installed:
				t.Logf("installed %s", id)
			case m.Status == "error":
				t.Fatalf("installing %s failed", id)
			default:
				left = append(left, id)
			}
		}
		waiting = left
		if len(waiting) > 0 {
			if time.Now().After(deadline) {
				t.Fatalf("still installing after %s: %v", installWithin, waiting)
			}
			time.Sleep(15 * time.Second)
		}
	}
}

// installRuntimes installs the runtimes, such as llama.cpp, that models
// run on and the daemon doesn't have yet. A fresh daemon has none, and a
// chat with an installed model and no runtime fails.
func (d realDriver) installRuntimes(t *testing.T, models []contracts.Model) {
	t.Helper()
	need := map[string]bool{}
	for _, m := range models {
		for _, id := range m.Runtime {
			need[id] = true
		}
	}
	var runtimes []struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	d.do(t, http.MethodGet, "/api/v1/runtimes", nil, &runtimes)
	for _, rt := range runtimes {
		if !need[rt.ID] || rt.Status == "installed" {
			continue
		}
		t.Logf("installing runtime %s", rt.ID)
		d.do(t, http.MethodPost, "/api/v1/runtimes/"+rt.ID+"/install", nil, nil)
		t.Logf("installed runtime %s", rt.ID)
	}
}

// hardware says what the daemon runs models on, for the report: its
// largest graphics card and its memory, or the CPU.
func (d realDriver) hardware(t *testing.T) string {
	t.Helper()
	var hw contracts.HardwareInventory
	d.do(t, http.MethodGet, "/api/v1/hardware", nil, &hw)
	var best *contracts.Accelerator
	for i, a := range hw.Accelerators {
		if a.Kind == "gpu" && (best == nil || a.DedicatedVRAM > best.DedicatedVRAM) {
			best = &hw.Accelerators[i]
		}
	}
	if best == nil {
		return fmt.Sprintf("the CPU (%d threads, %.0f GB)", hw.CPU.Threads, float64(hw.Memory.TotalBytes)/(1<<30))
	}
	if best.DedicatedVRAM == 0 {
		return best.Model
	}
	return fmt.Sprintf("%s (%.0f GB)", best.Model, float64(best.DedicatedVRAM)/(1<<30))
}

// version says which build of the daemon the run tested, for the report.
func (d realDriver) version(t *testing.T) string {
	t.Helper()
	var v contracts.VersionResponse
	d.do(t, http.MethodGet, "/api/v1/version", nil, &v)
	if v.Commit == "" {
		return v.Version
	}
	return fmt.Sprintf("%s (%s)", v.Version, v.Commit)
}
