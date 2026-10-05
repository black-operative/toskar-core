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
// hardware, as the setup screen does, and waits until they are installed.
// A runner that keeps its models between runs pays for this once.
func (d realDriver) installRecommended(t *testing.T) {
	t.Helper()
	var rec contracts.Recommendation
	d.do(t, http.MethodGet, "/api/v1/models/recommend", nil, &rec)
	var waiting []string
	for _, m := range rec.Models {
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
