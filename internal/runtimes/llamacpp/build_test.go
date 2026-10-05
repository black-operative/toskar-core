package llamacpp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// b11429Assets is the asset list of llama.cpp release b11429 (October 2026).
var b11429Assets = []string{
	"cudart-llama-b11429-bin-ubuntu-cuda-12.8-x64.tar.gz",
	"cudart-llama-b11429-bin-ubuntu-cuda-13.4-arm64.tar.gz",
	"cudart-llama-b11429-bin-ubuntu-cuda-13.4-x64.tar.gz",
	"cudart-llama-bin-win-cuda-12.4-x64.zip",
	"cudart-llama-bin-win-cuda-13.4-arm64.zip",
	"cudart-llama-bin-win-cuda-13.4-x64.zip",
	"llama-b11429-bin-android-arm64-snapdragon.tar.gz",
	"llama-b11429-bin-android-arm64.tar.gz",
	"llama-b11429-bin-linux-arm64-snapdragon.tar.gz",
	"llama-b11429-bin-macos-arm64.tar.gz",
	"llama-b11429-bin-macos-x64.tar.gz",
	"llama-b11429-bin-ubuntu-arm64.tar.gz",
	"llama-b11429-bin-ubuntu-cuda-12.8-x64.tar.gz",
	"llama-b11429-bin-ubuntu-cuda-13.4-arm64.tar.gz",
	"llama-b11429-bin-ubuntu-cuda-13.4-x64.tar.gz",
	"llama-b11429-bin-ubuntu-openvino-2026.4.1-x64.tar.gz",
	"llama-b11429-bin-ubuntu-rocm-10.0-x64.tar.gz",
	"llama-b11429-bin-ubuntu-s390x.tar.gz",
	"llama-b11429-bin-ubuntu-sycl-fp16-x64.tar.gz",
	"llama-b11429-bin-ubuntu-sycl-fp32-x64.tar.gz",
	"llama-b11429-bin-ubuntu-vulkan-arm64.tar.gz",
	"llama-b11429-bin-ubuntu-vulkan-x64.tar.gz",
	"llama-b11429-bin-ubuntu-x64.tar.gz",
	"llama-b11429-bin-win-cpu-arm64.zip",
	"llama-b11429-bin-win-cpu-x64.zip",
	"llama-b11429-bin-win-cuda-12.4-x64.zip",
	"llama-b11429-bin-win-cuda-13.4-arm64.zip",
	"llama-b11429-bin-win-cuda-13.4-x64.zip",
	"llama-b11429-bin-win-opencl-adreno-arm64.zip",
	"llama-b11429-bin-win-openvino-2026.4.1-x64.zip",
	"llama-b11429-bin-win-rocm-10.0-x64.zip",
	"llama-b11429-bin-win-sycl-x64.zip",
	"llama-b11429-bin-win-vulkan-arm64.zip",
	"llama-b11429-bin-win-vulkan-x64.zip",
	"llama-b11429-ui.tar.gz",
	"llama-b11429-xcframework.zip",
}

func release(names ...string) ghRelease {
	var rel ghRelease
	for _, n := range names {
		rel.Assets = append(rel.Assets, struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		}{Name: n, BrowserDownloadURL: "https://example.test/" + n})
	}
	return rel
}

// testRuntime is a Runtime for goos/goarch with the given releases and GPU.
func testRuntime(t *testing.T, goos, goarch string, vulkan bool, rels ...ghRelease) *Runtime {
	t.Helper()
	r := New(t.TempDir(), t.TempDir())
	r.goos, r.goarch = goos, goarch
	r.releases = func(context.Context) ([]ghRelease, error) { return rels, nil }
	r.vulkan = func(context.Context) bool { return vulkan }
	return r
}

func TestChooseAsset(t *testing.T) {
	// The newest "release" is a nightly marker with no builds, as on GitHub.
	nightly := release("nightly-tag.txt")
	current := release(b11429Assets...)
	cases := []struct {
		goos, goarch string
		vulkan       bool
		want         string
	}{
		{"linux", "amd64", true, "llama-b11429-bin-ubuntu-vulkan-x64.tar.gz"},
		{"linux", "amd64", false, "llama-b11429-bin-ubuntu-x64.tar.gz"},
		{"linux", "arm64", true, "llama-b11429-bin-ubuntu-vulkan-arm64.tar.gz"},
		{"linux", "arm64", false, "llama-b11429-bin-ubuntu-arm64.tar.gz"},
		{"windows", "amd64", true, "llama-b11429-bin-win-vulkan-x64.zip"},
		{"windows", "amd64", false, "llama-b11429-bin-win-cpu-x64.zip"},
		{"windows", "arm64", false, "llama-b11429-bin-win-cpu-arm64.zip"},
		// macOS builds have Metal; a Vulkan answer changes nothing there.
		{"darwin", "arm64", true, "llama-b11429-bin-macos-arm64.tar.gz"},
		{"darwin", "amd64", false, "llama-b11429-bin-macos-x64.tar.gz"},
	}
	for _, c := range cases {
		r := testRuntime(t, c.goos, c.goarch, c.vulkan, nightly, current)
		got, err := r.chooseAsset(context.Background())
		if err != nil {
			t.Fatalf("%s/%s vulkan=%v: %v", c.goos, c.goarch, c.vulkan, err)
		}
		if got.Name != c.want {
			t.Errorf("%s/%s vulkan=%v: got %s, want %s", c.goos, c.goarch, c.vulkan, got.Name, c.want)
		}
		if got.URL != "https://example.test/"+c.want {
			t.Errorf("%s: url %s", c.want, got.URL)
		}
	}
}

func TestChooseAssetFallsBackToCPUInNewestRelease(t *testing.T) {
	// A newer release whose Vulkan build is missing (still building, say)
	// gets its CPU build rather than an older release's Vulkan one.
	newer := release("llama-b2-bin-ubuntu-x64.tar.gz", "llama-b2-bin-ubuntu-cuda-12.8-x64.tar.gz")
	older := release("llama-b1-bin-ubuntu-vulkan-x64.tar.gz", "llama-b1-bin-ubuntu-x64.tar.gz")
	r := testRuntime(t, "linux", "amd64", true, newer, older)
	got, err := r.chooseAsset(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "llama-b2-bin-ubuntu-x64.tar.gz" {
		t.Fatalf("got %s", got.Name)
	}
}

func TestChooseAssetNeverPicksOtherVariants(t *testing.T) {
	// Only variants and runtime packs: nothing here is the build we want,
	// though several contain "bin-ubuntu-" or "vulkan" or "x64".
	rel := release(
		"cudart-llama-b1-bin-ubuntu-vulkan-x64.tar.gz",
		"llama-b1-bin-ubuntu-vulkan-x64-debug.tar.gz",
		"llama-b1-bin-ubuntu-x64-noavx.tar.gz",
		"llama-b1-bin-ubuntu-cuda-12.8-x64.tar.gz",
		"llama-b1-bin-ubuntu-vulkan-x64.tar.gz.sha256",
	)
	r := testRuntime(t, "linux", "amd64", true, rel)
	if got, err := r.chooseAsset(context.Background()); err == nil {
		t.Fatalf("picked %s", got.Name)
	}
}

func TestChooseAssetReleaseError(t *testing.T) {
	r := testRuntime(t, "linux", "amd64", true)
	r.releases = func(context.Context) ([]ghRelease, error) { return nil, errors.New("offline") }
	if _, err := r.chooseAsset(context.Background()); err == nil {
		t.Fatal("want error")
	}
}

func TestAssetBuild(t *testing.T) {
	cases := map[string]string{
		"llama-b11429-bin-ubuntu-vulkan-x64.tar.gz": "ubuntu-vulkan-x64",
		"llama-b11429-bin-win-cpu-x64.zip":          "win-cpu-x64",
		"LLAMA-B1-BIN-UBUNTU-X64.TGZ":               "ubuntu-x64",
		"cudart-llama-bin-win-cuda-12.4-x64.zip":    "",
		"llama-b11429-ui.tar.gz":                    "",
		"llama-b11429-bin-ubuntu-x64.tar.gz.sha256": "",
		"nightly-tag.txt":                           "",
	}
	for name, want := range cases {
		got, ok := assetBuild(name)
		if ok != (want != "") || got != want {
			t.Errorf("%s: got %q %v, want %q", name, got, ok, want)
		}
	}
}

// fakeHost is a filesystem and toolset for vulkanProbe.
type fakeHost struct {
	files      []string
	vulkaninfo string // "" means vulkaninfo is not installed
	gpu        bool
}

func (h fakeHost) probe(goos string) vulkanProbe {
	return vulkanProbe{
		goos: goos,
		exists: func(path string) bool {
			for _, f := range h.files {
				if filepath.FromSlash(f) == path {
					return true
				}
			}
			return false
		},
		glob: func(pattern string) []string {
			var out []string
			for _, f := range h.files {
				if ok, _ := filepath.Match(pattern, filepath.FromSlash(f)); ok {
					out = append(out, filepath.FromSlash(f))
				}
			}
			return out
		},
		vulkaninfo: func(context.Context) (string, error) {
			if h.vulkaninfo == "" {
				return "", errors.New("not installed")
			}
			return h.vulkaninfo, nil
		},
		hasGPU: func(context.Context) bool { return h.gpu },
	}
}

const summaryNVIDIA = `Devices:
========
GPU0:
	apiVersion         = 1.3.280
	vendorID           = 0x10de
	deviceType         = PHYSICAL_DEVICE_TYPE_DISCRETE_GPU
	deviceName         = NVIDIA GeForce RTX 3060
GPU1:
	deviceType         = PHYSICAL_DEVICE_TYPE_CPU
	deviceName         = llvmpipe (LLVM 17.0.6, 256 bits)
`

const summaryLlvmpipe = `Devices:
========
GPU0:
	deviceType         = PHYSICAL_DEVICE_TYPE_CPU
	deviceName         = llvmpipe (LLVM 17.0.6, 256 bits)
`

func TestVulkanProbe(t *testing.T) {
	loader := "/usr/lib/x86_64-linux-gnu/libvulkan.so.1"
	nvidiaICD := "/usr/share/vulkan/icd.d/nvidia_icd.json"
	lavapipe := "/usr/share/vulkan/icd.d/lvp_icd.x86_64.json"
	cases := []struct {
		name string
		host fakeHost
		want bool
	}{
		{"gpu, loader and driver", fakeHost{files: []string{loader, nvidiaICD}, gpu: true}, true},
		{"no loader", fakeHost{files: []string{nvidiaICD}, gpu: true, vulkaninfo: summaryNVIDIA}, false},
		{"only lavapipe", fakeHost{files: []string{loader, lavapipe}, gpu: true}, false},
		{"driver but no gpu found", fakeHost{files: []string{loader, nvidiaICD}}, false},
		{"vulkaninfo sees a gpu", fakeHost{files: []string{loader}, vulkaninfo: summaryNVIDIA}, true},
		{"vulkaninfo sees only llvmpipe", fakeHost{files: []string{loader, nvidiaICD, lavapipe}, gpu: true, vulkaninfo: summaryLlvmpipe}, false},
	}
	for _, c := range cases {
		if got := c.host.probe("linux").usable(context.Background()); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestVulkanProbeWindows(t *testing.T) {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	dll := filepath.Join(root, "System32", "vulkan-1.dll")
	if !(fakeHost{files: []string{dll}, gpu: true}).probe("windows").usable(context.Background()) {
		t.Error("gpu with vulkan-1.dll: want usable")
	}
	if (fakeHost{gpu: true}).probe("windows").usable(context.Background()) {
		t.Error("no vulkan-1.dll: want unusable")
	}
	if (fakeHost{files: []string{dll}}).probe("windows").usable(context.Background()) {
		t.Error("no gpu: want unusable")
	}
}

func TestSoftwareAdapter(t *testing.T) {
	for _, m := range []string{"Microsoft Basic Display Adapter", "Microsoft Remote Display Adapter"} {
		if !softwareAdapter(m) {
			t.Errorf("%s: want software", m)
		}
	}
	if softwareAdapter("AMD Radeon RX 7800 XT") {
		t.Error("Radeon: want hardware")
	}
}

func installFake(t *testing.T, r *Runtime, libs ...string) {
	t.Helper()
	dir := r.installDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range append([]string{filepath.Base(r.binaryPath())}, libs...) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCapabilitiesFollowInstalledBuild(t *testing.T) {
	ctx := context.Background()
	cpu := testRuntime(t, "linux", "amd64", true)
	installFake(t, cpu, "libggml-cpu-haswell.so", "libggml-base.so")
	caps, _ := cpu.Capabilities(ctx)
	if !reflect.DeepEqual(caps.Backends, []string{"cpu"}) || caps.SupportsGPU {
		t.Errorf("CPU build: got %v gpu=%v", caps.Backends, caps.SupportsGPU)
	}

	vk := testRuntime(t, "linux", "amd64", true)
	installFake(t, vk, "libggml-cpu-haswell.so", "libggml-vulkan.so")
	caps, _ = vk.Capabilities(ctx)
	if !reflect.DeepEqual(caps.Backends, []string{"cpu", "vulkan"}) || !caps.SupportsGPU {
		t.Errorf("Vulkan build: got %v gpu=%v", caps.Backends, caps.SupportsGPU)
	}

	win := testRuntime(t, "windows", "amd64", true)
	installFake(t, win, "ggml-vulkan.dll")
	if caps, _ = win.Capabilities(ctx); !reflect.DeepEqual(caps.Backends, []string{"cpu", "vulkan"}) {
		t.Errorf("Windows Vulkan build: got %v", caps.Backends)
	}

	mac := testRuntime(t, "darwin", "arm64", false)
	if caps, _ = mac.Capabilities(ctx); !reflect.DeepEqual(caps.Backends, []string{"cpu", "metal"}) {
		t.Errorf("macOS: got %v", caps.Backends)
	}
}

func TestUpgradeAvailable(t *testing.T) {
	ctx := context.Background()

	none := testRuntime(t, "linux", "amd64", true)
	if none.UpgradeAvailable(ctx) {
		t.Error("nothing installed: no upgrade")
	}

	cpu := testRuntime(t, "linux", "amd64", true)
	installFake(t, cpu, "libggml-cpu-haswell.so")
	if !cpu.UpgradeAvailable(ctx) {
		t.Error("CPU build with a usable GPU: want upgrade")
	}
	det, _ := cpu.Detect(ctx)
	if !det.Installed || !strings.Contains(det.Message, "GPU") {
		t.Errorf("detection should say a GPU build fits: %+v", det)
	}

	noGPU := testRuntime(t, "linux", "amd64", false)
	installFake(t, noGPU, "libggml-cpu-haswell.so")
	if noGPU.UpgradeAvailable(ctx) {
		t.Error("CPU build without a GPU: no upgrade")
	}

	vk := testRuntime(t, "windows", "amd64", true)
	installFake(t, vk, "ggml-vulkan.dll")
	if vk.UpgradeAvailable(ctx) {
		t.Error("Vulkan build: no upgrade")
	}

	// A CUDA build someone placed there themselves is a GPU build too.
	cuda := testRuntime(t, "linux", "amd64", true)
	installFake(t, cuda, "libggml-cuda.so")
	if cuda.UpgradeAvailable(ctx) {
		t.Error("CUDA build: no upgrade")
	}
}

func TestRemoveBackendLibs(t *testing.T) {
	dir := t.TempDir()
	keep := []string{"llama-server", "libllama.so", "libmtmd.so", "LICENSE"}
	gone := []string{"libggml-vulkan.so", "libggml-cpu-zen4.so", "libggml-base.so", "ggml-vulkan.dll"}
	for _, name := range append(append([]string{}, keep...), gone...) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := removeBackendLibs(dir); err != nil {
		t.Fatal(err)
	}
	for _, name := range keep {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s removed", name)
		}
	}
	for _, name := range gone {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Errorf("%s kept", name)
		}
	}
}
