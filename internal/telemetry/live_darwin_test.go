//go:build darwin

package telemetry

import (
	"context"
	"encoding/json"
	"testing"
)

// The macOS reader gives CPU, memory, and the GPU's busy % on a real Mac.
func TestDarwinReaderReadsThisMac(t *testing.T) {
	if testing.Short() {
		t.Skip("reads this computer")
	}
	s := NewReader().Read(context.Background())
	raw, _ := json.Marshal(s)
	t.Logf("%s", raw)
	if s.CPUPercent == nil || s.MemoryUsedBytes == nil || s.MemoryTotalBytes == nil {
		t.Errorf("CPU or memory missing: %s", raw)
	}
	if len(s.GPUs) == 0 || s.GPUs[0].BusyPercent == nil {
		t.Errorf("no GPU busy %%: %s", raw)
	}
}
