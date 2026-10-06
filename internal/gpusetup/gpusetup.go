// Package gpusetup finds what stands between a computer's graphics card and
// Toskar using it, and how to fix each piece on that computer (#317): the
// Vulkan loader and drivers, permission to open the card, NVIDIA's driver,
// and the CPU-only llama.cpp build. It only looks; it changes nothing.
package gpusetup

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Host is what Check looks at, so tests can stand in for a computer.
type Host struct {
	GOOS string
	// Cards are the graphics cards found, such as from the hardware
	// inventory: names, with "NVIDIA", "AMD", or "Intel" in them.
	Cards []string
	// Exists reports whether a file is there.
	Exists func(path string) bool
	// Glob lists files matching a pattern.
	Glob func(pattern string) []string
	// ReadFile reads a file, such as /etc/os-release.
	ReadFile func(path string) ([]byte, error)
	// LookPath finds a command.
	LookPath func(name string) (string, error)
	// Vulkaninfo runs `vulkaninfo --summary`; an error means it couldn't.
	Vulkaninfo func(ctx context.Context) (string, error)
	// CanOpen reports whether this process may open a file read-write.
	CanOpen func(path string) bool
	// User is the account Toskar runs as, such as "yggdrasil".
	User string
	// Service is true when Toskar runs as the system service.
	Service bool
	// CPUBuild is true when the CPU-only llama.cpp is installed where the
	// GPU build would run.
	CPUBuild bool
}

// Check lists the problems, most basic first: a fix further down rarely
// helps while one above is open.
func Check(ctx context.Context, h Host) contracts.GPUSetup {
	out := contracts.GPUSetup{Problems: []contracts.GPUProblem{}}
	cards := realCards(h.Cards)
	if h.GOOS == "darwin" {
		// Metal is part of macOS.
		if len(cards) > 0 {
			out.GPU = cards[0]
		}
		return out
	}
	if len(cards) == 0 {
		return out
	}
	out.GPU = cards[0]
	nvidia := anyContains(cards, "nvidia")
	switch h.GOOS {
	case "linux":
		distro := osRelease(h)
		if nvidia {
			if _, err := h.LookPath("nvidia-smi"); err != nil {
				out.Problems = append(out.Problems, nvidiaFix(distro))
			}
		}
		if !linuxLoader(h) {
			out.Problems = append(out.Problems, contracts.GPUProblem{Code: contracts.GPUProblemVulkanLoader, Command: vulkanPackages(distro)})
		} else if !nvidia || len(out.Problems) == 0 {
			if summary, err := h.Vulkaninfo(ctx); err == nil && !summaryHasGPU(summary) {
				out.Problems = append(out.Problems, contracts.GPUProblem{Code: contracts.GPUProblemVulkanDriver, Command: vulkanPackages(distro)})
			} else if err != nil && !anyICD(h) {
				out.Problems = append(out.Problems, contracts.GPUProblem{Code: contracts.GPUProblemVulkanDriver, Command: vulkanPackages(distro)})
			}
		}
		if p, ok := renderAccess(h); ok {
			out.Problems = append(out.Problems, p)
		}
	case "windows":
		root := "C:\\Windows"
		if v := os.Getenv("SystemRoot"); v != "" {
			root = v
		}
		_, smiErr := h.LookPath("nvidia-smi")
		if !h.Exists(filepath.Join(root, "System32", "vulkan-1.dll")) || (nvidia && smiErr != nil) {
			out.Problems = append(out.Problems, contracts.GPUProblem{Code: contracts.GPUProblemWindowsDriver, URL: driverURL(cards[0])})
		}
	}
	if h.CPUBuild && len(out.Problems) == 0 {
		out.Problems = append(out.Problems, contracts.GPUProblem{Code: contracts.GPUProblemCPUBuild})
	}
	return out
}

// realCards leaves out display adapters that are not a GPU.
func realCards(cards []string) []string {
	var out []string
	for _, c := range cards {
		l := strings.ToLower(c)
		if c == "" || strings.Contains(l, "basic display") || strings.Contains(l, "basic render") ||
			strings.Contains(l, "remote display") || strings.Contains(l, "cpu inference") {
			continue
		}
		out = append(out, c)
	}
	return out
}

func anyContains(list []string, sub string) bool {
	for _, s := range list {
		if strings.Contains(strings.ToLower(s), sub) {
			return true
		}
	}
	return false
}

var linuxLibDirs = []string{"/usr/lib/x86_64-linux-gnu", "/usr/lib/aarch64-linux-gnu", "/usr/lib64", "/usr/lib", "/lib/x86_64-linux-gnu", "/lib/aarch64-linux-gnu", "/lib64", "/usr/local/lib"}

func linuxLoader(h Host) bool {
	for _, d := range linuxLibDirs {
		if h.Exists(filepath.Join(d, "libvulkan.so.1")) {
			return true
		}
	}
	return false
}

// anyICD reports a registered Vulkan driver for a GPU; Mesa's lavapipe
// renders on the CPU, so it doesn't count.
func anyICD(h Host) bool {
	for _, dir := range []string{"/usr/share/vulkan/icd.d", "/etc/vulkan/icd.d", "/usr/local/share/vulkan/icd.d"} {
		for _, f := range h.Glob(filepath.Join(dir, "*.json")) {
			if !strings.HasPrefix(strings.ToLower(filepath.Base(f)), "lvp_") {
				return true
			}
		}
	}
	return false
}

func summaryHasGPU(out string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "deviceType") && (strings.Contains(line, "DISCRETE_GPU") || strings.Contains(line, "INTEGRATED_GPU") || strings.Contains(line, "VIRTUAL_GPU")) {
			return true
		}
	}
	return false
}

// renderAccess is a problem when the card's render devices exist but this
// process may open none of them.
func renderAccess(h Host) (contracts.GPUProblem, bool) {
	nodes := h.Glob("/dev/dri/renderD*")
	if len(nodes) == 0 {
		return contracts.GPUProblem{}, false
	}
	for _, n := range nodes {
		if h.CanOpen(n) {
			return contracts.GPUProblem{}, false
		}
	}
	user := h.User
	if user == "" {
		user = "$USER"
	}
	cmd := "sudo usermod -a -G render,video " + user
	if h.Service {
		cmd += " && sudo systemctl restart toskar"
	} else {
		cmd += ", then sign out and back in"
	}
	return contracts.GPUProblem{Code: contracts.GPUProblemRenderAccess, Command: cmd}, true
}

// distro is /etc/os-release's ID and ID_LIKE.
type distro struct{ id, like string }

func (d distro) is(names ...string) bool {
	for _, n := range names {
		if d.id == n || strings.Contains(" "+d.like+" ", " "+n+" ") {
			return true
		}
	}
	return false
}

var osReleaseRe = regexp.MustCompile(`(?m)^(ID|ID_LIKE)=["']?([^"'\n]*)["']?$`)

func osRelease(h Host) distro {
	raw, err := h.ReadFile("/etc/os-release")
	if err != nil {
		return distro{}
	}
	var d distro
	for _, m := range osReleaseRe.FindAllStringSubmatch(string(raw), -1) {
		if m[1] == "ID" {
			d.id = strings.ToLower(m[2])
		} else {
			d.like = strings.ToLower(m[2])
		}
	}
	return d
}

func vulkanPackages(d distro) string {
	switch {
	case d.is("fedora", "rhel", "centos"):
		return "sudo dnf install vulkan-loader mesa-vulkan-drivers vulkan-tools"
	case d.is("arch"):
		return "sudo pacman -S vulkan-icd-loader vulkan-radeon vulkan-intel vulkan-tools"
	case d.is("opensuse", "suse"):
		return "sudo zypper install libvulkan1 libvulkan_radeon libvulkan_intel vulkan-tools"
	default:
		return "sudo apt-get install libvulkan1 mesa-vulkan-drivers vulkan-tools"
	}
}

func nvidiaFix(d distro) contracts.GPUProblem {
	p := contracts.GPUProblem{Code: contracts.GPUProblemNVIDIADriver}
	switch {
	case d.is("ubuntu"):
		p.Command = "sudo ubuntu-drivers install"
	case d.is("fedora"):
		p.Command, p.URL = "sudo dnf install akmod-nvidia", "https://rpmfusion.org/Howto/NVIDIA"
	case d.is("debian"):
		p.Command, p.URL = "sudo apt-get install nvidia-driver", "https://wiki.debian.org/NvidiaGraphicsDrivers"
	case d.is("arch"):
		p.Command = "sudo pacman -S nvidia"
	default:
		p.URL = "https://www.nvidia.com/Download/index.aspx"
	}
	return p
}

func driverURL(card string) string {
	l := strings.ToLower(card)
	switch {
	case strings.Contains(l, "nvidia"):
		return "https://www.nvidia.com/Download/index.aspx"
	case strings.Contains(l, "amd") || strings.Contains(l, "radeon"):
		return "https://www.amd.com/en/support/download/drivers.html"
	case strings.Contains(l, "intel"):
		return "https://www.intel.com/content/www/us/en/download-center/home.html"
	}
	return ""
}

// PCICards are the display controllers lspci lists, on Linux: an NVIDIA
// card without its driver shows here, though not to nvidia-smi.
func PCICards(ctx context.Context, run func(ctx context.Context, name string, args ...string) (string, error)) []string {
	out, err := run(ctx, "lspci")
	if err != nil {
		return nil
	}
	var cards []string
	for _, line := range strings.Split(out, "\n") {
		l := strings.ToLower(line)
		if strings.Contains(l, "vga") || strings.Contains(l, "3d controller") || strings.Contains(l, "display controller") {
			if _, rest, ok := strings.Cut(line, ": "); ok {
				cards = append(cards, strings.TrimSpace(rest))
			}
		}
	}
	return cards
}
