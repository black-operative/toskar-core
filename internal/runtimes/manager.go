package runtimes

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sync"

	"github.com/yeixio/toskar-core/internal/events"
)

// Manager coordinates runtime adapters and persistence.
type Manager struct {
	registry *Registry
	db       *sql.DB
	bus      *events.Bus
	gen      Generator
	// OnRemote, when set, hears each chat sent to a server that is not on
	// this computer, such as an external OpenAI-compatible server (§63).
	OnRemote func(ctx context.Context, host string)
	// OnStart, when set, hears each model start on this computer and
	// whether it failed, for community ratings' runtime observations.
	OnStart func(ctx context.Context, modelID string, err error)

	mu sync.RWMutex
}

// NewManager creates a runtime manager.
func NewManager(registry *Registry, db *sql.DB, bus *events.Bus, gen Generator) *Manager {
	return &Manager{registry: registry, db: db, bus: bus, gen: gen}
}

// SetGenerator sets the chat generator (typically llamacpp client).
func (m *Manager) SetGenerator(gen Generator) {
	m.mu.Lock()
	m.gen = gen
	m.mu.Unlock()
}

// Generator returns the configured chat generator.
func (m *Manager) Generator() Generator {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.gen
}

// Registry returns the underlying registry.
func (m *Manager) Registry() *Registry { return m.registry }

// List returns all registered runtimes with detection info.
func (m *Manager) List(ctx context.Context) ([]RuntimeInfo, error) {
	var out []RuntimeInfo
	for _, rt := range m.registry.List() {
		det, err := rt.Detect(ctx)
		var status string
		switch {
		case err != nil:
			status = "error"
		case det.Installed:
			status = "installed"
		default:
			status = "not_installed"
		}
		out = append(out, RuntimeInfo{
			ID:          rt.ID(),
			DisplayName: rt.DisplayName(),
			Detection:   det,
			Status:      status,
		})
		_ = m.persistRuntime(ctx, rt.ID(), rt.DisplayName(), det)
	}
	return out, nil
}

// RuntimeInfo is the API view of a runtime.
type RuntimeInfo struct {
	ID          string           `json:"id"`
	DisplayName string           `json:"display_name"`
	Detection   RuntimeDetection `json:"detection"`
	Status      string           `json:"status"`
}

// Upgradable is a runtime whose install can be outgrown by the computer, such
// as llama.cpp's CPU build on a computer with a GPU it could use.
type Upgradable interface {
	UpgradeAvailable(ctx context.Context) bool
	// UpgradeBuild installs the better build when no model is running, and
	// reports whether it did.
	UpgradeBuild(ctx context.Context) (bool, error)
}

// Install installs a runtime by ID. Already-installed runtimes are a no-op,
// unless a better build fits this computer.
func (m *Manager) Install(ctx context.Context, id string, opts InstallOptions) error {
	rt, err := m.registry.Get(id)
	if err != nil {
		return err
	}
	det, err := rt.Detect(ctx)
	if err == nil && det.Installed && !opts.Force {
		if u, ok := rt.(Upgradable); !ok || !u.UpgradeAvailable(ctx) {
			return nil
		}
	}
	return rt.Install(ctx, opts)
}

// UpgradeBuilds installs a better build of each runtime that has one, such
// as llama.cpp's GPU build in place of its CPU one, and returns the IDs it
// upgraded. A runtime with a model running is left for the next start.
func (m *Manager) UpgradeBuilds(ctx context.Context) ([]string, error) {
	var done []string
	var errs []error
	for _, rt := range m.registry.List() {
		u, ok := rt.(Upgradable)
		if !ok {
			continue
		}
		did, err := u.UpgradeBuild(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", rt.ID(), err))
			continue
		}
		if did {
			done = append(done, rt.ID())
		}
	}
	return done, errors.Join(errs...)
}

// Get returns a runtime adapter.
func (m *Manager) Get(id string) (Runtime, error) {
	return m.registry.Get(id)
}

func (m *Manager) persistRuntime(ctx context.Context, id, name string, det RuntimeDetection) error {
	meta, _ := json.Marshal(det)
	status := "not_installed"
	if det.Installed {
		status = "installed"
	}
	_, err := m.db.ExecContext(ctx, `
		INSERT INTO runtimes (id, display_name, version, status, install_path, meta_json, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(id) DO UPDATE SET
			display_name=excluded.display_name,
			version=excluded.version,
			status=excluded.status,
			install_path=excluded.install_path,
			meta_json=excluded.meta_json,
			updated_at=datetime('now')`,
		id, name, det.Version, status, det.Path, string(meta))
	return err
}

// StartModel loads a model on the given runtime.
func (m *Manager) StartModel(ctx context.Context, runtimeID string, cfg ModelStartConfig) (RunningModel, error) {
	rt, err := m.registry.Get(runtimeID)
	if err != nil {
		return RunningModel{}, err
	}
	running, err := rt.StartModel(ctx, cfg)
	if m.OnStart != nil {
		m.OnStart(ctx, cfg.ModelID, err)
	}
	if err != nil {
		return RunningModel{}, err
	}
	m.bus.Publish(events.New(events.ModelLoadCompleted, map[string]any{
		"model_id":   cfg.ModelID,
		"runtime_id": runtimeID,
		"endpoint":   running.Endpoint,
	}))
	return running, nil
}

// StopModel unloads a running model.
func (m *Manager) StopModel(ctx context.Context, runtimeID, instanceID string) error {
	rt, err := m.registry.Get(runtimeID)
	if err != nil {
		return err
	}
	return rt.StopModel(ctx, instanceID)
}

// ListRunning returns loaded model instances for a runtime.
func (m *Manager) ListRunning(ctx context.Context, runtimeID string) ([]RunningModel, error) {
	rt, err := m.registry.Get(runtimeID)
	if err != nil {
		return nil, err
	}
	return rt.ListRunning(ctx)
}

// StopAll stops every running model instance across all runtimes.
// Used during daemon shutdown so child llama-server processes do not linger.
func (m *Manager) StopAll(ctx context.Context) {
	for _, rt := range m.registry.List() {
		running, err := rt.ListRunning(ctx)
		if err != nil || len(running) == 0 {
			continue
		}
		var wg sync.WaitGroup
		for _, inst := range running {
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				_ = rt.StopModel(ctx, id)
			}(inst.ID)
		}
		wg.Wait()
	}
}

// Chat streams generation through the configured generator.
func (m *Manager) Chat(ctx context.Context, req ChatRequest) (<-chan ChatChunk, error) {
	gen := m.Generator()
	if gen == nil {
		return nil, fmt.Errorf("no chat generator configured")
	}
	if m.OnRemote != nil {
		if host, remote := remoteHost(req.ModelEndpoint); remote {
			m.OnRemote(ctx, host)
		}
	}
	return gen.Chat(ctx, req)
}

// remoteHost reports the host of an endpoint that is not on this computer.
func remoteHost(endpoint string) (string, bool) {
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return "", false
	}
	host := u.Hostname()
	if host == "localhost" {
		return "", false
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return "", false
	}
	return host, true
}
