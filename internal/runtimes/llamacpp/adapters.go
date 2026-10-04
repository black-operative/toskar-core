package llamacpp

import (
	"fmt"
	"sync"

	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// llama-server applies every loaded LoRA adapter at full strength unless a
// request sets its scale, even when started with --lora-init-without-apply.
// The client therefore sends an explicit scale for each adapter on every
// request to an endpoint that has adapters, so a base-model chat stays base.
var (
	adaptersMu sync.RWMutex
	adaptersBy = map[string][]string{}
)

func registerAdapters(endpoint string, ids []string) {
	adaptersMu.Lock()
	defer adaptersMu.Unlock()
	if len(ids) == 0 {
		delete(adaptersBy, endpoint)
		return
	}
	adaptersBy[endpoint] = append([]string(nil), ids...)
}

// loraScales returns the per-request "lora" field for an endpoint: scale 1
// for the requested adapter and 0 for every other one. It returns nil when
// the endpoint has no adapters.
func loraScales(endpoint, adapter string) ([]map[string]any, error) {
	adaptersMu.RLock()
	ids := adaptersBy[endpoint]
	adaptersMu.RUnlock()
	if len(ids) == 0 {
		if adapter != "" {
			return nil, fmt.Errorf("adapter %s is not loaded on %s", adapter, endpoint)
		}
		return nil, nil
	}
	out := make([]map[string]any, len(ids))
	found := adapter == ""
	for i, id := range ids {
		scale := 0.0
		if id == adapter {
			scale, found = 1.0, true
		}
		out[i] = map[string]any{"id": i, "scale": scale}
	}
	if !found {
		return nil, fmt.Errorf("adapter %s is not loaded on %s", adapter, endpoint)
	}
	return out, nil
}

func adapterIDs(list []pluginapi.Adapter) []string {
	if len(list) == 0 {
		return nil
	}
	out := make([]string, len(list))
	for i, a := range list {
		out[i] = a.ID
	}
	return out
}

func sameAdapters(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
