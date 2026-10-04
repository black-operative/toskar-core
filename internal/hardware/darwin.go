//go:build darwin

package hardware

import (
	"context"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"github.com/yeixio/toskar-core/pkg/contracts"
	"golang.org/x/sys/unix"
)

func platformCPU(ctx context.Context) (contracts.CPUInfo, error) {
	brand, _ := sysctl(ctx, "machdep.cpu.brand_string")
	coresStr, _ := sysctl(ctx, "hw.physicalcpu")
	threadsStr, _ := sysctl(ctx, "hw.logicalcpu")
	cores, _ := strconv.Atoi(strings.TrimSpace(coresStr))
	threads, _ := strconv.Atoi(strings.TrimSpace(threadsStr))
	model := strings.TrimSpace(brand)
	if model == "" {
		model = "Apple Silicon / Intel CPU"
	}
	return contracts.CPUInfo{Model: model, Cores: cores, Threads: threads}, nil
}

func platformMemory(ctx context.Context) (contracts.MemoryInfo, error) {
	total, err := unix.SysctlUint64("hw.memsize")
	if err != nil {
		memStr, err2 := sysctl(ctx, "hw.memsize")
		if err2 != nil {
			return contracts.MemoryInfo{}, err
		}
		total, err = strconv.ParseUint(strings.TrimSpace(memStr), 10, 64)
		if err != nil {
			return contracts.MemoryInfo{}, err
		}
	}
	info := contracts.MemoryInfo{TotalBytes: total, AvailableBytes: total}
	if avail, ok := darwinAvailableMemory(ctx); ok && avail > 0 {
		if avail > total {
			avail = total
		}
		info.AvailableBytes = avail
	}
	if swapTotal, swapUsed, ok := darwinSwap(ctx); ok {
		info.SwapTotalBytes = swapTotal
		info.SwapUsedBytes = swapUsed
	}
	return info, nil
}

func darwinAvailableMemory(ctx context.Context) (uint64, bool) {
	out, err := exec.CommandContext(ctx, "vm_stat").Output()
	if err != nil {
		return 0, false
	}
	return parseVMStatAvailable(string(out))
}

func darwinSwap(ctx context.Context) (total, used uint64, ok bool) {
	out, err := sysctl(ctx, "vm.swapusage")
	if err != nil {
		return 0, 0, false
	}
	return parseSwapUsage(out)
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
	// Prefer system_profiler for GPU name; fall back to Apple Silicon unified memory.
	out, err := exec.CommandContext(ctx, "system_profiler", "SPDisplaysDataType", "-detailLevel", "mini").Output()
	model := "Apple GPU"
	if err == nil {
		re := regexp.MustCompile(`(?m)^\s*Chipset Model:\s*(.+)$`)
		if m := re.FindSubmatch(out); len(m) == 2 {
			model = strings.TrimSpace(string(m[1]))
		}
	}

	mem, _ := platformMemory(ctx)
	accel := contracts.Accelerator{
		ID:            "gpu0",
		Vendor:        "Apple",
		Model:         model,
		Kind:          "gpu",
		UnifiedMemory: mem.TotalBytes,
		Backends:      []string{"metal", "cpu"},
	}

	// Detect Apple Silicon via arch / brand.
	brand, _ := sysctl(ctx, "machdep.cpu.brand_string")
	if strings.Contains(strings.ToLower(brand), "apple") || isArm64() {
		accel.Vendor = "Apple"
		if !strings.Contains(strings.ToLower(model), "apple") && model == "Apple GPU" {
			accel.Model = "Apple Silicon GPU"
		}
	}
	return []contracts.Accelerator{accel}, nil
}

func sysctl(ctx context.Context, name string) (string, error) {
	out, err := exec.CommandContext(ctx, "sysctl", "-n", name).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func isArm64() bool {
	out, err := exec.Command("uname", "-m").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "arm64"
}
