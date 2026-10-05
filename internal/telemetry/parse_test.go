package telemetry

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func near(a, b float64) bool { return math.Abs(a-b) < 0.01 }

func TestProcStatAndMeminfo(t *testing.T) {
	a, ok := parseProcStat("cpu  100 0 100 700 100 0 0 0 0 0\ncpu0 1 2 3 4\n")
	if !ok || a.busy != 200 || a.total != 1000 {
		t.Fatalf("first: %+v %v", a, ok)
	}
	b, _ := parseProcStat("cpu  200 0 200 1300 100 0 0 0 0 0\n")
	if p, ok := cpuPercent(a, b); !ok || !near(p, 25) {
		t.Errorf("cpu = %v %v, want 25", p, ok)
	}
	if _, ok := cpuPercent(b, a); ok {
		t.Error("a reading going backwards should give nothing")
	}
	used, total, ok := parseMeminfo("MemTotal:       65536000 kB\nMemFree:  1000 kB\nMemAvailable:   49152000 kB\n")
	if !ok || total != 65536000*1024 || used != 16384000*1024 {
		t.Errorf("meminfo: %d %d %v", used, total, ok)
	}
	if _, _, ok := parseMeminfo("MemTotal: 1 kB\n"); ok {
		t.Error("meminfo without MemAvailable should give nothing")
	}
}

func TestNvidiaSMI(t *testing.T) {
	got := parseNvidiaSMI("NVIDIA GeForce RTX 4090, 87, 9216, 24564, 66, 312.45\nNVIDIA T400, [N/A], 100, 2048, 40, [Not Supported]\n")
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	g := got[0]
	if g.Name != "NVIDIA GeForce RTX 4090" || !near(*g.BusyPercent, 87) || *g.MemoryUsedBytes != 9216<<20 || *g.MemoryTotalBytes != 24564<<20 || !near(*g.TemperatureC, 66) || !near(*g.PowerWatts, 312.45) {
		t.Errorf("4090: %+v", g)
	}
	if got[1].BusyPercent != nil || got[1].PowerWatts != nil || got[1].MemoryUsedBytes == nil {
		t.Errorf("a figure the card doesn't give should be left out: %+v", got[1])
	}
}

func TestIoregAndTop(t *testing.T) {
	ioreg := `+-o AGXAcceleratorG14X  <class AGXAcceleratorG14X, id 0x100000460, registered, matched, active, busy 0 (2 ms), retain 51>
    {
      "model" = "Apple M2 Pro"
      "PerformanceStatistics" = {"In use system memory (driver)"=0,"Alloc system memory"=6727696384,"Tiled Scene Bytes"=1835008,"Renderer Utilization %"=31,"Device Utilization %"=34,"In use system memory"=4810719232}
    }
`
	got := parseIoreg(ioreg)
	if len(got) != 1 || got[0].Name != "Apple M2 Pro" || !near(*got[0].BusyPercent, 34) || *got[0].MemoryUsedBytes != 4810719232 {
		t.Fatalf("ioreg: %+v", got)
	}
	if got[0].TemperatureC != nil || got[0].PowerWatts != nil {
		t.Error("macOS gives no temperature or power without root")
	}
	top := "Processes: 1\nCPU usage: 3.1% user, 2.2% sys, 94.7% idle\nProcesses: 1\nCPU usage: 20.5% user, 9.5% sys, 70.0% idle\n"
	if p, ok := parseTopCPU(top); !ok || !near(p, 30) {
		t.Errorf("top = %v %v, want 30 (the second reading)", p, ok)
	}
	vm := "Mach Virtual Memory Statistics: (page size of 16384 bytes)\nPages free:  1000.\nPages active:  100000.\nPages inactive:  5000.\nPages wired down:  50000.\nPages occupied by compressor:  10000.\n"
	if used, ok := parseVMStatUsed(vm); !ok || used != 160000*16384 {
		t.Errorf("vm_stat used = %d %v", used, ok)
	}
}

func TestTypeperf(t *testing.T) {
	out := `
"(PDH-CSV 4.0)","\\PC\Processor(_Total)\% Processor Time","\\PC\GPU Engine(pid_1234_luid_0x00000000_0x0000D1A2_phys_0_eng_0_engtype_3D)\Utilization Percentage","\\PC\GPU Engine(pid_99_luid_0x00000000_0x0000D1A2_phys_0_eng_0_engtype_3D)\Utilization Percentage","\\PC\GPU Engine(pid_99_luid_0x00000000_0x0000D1A2_phys_0_eng_1_engtype_Copy)\Utilization Percentage","\\PC\GPU Adapter Memory(luid_0x00000000_0x0000D1A2_phys_0)\Dedicated Usage"
"10/05/2026 20:00:00.000","12.5","40.0","25.5","90.0","4294967296"
`
	values := parseTypeperf(out)
	if !near(values[`\\PC\Processor(_Total)\% Processor Time`], 12.5) {
		t.Fatalf("values: %v", values)
	}
	gpus := windowsGPUs(values, map[string]string{"0x00000000_0x0000D1A2": "AMD Radeon RX 7900 XTX"})
	if len(gpus) != 1 || gpus[0].Name != "AMD Radeon RX 7900 XTX" || !near(*gpus[0].BusyPercent, 65.5) || *gpus[0].MemoryUsedBytes != 4294967296 {
		t.Fatalf("gpus: %+v", gpus)
	}
}

// A fake /sys with an AMD card that gives everything, and one without
// hwmon.
func TestSysfsGPUs(t *testing.T) {
	root := t.TempDir()
	card := func(name, slot, vendor string, files map[string]string) {
		dev := filepath.Join(root, "devices", "pci0000:00", slot)
		for f, v := range files {
			p := filepath.Join(dev, f)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(v+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(dev, "vendor"), []byte(vendor+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(root, "class", "drm", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(dev, filepath.Join(dir, "device")); err != nil {
			t.Fatal(err)
		}
	}
	card("card0", "0000:03:00.0", "0x1002", map[string]string{
		"device": "0x744c", "gpu_busy_percent": "97", "mem_info_vram_used": "5368709120", "mem_info_vram_total": "25753026560",
		"hwmon/hwmon3/temp1_input": "61000", "hwmon/hwmon3/temp1_label": "edge",
		"hwmon/hwmon3/temp2_input": "78000", "hwmon/hwmon3/temp2_label": "junction",
		"hwmon/hwmon3/power1_average": "287000000",
	})
	card("card1", "0000:15:00.0", "0x1002", map[string]string{"device": "0x13c0", "gpu_busy_percent": "0", "mem_info_vram_used": "16777216", "mem_info_vram_total": "536870912"})
	card("card2", "0000:01:00.0", "0x10de", map[string]string{"device": "0x2684"})
	got := sysfsGPUs(root, map[string]string{"0000:03:00.0": "AMD Radeon RX 7900 XTX"})
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	g := got[0]
	if g.Name != "AMD Radeon RX 7900 XTX" || !near(*g.BusyPercent, 97) || *g.MemoryUsedBytes != 5368709120 || *g.MemoryTotalBytes != 25753026560 || !near(*g.TemperatureC, 61) || !near(*g.PowerWatts, 287) {
		t.Errorf("7900 XTX: %+v", g)
	}
	if got[1].Name != "AMD GPU (13c0)" || got[1].TemperatureC != nil || got[1].PowerWatts != nil || !near(*got[1].BusyPercent, 0) {
		t.Errorf("iGPU: %+v", got[1])
	}
}
