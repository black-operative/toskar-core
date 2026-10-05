package app

import (
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

func TestAccelerationView(t *testing.T) {
	gpuRun := &pluginapi.Acceleration{Backend: "vulkan", Devices: []string{"RX 7900 XTX"}, LayersOffloaded: 29, LayersTotal: 29, GPUMemoryBytes: 1}
	partial := &pluginapi.Acceleration{Backend: "vulkan", LayersOffloaded: 40, LayersTotal: 65}
	metalNoCount := &pluginapi.Acceleration{Backend: "metal", Devices: []string{"Apple M2"}}
	cpu := &pluginapi.Acceleration{Backend: "cpu"}
	for _, c := range []struct {
		name          string
		acc           *pluginapi.Acceleration
		gpu, cpuBuild bool
		state, reason string
	}{
		{"all layers on the GPU", gpuRun, true, false, contracts.AccelerationGPU, ""},
		{"no layer count", metalNoCount, true, false, contracts.AccelerationGPU, ""},
		{"some layers", partial, true, false, contracts.AccelerationPartial, contracts.AccelerationReasonGPUMemory},
		{"no GPU here", cpu, false, false, contracts.AccelerationCPUExpected, contracts.AccelerationReasonNoGPU},
		{"CPU build beside a GPU", cpu, true, true, contracts.AccelerationCPU, contracts.AccelerationReasonCPUBuild},
		{"GPU build found no GPU", cpu, true, false, contracts.AccelerationCPU, contracts.AccelerationReasonGPUUnavailable},
	} {
		v := accelerationView(c.acc, c.gpu, c.cpuBuild)
		if v == nil || v.State != c.state || v.Reason != c.reason {
			t.Errorf("%s: got %+v, want %s/%s", c.name, v, c.state, c.reason)
		}
	}
	if accelerationView(nil, true, false) != nil {
		t.Error("an unknown report should stay unknown")
	}
}

func TestAccelerationSummary(t *testing.T) {
	view := func(state, mode string) contracts.RunningModelView {
		return contracts.RunningModelView{Mode: mode, Acceleration: &contracts.Acceleration{State: state}}
	}
	for _, c := range []struct {
		name  string
		views []contracts.RunningModelView
		want  string
	}{
		{"nothing loaded", nil, contracts.AccelerationIdle},
		{"on the GPU", []contracts.RunningModelView{view("gpu", "")}, "gpu"},
		{"the worst chat model", []contracts.RunningModelView{view("gpu", ""), view("partial", "")}, "partial"},
		{"chat models over supporting ones", []contracts.RunningModelView{view("gpu", ""), view("cpu", "embedding")}, "gpu"},
		{"only a supporting model", []contracts.RunningModelView{view("cpu_expected", "embedding")}, "cpu_expected"},
		{"unknown", []contracts.RunningModelView{{}}, ""},
	} {
		if got := accelerationSummary(c.views); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestAccelerationLabel(t *testing.T) {
	if b, d := accelerationLabel(nil); b != "" || d != "" {
		t.Errorf("unknown: %q %q", b, d)
	}
	if b, d := accelerationLabel(&pluginapi.Acceleration{Backend: "vulkan", Devices: []string{"A", "B"}}); b != "vulkan" || d != "A, B" {
		t.Errorf("two cards: %q %q", b, d)
	}
	if b, d := accelerationLabel(&pluginapi.Acceleration{Backend: "cpu"}); b != "cpu" || d != "" {
		t.Errorf("cpu: %q %q", b, d)
	}
}
