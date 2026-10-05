package llamacpp

import (
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// capabilities reports llama.cpp runtime features for an install on goos
// whose folder holds the libraries for the gpu backends. macOS builds always
// have Metal; elsewhere only the GPU libraries actually installed count, so
// the CPU build never claims a GPU.
func capabilities(goos string, gpu []string) pluginapi.RuntimeCapabilities {
	backends := []string{"cpu"}
	if goos == "darwin" {
		backends = append(backends, "metal")
	} else {
		backends = append(backends, gpu...)
	}
	return pluginapi.RuntimeCapabilities{
		Backends:          backends,
		SupportsStreaming: true,
		SupportsTools:     true,
		SupportsGPU:       len(backends) > 1,
	}
}
