package llamacpp

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// mb is n MiB in bytes, as the parser counts each buffer.
func mb(n float64) uint64 { return uint64(n * (1 << 20)) }

func TestParseAcceleration(t *testing.T) {
	for _, c := range []struct {
		file string
		want *pluginapi.Acceleration
	}{
		{"vulkan-amd.log", &pluginapi.Acceleration{
			Backend: "vulkan", Devices: []string{"AMD Radeon RX 7900 XTX (RADV NAVI31)"},
			LayersOffloaded: 29, LayersTotal: 29,
			GPUMemoryBytes: mb(4168.09) + mb(448) + mb(304),
		}},
		{"cuda.log", &pluginapi.Acceleration{
			Backend: "cuda", Devices: []string{"NVIDIA GeForce RTX 4090"},
			LayersOffloaded: 49, LayersTotal: 49,
			GPUMemoryBytes: mb(8148.38) + mb(1536) + mb(696),
		}},
		{"rocm.log", &pluginapi.Acceleration{
			Backend: "rocm", Devices: []string{"AMD Radeon RX 7900 XTX"},
			LayersOffloaded: 29, LayersTotal: 29, GPUMemoryBytes: mb(4168.09),
		}},
		{"metal.log", &pluginapi.Acceleration{
			Backend: "metal", Devices: []string{"Apple M2 Pro"},
			LayersOffloaded: 29, LayersTotal: 29, GPUMemoryBytes: mb(4460.45) + mb(448),
		}},
		{"partial.log", &pluginapi.Acceleration{
			Backend: "vulkan", Devices: []string{"AMD Radeon RX 6600 (RADV NAVI23)"},
			LayersOffloaded: 40, LayersTotal: 65, GPUMemoryBytes: mb(7101.25),
		}},
		// llama.cpp b11433 (October 2026) on an Apple M5 Pro, at trace
		// verbosity: timestamps, and "(unknown id)" after the device.
		{"metal-b11433.log", &pluginapi.Acceleration{
			Backend: "metal", Devices: []string{"Apple M5 Pro"},
			LayersOffloaded: 17, LayersTotal: 17, GPUMemoryBytes: mb(762.81) + mb(4096) + mb(413.45),
		}},
		{"cpu.log", &pluginapi.Acceleration{Backend: "cpu"}},
		{"vulkan-no-offload.log", &pluginapi.Acceleration{Backend: "cpu", LayersTotal: 29}},
		{"before-load.log", nil},
	} {
		raw, err := os.ReadFile(filepath.Join("testdata", "offload", c.file))
		if err != nil {
			t.Fatal(err)
		}
		if got := parseAcceleration(string(raw)); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s:\n got  %+v\n want %+v", c.file, got, c.want)
		}
	}
}

func TestReadAcceleration(t *testing.T) {
	if readAcceleration(filepath.Join(t.TempDir(), "missing.log")) != nil {
		t.Error("a missing log has no acceleration")
	}
	if got := readAcceleration(filepath.Join("testdata", "offload", "metal.log")); got == nil || got.Backend != "metal" {
		t.Errorf("metal log: %+v", got)
	}
}
