package llamacpp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/yeixio/toskar-core/internal/hardware"
)

// llama.cpp loads its GPU backends from libraries beside llama-server, such
// as libggml-vulkan.so or ggml-vulkan.dll. Which ones are there says which
// build is installed.
var gpuBackendLibs = map[string]string{
	"vulkan": "vulkan",
	"cuda":   "cuda",
	"hip":    "rocm",
	"sycl":   "sycl",
}

// installedGPUBackends lists the GPU backends whose libraries sit in dir.
func installedGPUBackends(dir string) []string {
	var out []string
	for _, lib := range []string{"vulkan", "cuda", "hip", "sycl"} {
		for _, name := range []string{"libggml-" + lib + ".so", "ggml-" + lib + ".dll"} {
			if st, err := os.Stat(filepath.Join(dir, name)); err == nil && !st.IsDir() {
				out = append(out, gpuBackendLibs[lib])
				break
			}
		}
	}
	return out
}

// isBackendLib reports whether name is one of llama.cpp's ggml backend
// libraries, which llama-server loads by scanning its own folder.
func isBackendLib(name string) bool {
	lower := strings.ToLower(name)
	if !strings.HasPrefix(lower, "libggml-") && !strings.HasPrefix(lower, "ggml-") {
		return false
	}
	return strings.HasSuffix(lower, ".so") || strings.HasSuffix(lower, ".dll")
}

// platformBuilds lists the release builds that run on goos/goarch, best
// first. The names are what follows "-bin-" in an asset such as
// llama-b11429-bin-ubuntu-vulkan-x64.tar.gz.
func platformBuilds(goos, goarch string, vulkan bool) []string {
	var gpu, cpu string
	switch {
	case goos == "darwin" && goarch == "arm64":
		cpu = "macos-arm64"
	case goos == "darwin" && goarch == "amd64":
		cpu = "macos-x64"
	case goos == "linux" && goarch == "amd64":
		gpu, cpu = "ubuntu-vulkan-x64", "ubuntu-x64"
	case goos == "linux" && goarch == "arm64":
		gpu, cpu = "ubuntu-vulkan-arm64", "ubuntu-arm64"
	case goos == "windows" && goarch == "amd64":
		gpu, cpu = "win-vulkan-x64", "win-cpu-x64"
	case goos == "windows" && goarch == "arm64":
		gpu, cpu = "win-vulkan-arm64", "win-cpu-arm64"
	default:
		return []string{goos + "-" + goarch}
	}
	if vulkan && gpu != "" {
		return []string{gpu, cpu}
	}
	return []string{cpu}
}

// buildHasGPU reports whether a platformBuilds name is a GPU build.
func buildHasGPU(build string) bool {
	return strings.Contains(build, "-vulkan-")
}

// assetBuild returns the build an asset name carries, such as
// "ubuntu-vulkan-x64" for llama-b11429-bin-ubuntu-vulkan-x64.tar.gz. Only the
// main llama-* archives count, never cudart-* runtime packs.
func assetBuild(name string) (string, bool) {
	lower := strings.ToLower(name)
	if !strings.HasPrefix(lower, "llama-") {
		return "", false
	}
	switch {
	case strings.HasSuffix(lower, ".tar.gz"):
		lower = strings.TrimSuffix(lower, ".tar.gz")
	case strings.HasSuffix(lower, ".tgz"):
		lower = strings.TrimSuffix(lower, ".tgz")
	case strings.HasSuffix(lower, ".zip"):
		lower = strings.TrimSuffix(lower, ".zip")
	default:
		return "", false
	}
	i := strings.Index(lower, "-bin-")
	if i < 0 {
		return "", false
	}
	return lower[i+len("-bin-"):], true
}

// pickAsset returns the best build in the newest release that has one of
// builds. A newer release without the GPU build still wins over an older one
// with it, falling back to its CPU build.
func pickAsset(rels []ghRelease, builds []string) (assetInfo, error) {
	for _, rel := range rels {
		found := map[string]assetInfo{}
		for _, a := range rel.Assets {
			if b, ok := assetBuild(a.Name); ok {
				found[b] = assetInfo{Name: a.Name, URL: a.BrowserDownloadURL, Build: b}
			}
		}
		for _, b := range builds {
			if a, ok := found[b]; ok {
				return a, nil
			}
		}
	}
	return assetInfo{}, fmt.Errorf("no release asset for %s found; install llama-server manually", strings.Join(builds, " or "))
}

// vulkanProbe decides whether llama.cpp's Vulkan build would use a GPU here.
// Its fields are the host lookups, so tests can stand in for them.
type vulkanProbe struct {
	goos string
	// exists reports whether a file is there.
	exists func(path string) bool
	// glob lists files matching a pattern.
	glob func(pattern string) []string
	// vulkaninfo runs `vulkaninfo --summary`; an error means it could not run.
	vulkaninfo func(ctx context.Context) (string, error)
	// hasGPU reports whether the hardware inventory found a graphics card.
	hasGPU func(ctx context.Context) bool
}

func hostVulkanProbe() vulkanProbe {
	return vulkanProbe{
		goos: runtime.GOOS,
		exists: func(path string) bool {
			st, err := os.Stat(path)
			return err == nil && !st.IsDir()
		},
		glob: func(pattern string) []string {
			m, _ := filepath.Glob(pattern)
			return m
		},
		vulkaninfo: func(ctx context.Context) (string, error) {
			path, err := exec.LookPath("vulkaninfo")
			if err != nil {
				return "", err
			}
			out, err := exec.CommandContext(ctx, path, "--summary").Output()
			return string(out), err
		},
		hasGPU: func(ctx context.Context) bool {
			inv, err := (&hardware.Detector{}).Detect(ctx)
			if err != nil {
				return false
			}
			for _, a := range inv.Accelerators {
				if a.Kind == "gpu" && !softwareAdapter(a.Model) {
					return true
				}
			}
			return false
		},
	}
}

// softwareAdapter reports display adapters that are not a GPU, such as the
// one Windows uses before a driver is installed or over Remote Desktop.
func softwareAdapter(model string) bool {
	lower := strings.ToLower(model)
	return strings.Contains(lower, "basic display") || strings.Contains(lower, "basic render") ||
		strings.Contains(lower, "remote display")
}

// usable reports whether the Vulkan loader is installed and finds a GPU.
func (p vulkanProbe) usable(ctx context.Context) bool {
	if !p.loader() {
		return false
	}
	// vulkaninfo, when installed, knows best: it lists the devices the
	// loader finds, and a software renderer like llvmpipe reports a CPU.
	if out, err := p.vulkaninfo(ctx); err == nil {
		return summaryHasGPU(out)
	}
	if p.goos == "linux" && !p.gpuDriver() {
		return false
	}
	return p.hasGPU(ctx)
}

// loader reports whether the Vulkan loader library can be found.
func (p vulkanProbe) loader() bool {
	switch p.goos {
	case "linux":
		for _, dir := range []string{
			"/usr/lib/x86_64-linux-gnu", "/usr/lib/aarch64-linux-gnu",
			"/usr/lib64", "/usr/lib", "/lib/x86_64-linux-gnu", "/lib/aarch64-linux-gnu", "/lib64",
			"/usr/local/lib",
		} {
			if p.exists(filepath.Join(dir, "libvulkan.so.1")) {
				return true
			}
		}
		return false
	case "windows":
		root := os.Getenv("SystemRoot")
		if root == "" {
			root = `C:\Windows`
		}
		return p.exists(filepath.Join(root, "System32", "vulkan-1.dll"))
	}
	return false
}

// gpuDriver reports whether a Vulkan driver for a GPU is registered. Mesa's
// lavapipe (lvp) renders on the CPU, so it does not count.
func (p vulkanProbe) gpuDriver() bool {
	for _, dir := range []string{"/usr/share/vulkan/icd.d", "/etc/vulkan/icd.d", "/usr/local/share/vulkan/icd.d"} {
		for _, f := range p.glob(filepath.Join(dir, "*.json")) {
			if !strings.HasPrefix(strings.ToLower(filepath.Base(f)), "lvp_") {
				return true
			}
		}
	}
	return false
}

// summaryHasGPU reports whether `vulkaninfo --summary` lists a GPU device.
func summaryHasGPU(out string) bool {
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "deviceType") {
			continue
		}
		for _, t := range []string{"DISCRETE_GPU", "INTEGRATED_GPU", "VIRTUAL_GPU"} {
			if strings.Contains(line, t) {
				return true
			}
		}
	}
	return false
}
