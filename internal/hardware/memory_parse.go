package hardware

import (
	"strconv"
	"strings"
)

// parseVMStatAvailable reads the output of vm_stat. Available memory is
// free + inactive + speculative pages. That is a current-availability hint,
// not the machine's capacity.
func parseVMStatAvailable(text string) (uint64, bool) {
	page := uint64(4096)
	var free, inactive, speculative uint64
	var saw bool
	for _, line := range strings.Split(text, "\n") {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "page size of") {
			fields := strings.Fields(line)
			for i, field := range fields {
				if field == "of" && i+1 < len(fields) {
					n, err := strconv.ParseUint(strings.Trim(fields[i+1], "bytes."), 10, 64)
					if err == nil && n > 0 {
						page = n
					}
				}
			}
		}
		switch {
		case strings.HasPrefix(line, "Pages free:"):
			free, saw = mustPages(line), true
		case strings.HasPrefix(line, "Pages inactive:"):
			inactive = mustPages(line)
			saw = true
		case strings.HasPrefix(line, "Pages speculative:"):
			speculative = mustPages(line)
			saw = true
		}
	}
	if !saw || page == 0 {
		return 0, false
	}
	return (free + inactive + speculative) * page, true
}

func mustPages(line string) uint64 {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return 0
	}
	raw := strings.TrimSuffix(fields[len(fields)-1], ".")
	n, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// parseSwapUsage reads sysctl vm.swapusage, for example
// "total = 2048.00M  used = 530.50M  free = 1518.00M".
func parseSwapUsage(text string) (total, used uint64, ok bool) {
	fields := strings.Fields(text)
	for i := 0; i+2 < len(fields); i++ {
		if fields[i+1] != "=" {
			continue
		}
		n, parsed := parseSizeToken(fields[i+2])
		if !parsed {
			continue
		}
		switch fields[i] {
		case "total":
			total = n
			ok = true
		case "used":
			used = n
		}
	}
	return total, used, ok
}

func parseSizeToken(token string) (uint64, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return 0, false
	}
	mult := float64(1)
	last := token[len(token)-1]
	switch last {
	case 'K', 'k':
		mult = 1024
		token = token[:len(token)-1]
	case 'M', 'm':
		mult = 1024 * 1024
		token = token[:len(token)-1]
	case 'G', 'g':
		mult = 1024 * 1024 * 1024
		token = token[:len(token)-1]
	}
	f, err := strconv.ParseFloat(token, 64)
	if err != nil || f < 0 {
		return 0, false
	}
	return uint64(f * mult), true
}

// cgroupGroup reads this process's cgroup v2 path from /proc/self/cgroup
// ("0::/system.slice/toskar.service", or "0::/" inside a container).
func cgroupGroup(text string) (string, bool) {
	for _, line := range strings.Split(text, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "0::"); ok {
			return rest, true
		}
	}
	return "", false
}

// capByCgroup limits total and available memory to a cgroup v2 memory
// limit (memory.max, with memory.current in use), so a daemon in a
// container or a service with MemoryMax= picks models that fit what it may
// use rather than the whole machine. "max" or an unreadable value leaves
// them alone.
func capByCgroup(total, avail uint64, maxText, currentText string) (uint64, uint64) {
	limit, err := strconv.ParseUint(strings.TrimSpace(maxText), 10, 64)
	if err != nil || limit == 0 || (total > 0 && limit >= total) {
		return total, avail
	}
	total = limit
	if used, err := strconv.ParseUint(strings.TrimSpace(currentText), 10, 64); err == nil {
		free := uint64(0)
		if used < limit {
			free = limit - used
		}
		if free < avail || avail == 0 {
			avail = free
		}
	} else if avail > limit {
		avail = limit
	}
	return total, avail
}
