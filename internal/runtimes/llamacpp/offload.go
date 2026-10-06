package llamacpp

import (
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// llama-server says where it put a model while loading it, before it
// answers: the devices it found, the layers it offloaded, and the buffers it
// allocated on each device. These are the lines, across the Metal, Vulkan,
// CUDA, and ROCm builds and the older (llm_load_tensors) and newer
// (load_tensors) wording. Builds from late 2026 start each line with a
// timestamp and a level ("0.03.244.681 I load_tensors: …") and print these
// only at trace verbosity, which StartModel asks for.
var (
	// "llama_model_load_from_file_impl: using device Vulkan0 (AMD Radeon RX
	// 7900 XTX (RADV NAVI31)) - 24560 MiB free"; newer builds add the
	// device's id: "using device MTL0 (Apple M5 Pro) (unknown id) - …".
	usingDeviceRe = regexp.MustCompile(`using device ([A-Za-z_]+?)\d* \((.+)\) - \d+ MiB free`)
	deviceIDRe    = regexp.MustCompile(`\) \([^()]*\bid\b[^()]*$`)
	// "ggml_vulkan: 0 = AMD Radeon RX 7900 XTX (RADV NAVI31) (radv) | uma: 0 | ..."
	vulkanDeviceRe = regexp.MustCompile(`ggml_vulkan: \d+ = (.+?) \([^()]*\) \|`)
	// "ggml_cuda_init: found 1 ROCm devices:" then "  Device 0: NVIDIA GeForce RTX 4090, compute capability 8.9, VMM: yes"
	cudaInitRe   = regexp.MustCompile(`ggml_cuda_init: found \d+ (CUDA|ROCm) devices`)
	cudaDeviceRe = regexp.MustCompile(`(?:^|\s)Device \d+: ([^,]+),`)
	// "ggml_metal_init: found device: Apple M2 Pro", or newer "GPU name:   Apple M2 Pro"
	metalDeviceRe = regexp.MustCompile(`ggml_metal_\w+: (?:found device: |GPU name:\s+)(.+)$`)
	// "load_tensors: offloaded 29/29 layers to GPU"
	offloadedRe = regexp.MustCompile(`offloaded (\d+)/(\d+) layers to GPU`)
	// "load_tensors:      Vulkan0 model buffer size =  4168.09 MiB", also KV
	// and compute buffers. CPU, CPU_Mapped, and CPU_REPACK buffers are in
	// system memory, and so are Vulkan_Host and CUDA_Host (pinned for
	// copies), so they don't count as GPU memory.
	bufferRe = regexp.MustCompile(`\w+:\s+(\S+)\s+(?:model|KV|compute|output) buffer size =\s+([\d.]+) MiB`)
)

// parseAcceleration reads where llama-server put the model from its log. It
// returns nil when the log says nothing about it, such as one from before
// the model loaded.
func parseAcceleration(log string) *pluginapi.Acceleration {
	a := &pluginapi.Acceleration{}
	// found are the devices a build lists; used are those it says it loads
	// onto ("using device"), which win: a computer's built-in graphics is
	// found but not used beside its graphics card.
	var found, used []string
	add := func(list *[]string, name string) {
		name = strings.TrimSpace(name)
		for _, n := range *list {
			if n == name {
				return
			}
		}
		if name != "" {
			*list = append(*list, name)
		}
	}
	setBackend := func(b string) {
		if a.Backend == "" {
			a.Backend = b
		}
	}
	known := false
	cudaKind := ""
	for _, line := range strings.Split(log, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case usingDeviceRe.MatchString(line):
			m := usingDeviceRe.FindStringSubmatch(line)
			setBackend(backendOf(m[1], cudaKind))
			add(&used, deviceIDRe.ReplaceAllString(m[2], ""))
			known = true
		case vulkanDeviceRe.MatchString(line):
			setBackend("vulkan")
			add(&found, vulkanDeviceRe.FindStringSubmatch(line)[1])
			known = true
		case cudaInitRe.MatchString(line):
			cudaKind = strings.ToLower(cudaInitRe.FindStringSubmatch(line)[1])
		case cudaKind != "" && cudaDeviceRe.MatchString(line):
			setBackend(cudaKind)
			add(&found, cudaDeviceRe.FindStringSubmatch(line)[1])
			known = true
		case metalDeviceRe.MatchString(line):
			setBackend("metal")
			add(&found, metalDeviceRe.FindStringSubmatch(line)[1])
			known = true
		case offloadedRe.MatchString(line):
			m := offloadedRe.FindStringSubmatch(line)
			a.LayersOffloaded, _ = strconv.Atoi(m[1])
			a.LayersTotal, _ = strconv.Atoi(m[2])
			known = true
		case bufferRe.MatchString(line):
			m := bufferRe.FindStringSubmatch(line)
			// A CPU-only build's log has only CPU buffers: known, on the CPU.
			known = true
			if name := strings.ToUpper(m[1]); strings.HasPrefix(name, "CPU") || strings.HasSuffix(name, "_HOST") {
				continue
			}
			mib, _ := strconv.ParseFloat(m[2], 64)
			a.GPUMemoryBytes += uint64(mib * (1 << 20))
		}
	}
	if !known {
		return nil
	}
	a.Devices = used
	if len(a.Devices) == 0 {
		a.Devices = found
	}
	if a.Backend == "" || (a.LayersTotal > 0 && a.LayersOffloaded == 0 && a.GPUMemoryBytes == 0) {
		// Nothing went to a GPU, whatever was found.
		a.Backend = "cpu"
	}
	if a.Backend == "cpu" {
		a.Devices = nil
	}
	return a
}

// backendOf maps a ggml device name prefix (Vulkan, CUDA, Metal, MTL, ROCm,
// SYCL, CPU) to a backend; a CUDA device on a ROCm build is ROCm.
func backendOf(prefix, cudaKind string) string {
	switch strings.ToLower(prefix) {
	case "vulkan":
		return "vulkan"
	case "cuda":
		if cudaKind == "rocm" {
			return "rocm"
		}
		return "cuda"
	case "rocm", "hip":
		return "rocm"
	case "metal", "mtl":
		return "metal"
	case "sycl":
		return "sycl"
	}
	return "cpu"
}

// readAcceleration parses an instance's log, read up to its first 512 KiB:
// the loading lines come first.
func readAcceleration(path string) *pluginapi.Acceleration {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	raw, _ := io.ReadAll(io.LimitReader(f, 512<<10))
	return parseAcceleration(string(raw))
}
