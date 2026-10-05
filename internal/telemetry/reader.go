package telemetry

import (
	"context"
	"os/exec"
	"time"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Reader takes one reading of this computer's figures.
type Reader interface {
	Read(ctx context.Context) contracts.LiveSample
}

// nvidiaQuery is what nvidia-smi is asked for, in parseNvidiaSMI's order.
const nvidiaQuery = "--query-gpu=name,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw"

// nvidiaGPUs reads NVIDIA cards with nvidia-smi, which their driver
// installs on Linux and Windows; nil without it.
func nvidiaGPUs(ctx context.Context) []contracts.GPUSample {
	exe, err := exec.LookPath("nvidia-smi")
	if err != nil {
		return nil
	}
	out, err := run(ctx, exe, nvidiaQuery, "--format=csv,noheader,nounits")
	if err != nil {
		return nil
	}
	return parseNvidiaSMI(out)
}

// run runs a command with a short deadline and returns its output.
func run(ctx context.Context, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	return string(out), err
}
