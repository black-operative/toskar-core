// Package appinfo exposes the data-directory and API address the desktop shell
// needs. The shell is a separate module, so it cannot import internal/config.
package appinfo

import "github.com/yeixio/toskar-core/internal/config"

// DefaultDataDir is the OS data directory the daemon uses when started without flags.
func DefaultDataDir() string {
	return config.DefaultDataDir()
}

// DefaultAPIAddr is the loopback address the daemon listens on by default.
func DefaultAPIAddr() string {
	return config.DefaultConfig().APIAddr()
}
