//go:build linux

package telemetry

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// The Linux reader gives CPU (from its second reading) and memory on a real
// computer, and its GPUs where it has them.
func TestLinuxReaderReadsThisComputer(t *testing.T) {
	if testing.Short() {
		t.Skip("reads this computer")
	}
	r := NewReader()
	r.Read(context.Background())
	time.Sleep(200 * time.Millisecond)
	s := r.Read(context.Background())
	raw, _ := json.Marshal(s)
	t.Logf("%s", raw)
	if s.CPUPercent == nil || s.MemoryUsedBytes == nil || s.MemoryTotalBytes == nil {
		t.Errorf("CPU or memory missing: %s", raw)
	}
}
