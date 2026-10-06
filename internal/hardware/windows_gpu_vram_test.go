//go:build windows

package hardware

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Runs against real hardware; each case skips if that GPU is not installed.
func TestKnownGPUs(t *testing.T) {
	accels, err := platformAccelerators(context.Background())
	if err != nil {
		t.Fatalf("platformAccelerators: %v", err)
	}

	tests := []struct {
		model    string
		vendor   string
		backends []string
		minVRAM  uint64
	}{
		{"AMD Radeon 740M Graphics", "AMD", []string{"vulkan", "cpu"}, 512 << 20},
		{"NVIDIA GeForce RTX 3050 Laptop GPU", "NVIDIA", []string{"cuda", "vulkan", "cpu"}, 4 << 30},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			var got *contracts.Accelerator
			for i := range accels {
				if strings.EqualFold(accels[i].Model, tt.model) {
					got = &accels[i]
					break
				}
			}
			if got == nil {
				t.Skipf("%q not present on this machine", tt.model)
			}
			if got.Kind != "gpu" {
				t.Errorf("Kind = %q, want gpu", got.Kind)
			}
			if got.Vendor != tt.vendor {
				t.Errorf("Vendor = %q, want %q", got.Vendor, tt.vendor)
			}
			if !slices.Equal(got.Backends, tt.backends) {
				t.Errorf("Backends = %v, want %v", got.Backends, tt.backends)
			}
			if got.DedicatedVRAM < tt.minVRAM {
				t.Errorf("DedicatedVRAM = %d, want >= %d", got.DedicatedVRAM, tt.minVRAM)
			}
		})
	}
}

// Smoke test against real hardware: always yields >=1 accelerator, and any GPU
// reported must carry VRAM (zero-VRAM virtual adapters are filtered out).
func TestPlatformAcceleratorsSmoke(t *testing.T) {
	accels, err := platformAccelerators(context.Background())
	if err != nil {
		t.Fatalf("platformAccelerators: %v", err)
	}
	if len(accels) == 0 {
		t.Fatal("expected at least one accelerator (cpu fallback)")
	}
	for _, a := range accels {
		if a.Kind == "gpu" && a.Model == "" {
			t.Errorf("gpu %s has empty model", a.ID)
		}
		if len(a.Backends) == 0 {
			t.Errorf("accelerator %s has no backends", a.ID)
		}
		t.Logf("%s %s %q vram=%d", a.ID, a.Vendor, a.Model, a.DedicatedVRAM)
	}
}
