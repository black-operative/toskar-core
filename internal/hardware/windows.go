//go:build windows

package hardware

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"unsafe"

	"github.com/yeixio/toskar-core/pkg/contracts"
	"golang.org/x/sys/windows"
)

func platformCPU(ctx context.Context) (contracts.CPUInfo, error) {
	out, err := powershell(ctx, `(Get-CimInstance Win32_Processor | Select-Object -First 1).Name`)
	model := strings.TrimSpace(out)
	if err != nil || model == "" {
		model = "Windows CPU"
	}
	coresOut, _ := powershell(ctx, `(Get-CimInstance Win32_Processor | Measure-Object -Property NumberOfCores -Sum).Sum`)
	threadsOut, _ := powershell(ctx, `(Get-CimInstance Win32_Processor | Measure-Object -Property NumberOfLogicalProcessors -Sum).Sum`)
	cores, _ := strconv.Atoi(strings.TrimSpace(coresOut))
	threads, _ := strconv.Atoi(strings.TrimSpace(threadsOut))
	return contracts.CPUInfo{Model: model, Cores: cores, Threads: threads}, nil
}

func platformMemory(ctx context.Context) (contracts.MemoryInfo, error) {
	out, err := powershell(ctx, `(Get-CimInstance Win32_ComputerSystem).TotalPhysicalMemory`)
	if err != nil {
		return contracts.MemoryInfo{}, err
	}
	total, err := strconv.ParseUint(strings.TrimSpace(out), 10, 64)
	if err != nil {
		return contracts.MemoryInfo{}, err
	}
	info := contracts.MemoryInfo{TotalBytes: total, AvailableBytes: total}
	freeOut, err := powershell(ctx, `(Get-CimInstance Win32_OperatingSystem).FreePhysicalMemory`)
	if err == nil {
		freeKB, convErr := strconv.ParseUint(strings.TrimSpace(freeOut), 10, 64)
		if convErr == nil && freeKB > 0 {
			free := freeKB * 1024
			if free > total {
				free = total
			}
			info.AvailableBytes = free
		}
	}
	return info, nil
}

func platformDisk(ctx context.Context, path string) (contracts.DiskInfo, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return contracts.DiskInfo{Path: path}, err
	}
	var freeBytesAvailable, totalBytes, totalFreeBytes uint64
	err = windows.GetDiskFreeSpaceEx(pathPtr, &freeBytesAvailable, &totalBytes, &totalFreeBytes)
	if err != nil {
		return contracts.DiskInfo{Path: path}, err
	}
	_ = unsafe.Sizeof(totalFreeBytes)
	return contracts.DiskInfo{Path: path, TotalBytes: totalBytes, AvailableBytes: freeBytesAvailable}, nil
}

func platformAccelerators(ctx context.Context) ([]contracts.Accelerator, error) {
	out, err := powershell(ctx, `Get-CimInstance Win32_VideoController | ForEach-Object { $_.Name + '|' + $_.AdapterRAM }`)
	if err != nil {
		return []contracts.Accelerator{{
			ID: "cpu0", Vendor: "Generic", Model: "CPU inference", Kind: "cpu", Backends: []string{"cpu"},
		}}, nil
	}
	var accels []contracts.Accelerator
	for i, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		name := strings.TrimSpace(parts[0])
		var vram uint64
		if len(parts) > 1 {
			vram, _ = strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 64)
		}
		vendor := "Unknown"
		lower := strings.ToLower(name)
		backends := []string{"vulkan", "cpu"}
		switch {
		case strings.Contains(lower, "nvidia"):
			vendor = "NVIDIA"
			backends = []string{"cuda", "vulkan", "cpu"}
		case strings.Contains(lower, "amd") || strings.Contains(lower, "radeon"):
			vendor = "AMD"
		case strings.Contains(lower, "intel"):
			vendor = "Intel"
		}
		accels = append(accels, contracts.Accelerator{
			ID:            "gpu" + strconv.Itoa(i),
			Vendor:        vendor,
			Model:         name,
			Kind:          "gpu",
			DedicatedVRAM: vram,
			Backends:      backends,
		})
	}
	if len(accels) == 0 {
		accels = append(accels, contracts.Accelerator{
			ID: "cpu0", Vendor: "Generic", Model: "CPU inference", Kind: "cpu", Backends: []string{"cpu"},
		})
	}
	return accels, nil
}

func powershell(ctx context.Context, cmd string) (string, error) {
	out, err := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", cmd).Output()
	return string(out), err
}
