package gpusetup

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// host is a Linux computer with an AMD card and everything in place;
// each case takes something away.
func host() Host {
	files := map[string]bool{"/usr/lib/x86_64-linux-gnu/libvulkan.so.1": true}
	return Host{
		GOOS:   "linux",
		Cards:  []string{"AMD Radeon RX 7900 XTX"},
		Exists: func(p string) bool { return files[p] },
		Glob: func(p string) []string {
			switch p {
			case "/dev/dri/renderD*":
				return []string{"/dev/dri/renderD128"}
			case "/usr/share/vulkan/icd.d/*.json":
				return []string{"/usr/share/vulkan/icd.d/radeon_icd.x86_64.json"}
			}
			return nil
		},
		ReadFile: func(p string) ([]byte, error) {
			if p == "/etc/os-release" {
				return []byte("NAME=\"Ubuntu\"\nID=ubuntu\nID_LIKE=debian\n"), nil
			}
			return nil, errors.New("no file")
		},
		LookPath:   func(string) (string, error) { return "", errors.New("not found") },
		Vulkaninfo: func(context.Context) (string, error) { return "deviceType = PHYSICAL_DEVICE_TYPE_DISCRETE_GPU", nil },
		CanOpen:    func(string) bool { return true },
		User:       "yggdrasil",
		Service:    true,
	}
}

func codes(s contracts.GPUSetup) []string {
	out := []string{}
	for _, p := range s.Problems {
		out = append(out, p.Code)
	}
	return out
}

func TestCheck(t *testing.T) {
	ctx := context.Background()

	ready := Check(ctx, host())
	if ready.GPU != "AMD Radeon RX 7900 XTX" || len(ready.Problems) != 0 {
		t.Errorf("ready: %+v", ready)
	}

	noLoader := host()
	noLoader.Exists = func(string) bool { return false }
	got := Check(ctx, noLoader)
	if !reflect.DeepEqual(codes(got), []string{contracts.GPUProblemVulkanLoader}) || got.Problems[0].Command != "sudo apt-get install libvulkan1 mesa-vulkan-drivers vulkan-tools" {
		t.Errorf("no loader: %+v", got)
	}

	noDriver := host()
	noDriver.Vulkaninfo = func(context.Context) (string, error) { return "deviceType = PHYSICAL_DEVICE_TYPE_CPU", nil }
	if c := codes(Check(ctx, noDriver)); !reflect.DeepEqual(c, []string{contracts.GPUProblemVulkanDriver}) {
		t.Errorf("only lavapipe: %v", c)
	}

	locked := host()
	locked.CanOpen = func(string) bool { return false }
	got = Check(ctx, locked)
	if !reflect.DeepEqual(codes(got), []string{contracts.GPUProblemRenderAccess}) || got.Problems[0].Command != "sudo usermod -a -G render,video yggdrasil && sudo systemctl restart toskar" {
		t.Errorf("locked render device: %+v", got)
	}
	locked.Service, locked.User = false, "mike"
	if cmd := Check(ctx, locked).Problems[0].Command; !strings.HasPrefix(cmd, "sudo usermod -a -G render,video mike") || !strings.Contains(cmd, "sign out") {
		t.Errorf("a desktop user: %q", cmd)
	}

	fedoraNvidia := host()
	fedoraNvidia.Cards = []string{"NVIDIA GeForce RTX 4090"}
	fedoraNvidia.ReadFile = func(string) ([]byte, error) { return []byte("ID=fedora\n"), nil }
	got = Check(ctx, fedoraNvidia)
	if !reflect.DeepEqual(codes(got), []string{contracts.GPUProblemNVIDIADriver}) || got.Problems[0].Command != "sudo dnf install akmod-nvidia" || got.Problems[0].URL == "" {
		t.Errorf("Fedora without NVIDIA's driver: %+v", got)
	}

	cpuBuild := host()
	cpuBuild.CPUBuild = true
	if c := codes(Check(ctx, cpuBuild)); !reflect.DeepEqual(c, []string{contracts.GPUProblemCPUBuild}) {
		t.Errorf("CPU build: %v", c)
	}

	none := host()
	none.Cards = []string{"CPU inference"}
	if got := Check(ctx, none); got.GPU != "" || len(got.Problems) != 0 {
		t.Errorf("no GPU: %+v", got)
	}

	mac := host()
	mac.GOOS, mac.Cards, mac.CPUBuild = "darwin", []string{"Apple M2 Pro"}, true
	if got := Check(ctx, mac); got.GPU != "Apple M2 Pro" || len(got.Problems) != 0 {
		t.Errorf("a Mac needs nothing: %+v", got)
	}

	windows := host()
	windows.GOOS, windows.Cards = "windows", []string{"Microsoft Basic Display Adapter", "AMD Radeon RX 6600"}
	got = Check(ctx, windows)
	if !reflect.DeepEqual(codes(got), []string{contracts.GPUProblemWindowsDriver}) || !strings.Contains(got.Problems[0].URL, "amd.com") {
		t.Errorf("Windows without its driver: %+v", got)
	}
}
