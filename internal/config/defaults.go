package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	DefaultAPIPort      = 7331
	DefaultInternalPort = 7332
	DefaultBindLoopback = "127.0.0.1"
	ServiceType         = "_localai._tcp"
	ServiceDomain       = "local."
)

// DefaultDataDir returns the OS-appropriate application data directory:
// the Toskar folder, or the Yggdrasil folder an install from before the
// rename already has (#237). Nothing is moved: models alone can be many
// gigabytes, and App Sandbox, Homebrew, and launchd setups point at the
// old folder.
func DefaultDataDir() string {
	return chooseDataDir(dataDirNamed("Toskar"), dataDirNamed("Yggdrasil"))
}

// chooseDataDir picks the new folder once it holds a config.json, then an
// existing old folder, and otherwise the new folder. A new folder that
// something created empty does not hide the old one's data.
func chooseDataDir(newDir, oldDir string) string {
	if _, err := os.Stat(filepath.Join(newDir, "config.json")); err == nil {
		return newDir
	}
	if info, err := os.Stat(oldDir); err == nil && info.IsDir() {
		return oldDir
	}
	return newDir
}

// dataDirNamed is the OS's data folder for an app called name ("Toskar"),
// lowercased on Linux.
func dataDirNamed(name string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", name)
	case "windows":
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			return filepath.Join(local, name)
		}
		return filepath.Join(home, "AppData", "Local", name)
	default:
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			return filepath.Join(xdg, strings.ToLower(name))
		}
		return filepath.Join(home, ".local", "share", strings.ToLower(name))
	}
}

// DBPathIn is the database in dataDir: toskar.db, or yggdrasil.db when a
// data directory from before the rename has one (#237).
func DBPathIn(dataDir string) string {
	newDB := filepath.Join(dataDir, "toskar.db")
	if _, err := os.Stat(newDB); err == nil {
		return newDB
	}
	oldDB := filepath.Join(dataDir, "yggdrasil.db")
	if _, err := os.Stat(oldDB); err == nil {
		return oldDB
	}
	return newDB
}

// DefaultConfig returns a Config with safe local-first defaults.
func DefaultConfig() Config {
	dataDir := DefaultDataDir()
	return Config{
		DataDir:          dataDir,
		DBPath:           DBPathIn(dataDir),
		ModelsDir:        filepath.Join(dataDir, "models"),
		RuntimesDir:      filepath.Join(dataDir, "runtimes"),
		LogsDir:          filepath.Join(dataDir, "logs"),
		WebUIDir:         "",
		APIHost:          DefaultBindLoopback,
		APIPort:          DefaultAPIPort,
		InternalHost:     DefaultBindLoopback,
		InternalPort:     DefaultInternalPort,
		LANAPIEnabled:    false,
		WebUIEnabled:     true,
		DiscoveryEnabled: true,
		NodeName:         hostnameOr("Toskar-Node"),
	}
}

func hostnameOr(fallback string) string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return fallback
	}
	return h
}
