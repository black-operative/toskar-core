package telemetry

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

var drmCardRe = regexp.MustCompile(`^card[0-9]+$`)

// sysfsGPUs reads AMD and Intel cards from the Linux kernel under root
// (/sys): AMD's amdgpu gives busy %, video memory, temperature, and power;
// Intel's drivers give what their hwmon exposes. NVIDIA comes from
// nvidia-smi instead. names maps a PCI slot ("0000:03:00.0") to a name.
func sysfsGPUs(root string, names map[string]string) []contracts.GPUSample {
	entries, err := os.ReadDir(filepath.Join(root, "class", "drm"))
	if err != nil {
		return nil
	}
	type card struct {
		slot string
		g    contracts.GPUSample
	}
	var cards []card
	seen := map[string]bool{}
	for _, e := range entries {
		if !drmCardRe.MatchString(e.Name()) {
			continue
		}
		dev := filepath.Join(root, "class", "drm", e.Name(), "device")
		read := func(name string) (string, bool) {
			b, err := os.ReadFile(filepath.Join(dev, name))
			return strings.TrimSpace(string(b)), err == nil
		}
		vendor, _ := read("vendor")
		vendor = strings.TrimPrefix(vendor, "0x")
		if vendor != "1002" && vendor != "8086" {
			continue
		}
		slot := e.Name()
		if t, err := filepath.EvalSymlinks(dev); err == nil {
			slot = filepath.Base(t)
		}
		if seen[slot] {
			continue
		}
		seen[slot] = true
		name := names[slot]
		if name == "" {
			device, _ := read("device")
			if vendor == "1002" {
				name = "AMD GPU (" + strings.TrimPrefix(device, "0x") + ")"
			} else {
				name = "Intel GPU (" + strings.TrimPrefix(device, "0x") + ")"
			}
		}
		g := contracts.GPUSample{Name: name}
		if v, ok := read("gpu_busy_percent"); ok {
			if n, err := strconv.ParseFloat(v, 64); err == nil {
				g.BusyPercent = f64(n)
			}
		}
		if v, ok := read("mem_info_vram_used"); ok {
			if n, err := strconv.ParseUint(v, 10, 64); err == nil {
				g.MemoryUsedBytes = u64(n)
			}
		}
		if v, ok := read("mem_info_vram_total"); ok {
			if n, err := strconv.ParseUint(v, 10, 64); err == nil {
				g.MemoryTotalBytes = u64(n)
			}
		}
		g.TemperatureC, g.PowerWatts = hwmon(filepath.Join(dev, "hwmon"))
		if g.BusyPercent == nil && g.MemoryUsedBytes == nil && g.TemperatureC == nil && g.PowerWatts == nil {
			continue
		}
		cards = append(cards, card{slot, g})
	}
	sort.Slice(cards, func(i, j int) bool { return cards[i].slot < cards[j].slot })
	out := make([]contracts.GPUSample, 0, len(cards))
	for _, c := range cards {
		out = append(out, c.g)
	}
	return out
}

// hwmon reads a card's temperature (°C, from millidegrees: the "edge" or
// first sensor) and power draw (W, from microwatts: power1_average, or
// power1_input on newer kernels).
func hwmon(dir string) (*float64, *float64) {
	mons, _ := filepath.Glob(filepath.Join(dir, "hwmon*"))
	var temp, power *float64
	for _, m := range mons {
		readNum := func(name string) (float64, bool) {
			b, err := os.ReadFile(filepath.Join(m, name))
			if err != nil {
				return 0, false
			}
			n, err := strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
			return n, err == nil
		}
		if temp == nil {
			// The sensor labelled "edge", else the first.
			sensors, _ := filepath.Glob(filepath.Join(m, "temp*_input"))
			sort.Strings(sensors)
			for _, s := range sensors {
				label, _ := os.ReadFile(strings.TrimSuffix(s, "_input") + "_label")
				if strings.TrimSpace(string(label)) == "edge" || temp == nil {
					if v, ok := readNum(filepath.Base(s)); ok {
						temp = f64(v / 1000)
					}
				}
			}
		}
		if power == nil {
			for _, name := range []string{"power1_average", "power1_input"} {
				if v, ok := readNum(name); ok {
					power = f64(v / 1e6)
					break
				}
			}
		}
	}
	return temp, power
}
