package orchestrator

import (
	"fmt"

	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// Registry holds orchestrator implementations.
type Registry struct {
	items map[string]pluginapi.Orchestrator
	order []string
}

func NewRegistry() *Registry {
	return &Registry{items: make(map[string]pluginapi.Orchestrator)}
}

func (r *Registry) Register(o pluginapi.Orchestrator) {
	id := o.ID()
	if _, ok := r.items[id]; !ok {
		r.order = append(r.order, id)
	}
	r.items[id] = o
}

func (r *Registry) Get(id string) (pluginapi.Orchestrator, error) {
	o, ok := r.items[id]
	if !ok {
		return nil, fmt.Errorf("orchestrator %q not found", id)
	}
	return o, nil
}

func (r *Registry) List() []pluginapi.Orchestrator {
	out := make([]pluginapi.Orchestrator, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, r.items[id])
	}
	return out
}
