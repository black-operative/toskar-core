package hardware

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// drmGPU is a graphics card the Linux kernel lists under /sys/class/drm.
type drmGPU struct {
	// Slot is its PCI address, such as "0000:03:00.0".
	Slot string
	// Vendor and Device are its PCI ids, such as "1002" and "744c".
	Vendor, Device string
	// VRAM is its dedicated memory in bytes, where the driver reports it
	// (amdgpu does); 0 when unknown.
	VRAM uint64
}

var drmCard = regexp.MustCompile(`^card[0-9]+$`)

// drmGPUs reads the graphics cards under root/class/drm (root is /sys)
// that have a render node in dev (/dev/dri), the device a runtime opens.
// It needs no tools, so it works where lspci isn't installed, such as in a
// container given the card with --device /dev/dri. A container without the
// card still sees it in /sys but not in /dev, so it isn't counted there.
func drmGPUs(root, dev string) []drmGPU {
	entries, err := os.ReadDir(filepath.Join(root, "class", "drm"))
	if err != nil {
		return nil
	}
	var out []drmGPU
	seen := map[string]bool{}
	for _, e := range entries {
		if !drmCard.MatchString(e.Name()) {
			continue
		}
		device := filepath.Join(root, "class", "drm", e.Name(), "device")
		if !hasRenderNode(device, dev) {
			continue
		}
		read := func(name string) string {
			b, _ := os.ReadFile(filepath.Join(device, name))
			return strings.TrimPrefix(strings.TrimSpace(string(b)), "0x")
		}
		g := drmGPU{Vendor: read("vendor"), Device: read("device")}
		if g.Vendor == "" {
			continue
		}
		if target, err := filepath.EvalSymlinks(device); err == nil {
			g.Slot = filepath.Base(target)
		}
		if seen[g.Slot] && g.Slot != "" {
			continue
		}
		seen[g.Slot] = true
		g.VRAM, _ = strconv.ParseUint(read("mem_info_vram_total"), 10, 64)
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slot < out[j].Slot })
	return out
}

// hasRenderNode reports whether the card at device has a render node, such
// as renderD128, present in dev.
func hasRenderNode(device, dev string) bool {
	nodes, _ := filepath.Glob(filepath.Join(device, "drm", "renderD*"))
	for _, n := range nodes {
		if _, err := os.Stat(filepath.Join(dev, filepath.Base(n))); err == nil {
			return true
		}
	}
	return false
}

// shortSlot is a PCI address as lspci prints it: "03:00.0" for
// "0000:03:00.0".
func shortSlot(slot string) string {
	return strings.TrimPrefix(slot, "0000:")
}

// drmAccelerators turns AMD and Intel cards from drmGPUs into accelerators,
// named by their lspci line when lspci gave one (keyed by short slot), with
// AMD's dedicated memory. NVIDIA cards come from nvidia-smi instead. Intel
// graphics count only when nothing else does, as with lspci.
func drmAccelerators(cards []drmGPU, names map[string]string, rocm bool, others int) []contracts.Accelerator {
	var amd, intel []contracts.Accelerator
	for _, g := range cards {
		name := names[shortSlot(g.Slot)]
		switch g.Vendor {
		case "1002":
			if name == "" {
				name = "AMD GPU (" + g.Vendor + ":" + g.Device + ")"
			}
			backends := []string{"vulkan", "cpu"}
			if rocm {
				backends = append([]string{"rocm"}, backends...)
			}
			amd = append(amd, contracts.Accelerator{
				ID: "amd" + strconv.Itoa(len(amd)), Vendor: "AMD", Model: name, Kind: "gpu",
				DedicatedVRAM: g.VRAM, Backends: backends,
			})
		case "8086":
			if name == "" {
				name = "Intel GPU (" + g.Vendor + ":" + g.Device + ")"
			}
			intel = append(intel, contracts.Accelerator{
				ID: "intel" + strconv.Itoa(len(intel)), Vendor: "Intel", Model: name, Kind: "gpu",
				Backends: []string{"vulkan", "cpu"},
			})
		}
	}
	if others+len(amd) == 0 && len(intel) > 0 {
		return intel[:1]
	}
	return amd
}
