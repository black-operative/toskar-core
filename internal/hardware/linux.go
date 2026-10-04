//go:build linux

package hardware

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

func platformCPU(ctx context.Context) (contracts.CPUInfo, error) {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return contracts.CPUInfo{}, err
	}
	defer f.Close()
	model := "Unknown CPU"
	cores := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "model name") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				model = strings.TrimSpace(parts[1])
			}
		}
		if strings.HasPrefix(line, "processor") {
			cores++
		}
	}
	return contracts.CPUInfo{Model: model, Cores: cores, Threads: cores}, nil
}

func platformMemory(ctx context.Context) (contracts.MemoryInfo, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return contracts.MemoryInfo{}, err
	}
	defer f.Close()
	var total, avail, swapTotal, swapFree uint64
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		val, _ := strconv.ParseUint(fields[1], 10, 64)
		val *= 1024
		switch fields[0] {
		case "MemTotal:":
			total = val
		case "MemAvailable:":
			avail = val
		case "SwapTotal:":
			swapTotal = val
		case "SwapFree:":
			swapFree = val
		}
	}
	info := contracts.MemoryInfo{TotalBytes: total, AvailableBytes: avail, SwapTotalBytes: swapTotal}
	if swapTotal >= swapFree {
		info.SwapUsedBytes = swapTotal - swapFree
	}
	return info, nil
}

func platformDisk(ctx context.Context, path string) (contracts.DiskInfo, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return contracts.DiskInfo{Path: path}, err
	}
	total := st.Blocks * uint64(st.Bsize)
	avail := st.Bavail * uint64(st.Bsize)
	return contracts.DiskInfo{Path: path, TotalBytes: total, AvailableBytes: avail}, nil
}

func platformAccelerators(ctx context.Context) ([]contracts.Accelerator, error) {
	var accels []contracts.Accelerator

	// NVIDIA via nvidia-smi when present.
	if out, err := exec.CommandContext(ctx, "nvidia-smi", "--query-gpu=name,memory.total", "--format=csv,noheader,nounits").Output(); err == nil {
		for i, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			parts := strings.Split(line, ",")
			name := strings.TrimSpace(parts[0])
			var vram uint64
			if len(parts) > 1 {
				mb, _ := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 64)
				vram = mb * 1024 * 1024
			}
			backends := []string{"cuda", "vulkan", "cpu"}
			accels = append(accels, contracts.Accelerator{
				ID:            "nvidia" + strconv.Itoa(i),
				Vendor:        "NVIDIA",
				Model:         name,
				Kind:          "gpu",
				DedicatedVRAM: vram,
				Backends:      backends,
			})
		}
	}

	// AMD via lspci heuristic.
	if out, err := exec.CommandContext(ctx, "lspci").Output(); err == nil {
		for i, line := range strings.Split(string(out), "\n") {
			lower := strings.ToLower(line)
			if !strings.Contains(lower, "vga") && !strings.Contains(lower, "3d") && !strings.Contains(lower, "display") {
				continue
			}
			if strings.Contains(lower, "amd") || strings.Contains(lower, "ati") || strings.Contains(lower, "radeon") {
				backends := []string{"vulkan", "cpu"}
				if _, err := exec.LookPath("rocminfo"); err == nil {
					backends = append([]string{"rocm"}, backends...)
				}
				accels = append(accels, contracts.Accelerator{
					ID:       "amd" + strconv.Itoa(i),
					Vendor:   "AMD",
					Model:    strings.TrimSpace(line),
					Kind:     "gpu",
					Backends: backends,
				})
			} else if strings.Contains(lower, "intel") && len(accels) == 0 {
				accels = append(accels, contracts.Accelerator{
					ID:       "intel" + strconv.Itoa(i),
					Vendor:   "Intel",
					Model:    strings.TrimSpace(line),
					Kind:     "gpu",
					Backends: []string{"vulkan", "cpu"},
				})
			}
		}
	}

	if len(accels) == 0 {
		accels = append(accels, contracts.Accelerator{
			ID:       "cpu0",
			Vendor:   "Generic",
			Model:    "CPU inference",
			Kind:     "cpu",
			Backends: []string{"cpu"},
		})
	}
	return accels, nil
}
