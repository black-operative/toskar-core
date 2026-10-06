package discovery

import (
	"slices"
	"testing"

	"github.com/yeixio/toskar-core/internal/config"
)

func TestAdvertisedTXTSaysWhereTheAPIIs(t *testing.T) {
	cfg := config.Config{NodeID: "node-1", NodeName: "Studio", APIPort: 8080, InternalPort: 7332}
	txt := advertisedTXT(cfg, true)
	for _, want := range []string{"node_id=node-1", "name=Studio", "pairing=true", "api_port=8080"} {
		if !slices.Contains(txt, want) {
			t.Errorf("TXT %v lacks %q", txt, want)
		}
	}
	if !slices.Contains(advertisedTXT(cfg, false), "pairing=false") {
		t.Error("pairing=false missing")
	}
	// Whether the API answers on the network, for the phone (#216).
	if !slices.Contains(txt, "api_lan=false") {
		t.Errorf("a loopback API should say api_lan=false: %v", txt)
	}
	cfg.APIHost = "0.0.0.0"
	if !slices.Contains(advertisedTXT(cfg, true), "api_lan=true") {
		t.Error("a network API should say api_lan=true")
	}
	// Discovery reads the record back the same way.
	if got := parseTXT(txt)["api_port"]; got != "8080" {
		t.Errorf("parsed api_port %q", got)
	}
}
