//go:build windows

package telemetry

import (
	"context"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

type windowsReader struct{}

// NewReader reads Windows' built-in performance counters (typeperf) for
// CPU, and for each GPU's busy % and dedicated memory, which need no admin
// rights; memory from GlobalMemoryStatusEx; and nvidia-smi for NVIDIA
// cards, which also gives their temperature and power. Other cards'
// temperature and power need vendor tools, so they are left out.
func NewReader() Reader { return windowsReader{} }

var globalMemoryStatusEx = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")

type memoryStatusEx struct {
	length               uint32
	memoryLoad           uint32
	totalPhys            uint64
	availPhys            uint64
	totalPageFile        uint64
	availPageFile        uint64
	totalVirtual         uint64
	availVirtual         uint64
	availExtendedVirtual uint64
}

func (windowsReader) Read(ctx context.Context) contracts.LiveSample {
	var s contracts.LiveSample
	m := memoryStatusEx{length: uint32(unsafe.Sizeof(memoryStatusEx{}))}
	if ok, _, _ := globalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&m))); ok != 0 && m.availPhys <= m.totalPhys {
		s.MemoryUsedBytes, s.MemoryTotalBytes = u64(m.totalPhys-m.availPhys), u64(m.totalPhys)
	}
	out, err := run(ctx, "typeperf", "-sc", "1",
		`\Processor(_Total)\% Processor Time`,
		`\GPU Engine(*engtype_3D)\Utilization Percentage`,
		`\GPU Adapter Memory(*)\Dedicated Usage`)
	nvidia := nvidiaGPUs(ctx)
	if err == nil {
		values := parseTypeperf(out)
		for k, v := range values {
			if strings.HasSuffix(k, `\Processor(_Total)\% Processor Time`) {
				s.CPUPercent = f64(v)
			}
		}
		// The counters don't name adapters or say which is NVIDIA's, so with
		// nvidia-smi giving NVIDIA cards in full, they are left to it.
		if len(nvidia) == 0 {
			s.GPUs = windowsGPUs(values, nil)
		}
	}
	if len(nvidia) > 0 {
		s.GPUs = nvidia
	}
	return s
}
