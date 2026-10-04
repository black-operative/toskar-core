package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Config holds daemon runtime configuration.
type Config struct {
	DataDir          string `json:"data_dir"`
	DBPath           string `json:"db_path"`
	ModelsDir        string `json:"models_dir"`
	RuntimesDir      string `json:"runtimes_dir"`
	LogsDir          string `json:"logs_dir"`
	WebUIDir         string `json:"web_ui_dir"`
	APIHost          string `json:"api_host"`
	APIPort          int    `json:"api_port"`
	InternalHost     string `json:"internal_host"`
	InternalPort     int    `json:"internal_port"`
	LANAPIEnabled    bool   `json:"lan_api_enabled"`
	WebUIEnabled     bool   `json:"web_ui_enabled"`
	DiscoveryEnabled bool   `json:"discovery_enabled"`
	NodeName         string `json:"node_name"`
	NodeID           string `json:"node_id,omitempty"`
	// AdvertiseHost is an optional reachable hostname/IP for Bifrost peers (e.g. Docker DNS name).
	AdvertiseHost string `json:"advertise_host,omitempty"`
	// StaticPeers are Bifrost host:port addresses to probe when mDNS is unavailable.
	StaticPeers []string `json:"static_peers,omitempty"`
	// Map services for places and routes; empty uses OpenStreetMap's public
	// Nominatim, Overpass, and routing.openstreetmap.de.
	PlacesGeocoderURL string `json:"places_geocoder_url,omitempty"`
	PlacesOverpassURL string `json:"places_overpass_url,omitempty"`
	PlacesRouterURL   string `json:"places_router_url,omitempty"`
	// Community model ratings (#37): the ratings service, and the public
	// summary used when it cannot be reached. Empty uses Yeix's.
	RatingsURL string `json:"ratings_url,omitempty"`
	// ExternalOpenAIURL is an OpenAI-compatible server whose models can be
	// chosen for a chat (#111); its API key is in the secrets folder.
	ExternalOpenAIURL string `json:"external_openai_url,omitempty"`
	RatingsSummaryURL string `json:"ratings_summary_url,omitempty"`
}

// Manager loads and persists configuration.
type Manager struct {
	mu   sync.RWMutex
	cfg  Config
	path string
}

// NewManager creates a config manager rooted at dataDir.
func NewManager(dataDir string) (*Manager, error) {
	if dataDir == "" {
		dataDir = DefaultDataDir()
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	path := filepath.Join(dataDir, "config.json")
	m := &Manager{path: path, cfg: DefaultConfig()}
	m.cfg.DataDir = dataDir
	m.cfg.DBPath = DBPathIn(dataDir)
	m.cfg.ModelsDir = filepath.Join(dataDir, "models")
	m.cfg.RuntimesDir = filepath.Join(dataDir, "runtimes")
	m.cfg.LogsDir = filepath.Join(dataDir, "logs")

	if _, err := os.Stat(path); err == nil {
		if err := m.load(); err != nil {
			return nil, err
		}
	} else {
		if err := m.Save(); err != nil {
			return nil, err
		}
	}

	for _, dir := range []string{m.cfg.ModelsDir, m.cfg.RuntimesDir, m.cfg.LogsDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create dir %s: %w", dir, err)
		}
	}
	return m, nil
}

// Get returns a copy of the current config.
func (m *Manager) Get() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

// Update applies a mutator and persists.
func (m *Manager) Update(fn func(*Config)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	fn(&m.cfg)
	return m.saveLocked()
}

// Save persists the current config.
func (m *Manager) Save() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saveLocked()
}

func (m *Manager) load() error {
	data, err := os.ReadFile(m.path)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	// Preserve computed paths if empty in file. A file without data_dir
	// belongs to the directory it was read from, not the default one, so
	// --data-dir never falls back to the user's real data.
	defaults := DefaultConfig()
	if cfg.DataDir == "" {
		cfg.DataDir = filepath.Dir(m.path)
	}
	if cfg.DBPath == "" {
		cfg.DBPath = DBPathIn(cfg.DataDir)
	}
	if cfg.ModelsDir == "" {
		cfg.ModelsDir = filepath.Join(cfg.DataDir, "models")
	}
	if cfg.RuntimesDir == "" {
		cfg.RuntimesDir = filepath.Join(cfg.DataDir, "runtimes")
	}
	if cfg.LogsDir == "" {
		cfg.LogsDir = filepath.Join(cfg.DataDir, "logs")
	}
	if cfg.APIHost == "" {
		cfg.APIHost = defaults.APIHost
	}
	if cfg.APIPort == 0 {
		cfg.APIPort = defaults.APIPort
	}
	if cfg.InternalHost == "" {
		cfg.InternalHost = defaults.InternalHost
	}
	if cfg.InternalPort == 0 {
		cfg.InternalPort = defaults.InternalPort
	}
	if cfg.NodeName == "" {
		cfg.NodeName = defaults.NodeName
	}
	m.cfg = cfg
	return nil
}

func (m *Manager) saveLocked() error {
	data, err := json.MarshalIndent(m.cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := os.Rename(tmp, m.path); err != nil {
		return fmt.Errorf("finalize config: %w", err)
	}
	return nil
}

// APIAddr returns host:port for the public API.
func (c Config) APIAddr() string {
	return fmt.Sprintf("%s:%d", c.APIHost, c.APIPort)
}

// InternalAddr returns host:port for the Bifrost node protocol.
func (c Config) InternalAddr() string {
	return fmt.Sprintf("%s:%d", c.InternalHost, c.InternalPort)
}
