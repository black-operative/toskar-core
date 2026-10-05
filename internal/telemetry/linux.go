//go:build linux

package telemetry

import (
	"context"
	"os"
	"strings"
	"sync"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

type linuxReader struct {
	mu   sync.Mutex
	prev cpuTimes
	have bool
	// names are lspci's names for the cards, by PCI slot, read once.
	names     map[string]string
	namesOnce sync.Once
}

// NewReader reads /proc for CPU and memory, the kernel's card files for
// AMD and Intel GPUs, and nvidia-smi for NVIDIA.
func NewReader() Reader { return &linuxReader{} }

func (r *linuxReader) Read(ctx context.Context) contracts.LiveSample {
	var s contracts.LiveSample
	if raw, err := os.ReadFile("/proc/stat"); err == nil {
		if cur, ok := parseProcStat(string(raw)); ok {
			r.mu.Lock()
			if r.have {
				if p, ok := cpuPercent(r.prev, cur); ok {
					s.CPUPercent = f64(p)
				}
			}
			r.prev, r.have = cur, true
			r.mu.Unlock()
		}
	}
	if raw, err := os.ReadFile("/proc/meminfo"); err == nil {
		if used, total, ok := parseMeminfo(string(raw)); ok {
			s.MemoryUsedBytes, s.MemoryTotalBytes = u64(used), u64(total)
		}
	}
	r.namesOnce.Do(func() { r.names = lspciNames(ctx) })
	s.GPUs = append(nvidiaGPUs(ctx), sysfsGPUs("/sys", r.names)...)
	return s
}

// lspciNames are the display controllers lspci names, by full PCI slot
// ("0000:03:00.0"); empty without lspci.
func lspciNames(ctx context.Context) map[string]string {
	out := map[string]string{}
	text, err := run(ctx, "lspci", "-D")
	if err != nil {
		return out
	}
	for _, line := range strings.Split(text, "\n") {
		slot, rest, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		lower := strings.ToLower(rest)
		if !strings.Contains(lower, "vga") && !strings.Contains(lower, "3d") && !strings.Contains(lower, "display") {
			continue
		}
		// "VGA compatible controller: Advanced Micro Devices, Inc. [AMD/ATI]
		// Navi 31 [Radeon RX 7900 XT/7900 XTX] (rev c8)": the part after the
		// class, without the revision.
		if _, name, ok := strings.Cut(rest, ": "); ok {
			if i := strings.LastIndex(name, " (rev "); i > 0 {
				name = name[:i]
			}
			out[slot] = name
		}
	}
	return out
}
