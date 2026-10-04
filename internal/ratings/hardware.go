package ratings

import (
	"math"
	"regexp"
	"strings"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Hardware is a computer's class: never a name, serial, or address.
type Hardware struct {
	Platform     string `json:"platform"`
	Architecture string `json:"architecture"`
	// Vendor is apple, nvidia, amd, intel, or cpu (no accelerator).
	Vendor string `json:"vendor"`
	// Family is the accelerator model, such as m4-max or rtx-4090, or for
	// cpu, the CPU's maker.
	Family string `json:"family"`
	// MemoryType is unified, dedicated, or system.
	MemoryType string `json:"memory_type"`
	// MemoryBucket is the memory the model can use, in GB.
	MemoryBucket string `json:"memory_bucket_gb"`
}

// Backend is what llama.cpp runs on here: Metal on Apple silicon, and the
// CPU build Yggdrasil installs everywhere else.
func Backend(inv contracts.HardwareInventory) string {
	if inv.OS == "darwin" && inv.Arch == "arm64" {
		return "metal"
	}
	return "cpu"
}

// Normalize reduces a hardware inventory to its class, or false when the
// platform is not one ratings know.
func Normalize(inv contracts.HardwareInventory) (Hardware, bool) {
	h := Hardware{Platform: map[string]string{"darwin": "macos", "linux": "linux", "windows": "windows"}[inv.OS], Architecture: inv.Arch}
	if h.Platform == "" || (h.Architecture != "arm64" && h.Architecture != "amd64") {
		return Hardware{}, false
	}
	var best *contracts.Accelerator
	for i := range inv.Accelerators {
		a := &inv.Accelerators[i]
		if vendorOf(a.Vendor+" "+a.Model) == "" {
			continue
		}
		if best == nil || max(a.UnifiedMemory, a.DedicatedVRAM) > max(best.UnifiedMemory, best.DedicatedVRAM) {
			best = a
		}
	}
	switch {
	case best != nil && best.UnifiedMemory > 0:
		h.Vendor, h.MemoryType, h.MemoryBucket = vendorOf(best.Vendor+" "+best.Model), "unified", Bucket(best.UnifiedMemory)
	case best != nil && best.DedicatedVRAM > 0:
		h.Vendor, h.MemoryType, h.MemoryBucket = vendorOf(best.Vendor+" "+best.Model), "dedicated", Bucket(best.DedicatedVRAM)
	}
	if h.Vendor != "" {
		h.Family = familyOf(best.Model, h.Vendor)
		return h, true
	}
	h.Vendor, h.MemoryType, h.MemoryBucket = "cpu", "system", Bucket(inv.Memory.TotalBytes)
	h.Family = vendorOf(inv.CPU.Model)
	if h.Family == "" {
		h.Family = map[string]string{"arm64": "arm", "amd64": "x86"}[h.Architecture]
	}
	return h, true
}

// vendorOf names the maker in a device description, or "" for none ratings
// know (such as "Generic CPU inference").
func vendorOf(s string) string {
	s = strings.ToLower(s)
	switch {
	case strings.Contains(s, "apple"):
		return "apple"
	case strings.Contains(s, "nvidia"), strings.Contains(s, "geforce"):
		return "nvidia"
	case strings.Contains(s, "amd"), strings.Contains(s, "advanced micro devices"), strings.Contains(s, "radeon"), strings.Contains(s, "ryzen"):
		return "amd"
	case strings.Contains(s, "intel"):
		return "intel"
	}
	return ""
}

var (
	revRe   = regexp.MustCompile(`\(rev [^)]*\)`)
	wordsRe = regexp.MustCompile(`[a-z0-9]+`)
	// noise is words that say nothing about which accelerator it is.
	noise = map[string]bool{"apple": true, "nvidia": true, "geforce": true, "amd": true, "ati": true, "radeon": true, "intel": true,
		"r": true, "tm": true, "graphics": true, "corporation": true, "inc": true, "advanced": true, "micro": true, "devices": true, "gpu": true}
)

// familyOf is an accelerator's model as a slug: "Apple M4 Max" is m4-max,
// "NVIDIA GeForce RTX 4090" rtx-4090, and an lspci line's bracketed name
// "[Radeon RX 7900 XT/7900 XTX]" rx-7900-xt.
func familyOf(model, vendor string) string {
	s := strings.ToLower(revRe.ReplaceAllString(model, ""))
	if i := strings.LastIndex(s, "["); i >= 0 {
		if j := strings.Index(s[i:], "]"); j > 0 {
			s = s[i+1 : i+j]
		}
	}
	if i := strings.Index(s, "/"); i >= 0 {
		s = s[:i]
	}
	var words []string
	for _, w := range wordsRe.FindAllString(s, -1) {
		if !noise[w] {
			words = append(words, w)
		}
	}
	f := strings.Join(words, "-")
	if len(f) > 40 {
		f = strings.TrimRight(f[:40], "-")
	}
	if f == "" {
		return vendor + "-gpu"
	}
	return f
}

// Bucket is a memory size's band in GB, rounded so an 8 GB card that
// reports a little less is still 8.
func Bucket(bytes uint64) string {
	gb := math.Round(float64(bytes) / (1 << 30))
	switch {
	case gb < 8:
		return "0-8"
	case gb < 16:
		return "8-16"
	case gb < 32:
		return "16-32"
	case gb < 64:
		return "32-64"
	case gb < 128:
		return "64-128"
	}
	return "128+"
}

// Key is the exact cohort, such as apple:m4-max:unified:32-64.
func (h Hardware) Key() string {
	return h.Vendor + ":" + h.Family + ":" + h.MemoryType + ":" + h.MemoryBucket
}

var (
	appleRe  = regexp.MustCompile(`^m\d+`)
	rtxRe    = regexp.MustCompile(`^rtx-(?:a?)(\d{2})\d{2}`)
	gtxRe    = regexp.MustCompile(`^(gtx|rtx)-(\d{1,2})`)
	radeonRe = regexp.MustCompile(`^rx-(\d)\d{3}`)
	arcRe    = regexp.MustCompile(`^arc-([ab])\d+`)
)

// Class is the accelerator class a family belongs to, such as
// apple-silicon or rtx-40. It matches the ratings service's.
func Class(vendor, family string) string {
	switch vendor {
	case "apple":
		if appleRe.MatchString(family) {
			return "apple-silicon"
		}
	case "nvidia":
		if m := rtxRe.FindStringSubmatch(family); m != nil {
			return "rtx-" + m[1]
		}
		if m := gtxRe.FindStringSubmatch(family); m != nil {
			return m[1] + "-" + m[2]
		}
		return "nvidia-other"
	case "amd":
		if m := radeonRe.FindStringSubmatch(family); m != nil {
			return "rx-" + m[1] + "000"
		}
		return "amd-other"
	case "intel":
		if m := arcRe.FindStringSubmatch(family); m != nil {
			return "arc-" + m[1]
		}
		return "intel-other"
	case "cpu":
		return "cpu"
	}
	return vendor + "-other"
}

// Cohorts are the published cohorts hardware like h belongs to, narrowest
// first: family, class, then backend. The exact cohort is never published.
func Cohorts(h Hardware, backend string) [3]Cohort {
	return [3]Cohort{
		{Tier: "family", Key: h.Vendor + ":" + h.Family},
		{Tier: "class", Key: h.Vendor + ":" + Class(h.Vendor, h.Family) + ":" + h.MemoryBucket},
		{Tier: "backend", Key: backend + ":" + h.MemoryBucket},
	}
}

// Cohort is one group of similar hardware.
type Cohort struct {
	Tier string
	Key  string
}
