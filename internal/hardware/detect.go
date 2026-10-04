package hardware

import (
	"context"
	"os"
	"runtime"
	"time"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Detector gathers host hardware inventory.
type Detector struct {
	DataPath string
}

// Detect returns the best-effort hardware inventory for this host.
// Partial failures degrade fields rather than failing the whole call.
func (d *Detector) Detect(ctx context.Context) (contracts.HardwareInventory, error) {
	inv := contracts.HardwareInventory{
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		DetectedAt:   time.Now().UTC(),
		Accelerators: []contracts.Accelerator{},
	}
	if h, err := os.Hostname(); err == nil {
		inv.Hostname = h
	}

	cpu, err := detectCPU(ctx)
	if err == nil {
		inv.CPU = cpu
	} else if inv.CPU.Cores == 0 {
		inv.CPU = contracts.CPUInfo{Model: "Unknown CPU", Cores: runtime.NumCPU(), Threads: runtime.NumCPU()}
	}

	mem, err := detectMemory(ctx)
	if err == nil {
		inv.Memory = mem
	}

	path := d.DataPath
	if path == "" {
		path = "."
	}
	disk, err := detectDisk(ctx, path)
	if err == nil {
		inv.Disk = disk
	} else {
		inv.Disk = contracts.DiskInfo{Path: path}
	}

	accels, err := detectAccelerators(ctx)
	if err == nil && len(accels) > 0 {
		inv.Accelerators = accels
	}

	return inv, nil
}
