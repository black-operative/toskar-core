package runtimes

import (
	"context"
	"testing"

	"github.com/yeixio/toskar-core/internal/runtimes/external"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

func TestRegistryKeepsRegistrationOrder(t *testing.T) {
	registry := NewRegistry()
	first := external.New(external.Config{})
	second := &staticRuntime{id: "static"}
	registry.Register(first)
	registry.Register(second)
	registry.Register(external.New(external.Config{BaseURL: "http://127.0.0.1:9"}))

	listed := registry.List()
	if len(listed) != 2 || listed[0].ID() != first.ID() || listed[1].ID() != "static" {
		t.Fatalf("list=%v", ids(listed))
	}
	got, err := registry.Get("static")
	if err != nil || got.ID() != "static" {
		t.Fatalf("get=%v err=%v", got, err)
	}
	if _, err := registry.Get("missing"); err == nil {
		t.Fatal("missing runtime was returned")
	}
}

type staticRuntime struct{ id string }

func (r *staticRuntime) ID() string          { return r.id }
func (r *staticRuntime) DisplayName() string { return r.id }
func (r *staticRuntime) Detect(context.Context) (pluginapi.RuntimeDetection, error) {
	return pluginapi.RuntimeDetection{}, nil
}
func (r *staticRuntime) Install(context.Context, pluginapi.InstallOptions) error { return nil }
func (r *staticRuntime) Update(context.Context) error                            { return nil }
func (r *staticRuntime) Capabilities(context.Context) (pluginapi.RuntimeCapabilities, error) {
	return pluginapi.RuntimeCapabilities{}, nil
}
func (r *staticRuntime) StartModel(context.Context, pluginapi.ModelStartConfig) (pluginapi.RunningModel, error) {
	return pluginapi.RunningModel{}, nil
}
func (r *staticRuntime) StopModel(context.Context, string) error { return nil }
func (r *staticRuntime) ListRunning(context.Context) ([]pluginapi.RunningModel, error) {
	return nil, nil
}
func (r *staticRuntime) Health(context.Context) error { return nil }

func ids(items []Runtime) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = item.ID()
	}
	return out
}
