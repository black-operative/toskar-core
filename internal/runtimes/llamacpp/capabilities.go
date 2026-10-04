package llamacpp

import (
	"context"
	"runtime"

	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// Capabilities reports llama.cpp runtime features for this host.
func Capabilities(ctx context.Context) (pluginapi.RuntimeCapabilities, error) {
	backends := []string{"cpu"}
	switch runtime.GOOS {
	case "darwin":
		backends = append(backends, "metal")
	case "linux", "windows":
		backends = append(backends, "vulkan")
	}
	return pluginapi.RuntimeCapabilities{
		Backends:          backends,
		SupportsStreaming: true,
		SupportsTools:     true,
		SupportsGPU:       len(backends) > 1,
	}, nil
}
