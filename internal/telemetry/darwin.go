//go:build darwin

package telemetry

import (
	"context"
	"strconv"
	"strings"
	"sync"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

type darwinReader struct {
	totalOnce sync.Once
	total     uint64
}

// NewReader reads top and vm_stat for CPU and memory, and ioreg for the
// GPU (busy %, and on Apple silicon the shared memory it is using). macOS
// gives GPU temperature and power only to root (powermetrics), so they are
// left out.
func NewReader() Reader { return &darwinReader{} }

func (r *darwinReader) Read(ctx context.Context) contracts.LiveSample {
	var s contracts.LiveSample
	// Two samples a second apart: the first covers all time since boot.
	if out, err := run(ctx, "top", "-l", "2", "-n", "0", "-s", "1"); err == nil {
		if p, ok := parseTopCPU(out); ok {
			s.CPUPercent = f64(p)
		}
	}
	r.totalOnce.Do(func() {
		if out, err := run(ctx, "sysctl", "-n", "hw.memsize"); err == nil {
			r.total, _ = strconv.ParseUint(strings.TrimSpace(out), 10, 64)
		}
	})
	if out, err := run(ctx, "vm_stat"); err == nil {
		if used, ok := parseVMStatUsed(out); ok && r.total > 0 {
			s.MemoryUsedBytes, s.MemoryTotalBytes = u64(used), u64(r.total)
		}
	}
	if out, err := run(ctx, "ioreg", "-r", "-d", "1", "-w", "0", "-c", "IOAccelerator"); err == nil {
		s.GPUs = parseIoreg(out)
	}
	return s
}
