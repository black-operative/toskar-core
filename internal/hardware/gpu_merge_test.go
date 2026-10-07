package hardware

import (
	"slices"
	"testing"
)

func TestQwordFromBinary(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want uint64
	}{
		{"empty", nil, 0},
		{"4 GiB", []byte{0, 0, 0, 0, 1, 0, 0, 0}, 4 << 30},
		{"6 GiB", []byte{0, 0, 0, 0x80, 1, 0, 0, 0}, 6 << 30},
		{"12 GiB", []byte{0, 0, 0, 0, 3, 0, 0, 0}, 12 << 30},
		{"low byte first", []byte{0x01, 0x02, 0, 0, 0, 0, 0, 0}, 0x0201},
		{"32-bit MemorySize", []byte{0x00, 0x00, 0x00, 0x40}, 1 << 30},
		{"extra bytes ignored", []byte{1, 0, 0, 0, 0, 0, 0, 0, 0xff}, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := qwordFromBinary(tt.in); got != tt.want {
				t.Errorf("qwordFromBinary(%v) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestMergeGPUs(t *testing.T) {
	const adapterCap = 4293918720 // AdapterRAM's 32-bit ceiling

	tests := []struct {
		name     string
		present  []gpuRow
		registry map[string][]uint64
		want     []gpuRow
	}{
		{
			name:     "card over 4 GiB gets its registry size",
			present:  []gpuRow{{"NVIDIA GeForce RTX 3050 Laptop GPU", adapterCap}},
			registry: map[string][]uint64{"nvidia geforce rtx 3050 laptop gpu": {6 << 30}},
			want:     []gpuRow{{"NVIDIA GeForce RTX 3050 Laptop GPU", 6 << 30}},
		},
		{
			name:     "two identical cards each get their own size, in order",
			present:  []gpuRow{{"GPU A", adapterCap}, {"GPU A", adapterCap}},
			registry: map[string][]uint64{"gpu a": {8 << 30, 12 << 30}},
			want:     []gpuRow{{"GPU A", 8 << 30}, {"GPU A", 12 << 30}},
		},
		{
			name:     "no registry size falls back to AdapterRAM",
			present:  []gpuRow{{"GPU A", 2 << 30}},
			registry: map[string][]uint64{},
			want:     []gpuRow{{"GPU A", 2 << 30}},
		},
		{
			name:     "registry entry with size 0 falls back to AdapterRAM",
			present:  []gpuRow{{"GPU A", 2 << 30}},
			registry: map[string][]uint64{"gpu a": {0}},
			want:     []gpuRow{{"GPU A", 2 << 30}},
		},
		{
			name:     "stale registry entry for a removed card is not reported",
			present:  []gpuRow{{"GPU A", 2 << 30}},
			registry: map[string][]uint64{"gpu a": {2 << 30}, "old 12 gb card": {12 << 30}},
			want:     []gpuRow{{"GPU A", 2 << 30}},
		},
		{
			name:     "present order is kept",
			present:  []gpuRow{{"Intel iGPU", 1 << 30}, {"GPU A", adapterCap}},
			registry: map[string][]uint64{"gpu a": {6 << 30}},
			want:     []gpuRow{{"Intel iGPU", 1 << 30}, {"GPU A", 6 << 30}},
		},
		{
			name:     "name match ignores case and spacing",
			present:  []gpuRow{{"  GPU A ", adapterCap}},
			registry: map[string][]uint64{"gpu a": {6 << 30}},
			want:     []gpuRow{{"  GPU A ", 6 << 30}},
		},
		{
			name:     "adapter with no memory anywhere is kept",
			present:  []gpuRow{{"Microsoft Basic Display Adapter", 0}},
			registry: map[string][]uint64{},
			want:     []gpuRow{{"Microsoft Basic Display Adapter", 0}},
		},
		{
			name:     "no adapters present",
			present:  nil,
			registry: map[string][]uint64{"gpu a": {6 << 30}},
			want:     []gpuRow{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeGPUs(tt.present, tt.registry)
			if !slices.Equal(got, tt.want) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
