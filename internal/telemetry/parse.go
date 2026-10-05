// Package telemetry reads a computer's live CPU, memory, and GPU figures,
// on Linux, macOS, and Windows, without root or admin rights (#317). A
// figure a platform can't give is left out, never reported as 0.
package telemetry

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

func f64(v float64) *float64 { return &v }
func u64(v uint64) *uint64   { return &v }

// cpuTimes are /proc/stat's first line: the time all CPUs spent busy and
// in total, in clock ticks.
type cpuTimes struct{ busy, total uint64 }

// parseProcStat reads the aggregate "cpu" line of /proc/stat.
func parseProcStat(text string) (cpuTimes, bool) {
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 5 || f[0] != "cpu" {
			continue
		}
		var t cpuTimes
		for i, s := range f[1:] {
			v, err := strconv.ParseUint(s, 10, 64)
			if err != nil {
				return cpuTimes{}, false
			}
			t.total += v
			// idle (3) and iowait (4) are not busy; guest time is already
			// in user and nice, so it is not counted twice.
			switch i {
			case 3, 4:
			case 8, 9:
				t.total -= v
			default:
				t.busy += v
			}
		}
		return t, true
	}
	return cpuTimes{}, false
}

// cpuPercent is the busy share between two readings.
func cpuPercent(prev, cur cpuTimes) (float64, bool) {
	if cur.total <= prev.total || cur.busy < prev.busy {
		return 0, false
	}
	return 100 * float64(cur.busy-prev.busy) / float64(cur.total-prev.total), true
}

// parseMeminfo reads /proc/meminfo: memory used is total less available.
func parseMeminfo(text string) (used, total uint64, ok bool) {
	var avail uint64
	var haveTotal, haveAvail bool
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		v, err := strconv.ParseUint(f[1], 10, 64)
		if err != nil {
			continue
		}
		switch f[0] {
		case "MemTotal:":
			total, haveTotal = v*1024, true
		case "MemAvailable:":
			avail, haveAvail = v*1024, true
		}
	}
	if !haveTotal || !haveAvail || avail > total {
		return 0, 0, false
	}
	return total - avail, total, true
}

// parseNvidiaSMI reads `nvidia-smi --query-gpu=name,utilization.gpu,
// memory.used,memory.total,temperature.gpu,power.draw --format=csv,noheader,
// nounits`: one line per card, "[N/A]" or "[Not Supported]" where a card
// doesn't say.
func parseNvidiaSMI(text string) []contracts.GPUSample {
	num := func(s string) (float64, bool) {
		v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		return v, err == nil
	}
	var out []contracts.GPUSample
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		f := strings.Split(line, ",")
		if len(f) < 6 || strings.TrimSpace(f[0]) == "" {
			continue
		}
		g := contracts.GPUSample{Name: strings.TrimSpace(f[0])}
		if v, ok := num(f[1]); ok {
			g.BusyPercent = f64(v)
		}
		if v, ok := num(f[2]); ok {
			g.MemoryUsedBytes = u64(uint64(v) << 20)
		}
		if v, ok := num(f[3]); ok {
			g.MemoryTotalBytes = u64(uint64(v) << 20)
		}
		if v, ok := num(f[4]); ok {
			g.TemperatureC = f64(v)
		}
		if v, ok := num(f[5]); ok {
			g.PowerWatts = f64(v)
		}
		out = append(out, g)
	}
	return out
}

var (
	// ioreg lists each GPU as an IOAccelerator; its PerformanceStatistics
	// has "Device Utilization %" and, on Apple silicon, "In use system
	// memory" (bytes the GPU is using of the shared memory).
	ioregModelRe = regexp.MustCompile(`"model" = "([^"]+)"`)
	ioregBusyRe  = regexp.MustCompile(`"Device Utilization %"=(\d+)`)
	ioregMemRe   = regexp.MustCompile(`"In use system memory"=(\d+)`)
	// "CPU usage: 6.25% user, 9.37% sys, 84.37% idle"
	topCPURe = regexp.MustCompile(`CPU usage: [\d.]+% user, [\d.]+% sys, ([\d.]+)% idle`)
)

// parseIoreg reads `ioreg -r -d 1 -w 0 -c IOAccelerator`: one entry per
// GPU, each starting with "+-o".
func parseIoreg(text string) []contracts.GPUSample {
	var out []contracts.GPUSample
	for _, block := range strings.Split(text, "+-o ")[1:] {
		busy := ioregBusyRe.FindStringSubmatch(block)
		if busy == nil {
			continue
		}
		name := "GPU"
		if m := ioregModelRe.FindStringSubmatch(block); m != nil {
			name = m[1]
		}
		g := contracts.GPUSample{Name: name}
		if v, err := strconv.ParseFloat(busy[1], 64); err == nil {
			g.BusyPercent = f64(v)
		}
		if m := ioregMemRe.FindStringSubmatch(block); m != nil {
			if v, err := strconv.ParseUint(m[1], 10, 64); err == nil {
				g.MemoryUsedBytes = u64(v)
			}
		}
		out = append(out, g)
	}
	return out
}

// parseTopCPU reads the last "CPU usage" line of `top -l 2 -n 0 -s 1` (the
// first covers all time since boot).
func parseTopCPU(text string) (float64, bool) {
	m := topCPURe.FindAllStringSubmatch(text, -1)
	if len(m) == 0 {
		return 0, false
	}
	idle, err := strconv.ParseFloat(m[len(m)-1][1], 64)
	if err != nil {
		return 0, false
	}
	return 100 - idle, true
}

// parseVMStatUsed reads vm_stat: memory in use is active, wired, and
// compressed pages, as Activity Monitor counts "Memory Used" (near enough).
func parseVMStatUsed(text string) (uint64, bool) {
	page := uint64(4096)
	if m := regexp.MustCompile(`page size of (\d+) bytes`).FindStringSubmatch(text); m != nil {
		page, _ = strconv.ParseUint(m[1], 10, 64)
	}
	var pages uint64
	seen := false
	for _, line := range strings.Split(text, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "Pages active", "Pages wired down", "Pages occupied by compressor":
			n, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimSpace(v), "."), 10, 64)
			if err == nil {
				pages += n
				seen = true
			}
		}
	}
	return pages * page, seen
}

// parseTypeperf reads `typeperf -sc 1 <counters>`: a CSV header naming
// each counter, then one row of values.
func parseTypeperf(text string) map[string]float64 {
	var rows [][]string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, `"`) {
			continue
		}
		var cells []string
		for _, c := range strings.Split(line, `","`) {
			cells = append(cells, strings.Trim(c, `"`))
		}
		rows = append(rows, cells)
	}
	out := map[string]float64{}
	if len(rows) < 2 {
		return out
	}
	header, values := rows[0], rows[1]
	for i := 1; i < len(header) && i < len(values); i++ {
		if v, err := strconv.ParseFloat(strings.TrimSpace(values[i]), 64); err == nil {
			out[header[i]] = v
		}
	}
	return out
}

// windowsGPUs sums typeperf's per-engine and per-adapter GPU counters into
// one busy % and one dedicated memory figure per adapter (luid).
func windowsGPUs(values map[string]float64, names map[string]string) []contracts.GPUSample {
	luidRe := regexp.MustCompile(`luid_(0x[0-9a-fA-F]+_0x[0-9a-fA-F]+)`)
	busy := map[string]float64{}
	mem := map[string]float64{}
	var order []string
	seen := map[string]bool{}
	for counter, v := range values {
		m := luidRe.FindStringSubmatch(counter)
		if m == nil {
			continue
		}
		id := m[1]
		if !seen[id] {
			seen[id] = true
			order = append(order, id)
		}
		switch {
		case strings.Contains(counter, `\Utilization Percentage`) && strings.Contains(counter, "engtype_3D"):
			busy[id] += v
		case strings.Contains(counter, `\Dedicated Usage`):
			mem[id] += v
		}
	}
	// Map iteration order varies; keep adapters in a stable order.
	sort.Strings(order)
	var out []contracts.GPUSample
	for _, id := range order {
		name := names[id]
		if name == "" {
			name = "GPU"
		}
		g := contracts.GPUSample{Name: name}
		if b, ok := busy[id]; ok {
			g.BusyPercent = f64(min(b, 100))
		}
		if m, ok := mem[id]; ok {
			g.MemoryUsedBytes = u64(uint64(m))
		}
		if g.BusyPercent != nil || g.MemoryUsedBytes != nil {
			out = append(out, g)
		}
	}
	return out
}
