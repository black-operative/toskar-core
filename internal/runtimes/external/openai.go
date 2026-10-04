package external

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

const runtimeID = "external-openai"

// Config holds external OpenAI-compatible endpoint settings.
type Config struct {
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key,omitempty"`
}

// Runtime adapts a remote OpenAI-compatible server. Its models are only
// used when someone chooses one; Auto never does.
type Runtime struct {
	mu     sync.RWMutex
	cfg    Config
	client *http.Client
}

// New creates an external OpenAI runtime adapter.
func New(cfg Config) *Runtime {
	return &Runtime{cfg: cfg}
}

// Config is the current base URL and API key.
func (r *Runtime) Config() Config {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cfg
}

// SetConfig changes the base URL and API key.
func (r *Runtime) SetConfig(cfg Config) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cfg = cfg
}

func (r *Runtime) ID() string          { return runtimeID }
func (r *Runtime) DisplayName() string { return "External OpenAI-compatible" }

func (r *Runtime) Detect(ctx context.Context) (pluginapi.RuntimeDetection, error) {
	cfg := r.Config()
	if cfg.BaseURL == "" {
		return pluginapi.RuntimeDetection{
			Installed: false,
			Message:   "Set external_openai_url in the configuration, or the external server in Settings, to use an OpenAI-compatible server",
		}, nil
	}
	return pluginapi.RuntimeDetection{
		Installed: true,
		Path:      cfg.BaseURL,
		Version:   "remote",
	}, nil
}

func (r *Runtime) Install(ctx context.Context, opts pluginapi.InstallOptions) error {
	return fmt.Errorf("external runtime cannot be installed; configure base_url instead")
}

func (r *Runtime) Update(ctx context.Context) error {
	return nil
}

func (r *Runtime) Capabilities(ctx context.Context) (pluginapi.RuntimeCapabilities, error) {
	return pluginapi.RuntimeCapabilities{
		Backends:          []string{"remote"},
		SupportsStreaming: true,
		SupportsTools:     true,
		SupportsGPU:       false,
	}, nil
}

func (r *Runtime) StartModel(ctx context.Context, cfg pluginapi.ModelStartConfig) (pluginapi.RunningModel, error) {
	det, err := r.Detect(ctx)
	if err != nil {
		return pluginapi.RunningModel{}, err
	}
	if !det.Installed {
		return pluginapi.RunningModel{}, fmt.Errorf("external runtime not configured")
	}
	return pluginapi.RunningModel{
		ID:        cfg.ModelID,
		ModelID:   cfg.ModelID,
		Endpoint:  r.Config().BaseURL,
		Status:    "remote",
		RuntimeID: runtimeID,
	}, nil
}

func (r *Runtime) StopModel(ctx context.Context, id string) error { return nil }

func (r *Runtime) ListRunning(ctx context.Context) ([]pluginapi.RunningModel, error) {
	return nil, nil
}

func (r *Runtime) Health(ctx context.Context) error {
	det, err := r.Detect(ctx)
	if err != nil {
		return err
	}
	if !det.Installed {
		return fmt.Errorf("external runtime not configured")
	}
	return nil
}
