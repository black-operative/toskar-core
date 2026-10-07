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

// adapterRAM lists all present video cards, value is capped at 32-bit (4 GB).
func adapterRAM(ctx context.Context) []gpuRow {
	out, err := powershell(
		ctx,
		`Get-CimInstance Win32_VideoController | ForEach-Object { $_.Name + '|' + $_.AdapterRAM }`,
	)
	if err != nil {
		return nil
	}
	var rows []gpuRow
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, ram, _ := strings.Cut(line, "|")
		vram, _ := strconv.ParseUint(strings.TrimSpace(ram), 10, 64)
		rows = append(rows, gpuRow{name: strings.TrimSpace(name), vram: vram})
	}
	return rows
}

// registryVRAM maps each display class entry's name to its memory sizes, in key
// order. The class key also holds entries for adapters that are gone, so it is only
// used to look up the size of an adapter that is present.
func registryVRAM() map[string][]uint64 {
	sizes := map[string][]uint64{}
	root, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Control\Class\{4d36e968-e325-11ce-bfc1-08002be10318}`,
		registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE,
	)
	if err != nil {
		return sizes
	}
	defer root.Close()
	subKeys, _ := root.ReadSubKeyNames(-1)

	for _, subKey := range subKeys {
		// filter numbers like 0000, 0001; skip "Configuration", "Properties" etc.
		if len(subKey) != 4 || strings.Trim(subKey, "0123456789") != "" {
			continue
		}
		key, err := registry.OpenKey(root, subKey, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		name, _, _ := key.GetStringValue("DriverDesc")
		size := registryMemory(key, "HardwareInformation.qwMemorySize")
		if size == 0 {
			// Some drivers only write the 32-bit value.
			size = registryMemory(key, "HardwareInformation.MemorySize")
		}
		key.Close()

		nameKey := gpuNameKey(name)
		sizes[nameKey] = append(sizes[nameKey], size)
	}
	return sizes
}

// registryMemory reads a size stored either as a number or as binary.
func registryMemory(key registry.Key, value string) uint64 {
	if v, _, err := key.GetIntegerValue(value); err == nil {
		return v
	}
	if b, _, err := key.GetBinaryValue(value); err == nil {
		return qwordFromBinary(b)
	}
	return 0
}

func platformAccelerators(ctx context.Context) ([]contracts.Accelerator, error) {
	// Win32_VideoController lists only adapters that are present, so a removed
	// card's leftover registry entry cannot show up. The registry only corrects sizes.
	gpus := mergeGPUs(adapterRAM(ctx), registryVRAM())

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
