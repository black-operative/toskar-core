package hardware

import "strings"

// gpuRow is one display adapter: its name and dedicated memory in bytes.
type gpuRow struct {
	name string
	vram uint64
}

// qwordFromBinary reads a little-endian integer from a registry binary value.
// A 4-byte value (HardwareInformation.MemorySize) reads as a 32-bit integer.
func qwordFromBinary(b []byte) uint64 {
	if len(b) > 8 {
		b = b[:8]
	}
	var v uint64
	// Shift left to make room for the next byte, then OR in the current one.
	for i := len(b) - 1; i >= 0; i-- {
		v = v<<8 | uint64(b[i])
	}
	return v
}

// Used to compare adapterRAM and Registry because there's a possibility of difference for same hardware.
func gpuNameKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// mergeGPUs keeps the adapters Windows reports as present, in their order, and
// takes memory from the registry when it has a size there, and from AdapterRAM
// otherwise. Registry maps a name to the sizes of its class entries, in key
// order, so identical cards each get their own. Registry entries with no
// present adapter, such as a removed card's leftover driver key, are ignored.
func mergeGPUs(present []gpuRow, registry map[string][]uint64) []gpuRow {
	next := make(map[string]int, len(registry))
	merged := make([]gpuRow, 0, len(present))
	for _, gpu := range present {
		key := gpuNameKey(gpu.name)
		if sizes := registry[key]; next[key] < len(sizes) {
			if size := sizes[next[key]]; size > 0 {
				gpu.vram = size
			}
			next[key]++
		}
		merged = append(merged, gpu)
	}
	return merged
}
