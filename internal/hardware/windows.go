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
	"golang.org/x/sys/windows/registry"
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

// Stores result across registry check as well as powershell command fallback
type gpuRow struct {
	name string
	vram uint64
}

// adapterRAMFallback reads Win32_VideoController. AdapterRAM is 32-bit, so it caps at 4 GB;
func adapterRAMFallback(ctx context.Context) []gpuRow {
	out, err := powershell(ctx, `Get-CimInstance Win32_VideoController | ForEach-Object { $_.Name + '|' + $_.AdapterRAM }`)
	if err != nil {
		return nil
	}

	var rows []gpuRow
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Name and VRAM
		parts := strings.SplitN(line, "|", 2)
		gpuName := strings.TrimSpace(parts[0])
		var gpuVram uint64
		if len(parts) > 1 {
			gpuVram, _ = strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 64)
		}
		rows = append(rows, gpuRow{name: gpuName, vram: gpuVram})
	}
	return rows
}

func platformAccelerators(ctx context.Context) ([]contracts.Accelerator, error) {
	var gpus []gpuRow

	// AdapterRAM results, loaded at most once and only if an adapter needs them
	var fallbackRows []gpuRow
	fallbackLoaded := false

	// Registry check
	if registryRoot, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Control\Class\{4d36e968-e325-11ce-bfc1-08002be10318}`,
		registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE,
	); err == nil {
		defer registryRoot.Close()
		subKeys, _ := registryRoot.ReadSubKeyNames(-1)

		for _, subKey := range subKeys {
			// filter numbers like 0000, 0001; skip "Configuration", "Properties" etc.
			if len(subKey) != 4 || strings.Trim(subKey, "0123456789") != "" {
				continue
			}

			currentKey, err := registry.OpenKey(registryRoot, subKey, registry.QUERY_VALUE)
			if err != nil {
				continue
			}

			// Name and VRAM
			gpuName, _, _ := currentKey.GetStringValue("DriverDesc")

			var size uint64
			if v, _, err := currentKey.GetIntegerValue("HardwareInformation.qwMemorySize"); err == nil {
				size = v
			} else if b, _, err := currentKey.GetBinaryValue("HardwareInformation.qwMemorySize"); err == nil && len(b) >= 8 {
				// Convert i'th byte to int and left shift for next byte
				for i := 7; i > -1; i-- {
					size = size<<8 | uint64(b[i])
				}
			}
			currentKey.Close()

			// Registry has no size for this adapter, try AdapterRAM by name
			if size == 0 {
				if !fallbackLoaded {
					fallbackRows = adapterRAMFallback(ctx)
					fallbackLoaded = true
				}
				for _, row := range fallbackRows {
					if strings.EqualFold(row.name, gpuName) {
						size = row.vram
						break
					}
				}
			}

			// Size still 0 implies Basic and Virtual display adapters, which have no VRAM value
			if size == 0 {
				continue
			}

			gpus = append(
				gpus,
				gpuRow{
					name: strings.TrimSpace(gpuName),
					vram: size,
				},
			)
		}
	}

	// Complete Fallback in-case registry check fails all
	if len(gpus) == 0 {
		if !fallbackLoaded {
			fallbackRows = adapterRAMFallback(ctx)
		}
		gpus = fallbackRows
	}

	var accels []contracts.Accelerator
	for i, gpu := range gpus {
		vendor := "Unknown"
		lower := strings.ToLower(gpu.name)
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
			Model:         gpu.name,
			Kind:          "gpu",
			DedicatedVRAM: gpu.vram,
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
