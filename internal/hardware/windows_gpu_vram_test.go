//go:build windows

package hardware

import (
	"context"
	"testing"
)

// Smoke test against the real machine: always yields at least one accelerator.
// The decision logic is covered by gpu_merge_test.go, which runs on every OS.
func TestPlatformAcceleratorsSmoke(t *testing.T) {
	accels, err := platformAccelerators(context.Background())
	if err != nil {
		t.Fatalf("platformAccelerators: %v", err)
	}
	if len(accels) == 0 {
		t.Fatal("expected at least one accelerator (cpu fallback)")
	}
	for _, a := range accels {
		if len(a.Backends) == 0 {
			t.Errorf("accelerator %s has no backends", a.ID)
		}
		t.Logf("%s %s %q vram=%d", a.ID, a.Vendor, a.Model, a.DedicatedVRAM)
	}
}
