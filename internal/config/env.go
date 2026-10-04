package config

import (
	"os"
	"strconv"
	"strings"
)

// Env reads a setting from the environment: TOSKAR_<name>, or the name
// from before the rename, YGGDRASIL_<name>, which existing Docker, systemd,
// launchd, and MCP configurations set (#237). The new name wins when both
// are set.
func Env(name string) string {
	if v := os.Getenv("TOSKAR_" + name); v != "" {
		return v
	}
	return os.Getenv("YGGDRASIL_" + name)
}

// ApplyEnvOverrides mutates cfg from TOSKAR_* (or YGGDRASIL_*) environment
// variables. Used for Docker / CI cluster nodes with fixed identities and
// static peers.
func ApplyEnvOverrides(cfg *Config) {
	if cfg == nil {
		return
	}
	if v := Env("NODE_ID"); v != "" {
		cfg.NodeID = v
	}
	if v := Env("NODE_NAME"); v != "" {
		cfg.NodeName = v
	}
	if v := Env("API_HOST"); v != "" {
		cfg.APIHost = v
	}
	if v := Env("API_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.APIPort = n
		}
	}
	if v := Env("INTERNAL_HOST"); v != "" {
		cfg.InternalHost = v
	}
	if v := Env("INTERNAL_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.InternalPort = n
		}
	}
	if v := Env("ADVERTISE_HOST"); v != "" {
		cfg.AdvertiseHost = v
	}
	if v := Env("DISCOVERY_ENABLED"); v != "" {
		cfg.DiscoveryEnabled = parseBool(v)
	}
	if v := Env("STATIC_PEERS"); v != "" {
		cfg.StaticPeers = splitCSV(v)
	}
	if v := Env("WEB_UI_DIR"); v != "" {
		cfg.WebUIDir = v
	}
	if v := Env("WEB_UI_ENABLED"); v != "" {
		cfg.WebUIEnabled = parseBool(v)
	}
}

// EnvTruthy reports whether the setting Env(name) is a truthy flag.
func EnvTruthy(name string) bool {
	return parseBool(Env(name))
}

func parseBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func splitCSV(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
