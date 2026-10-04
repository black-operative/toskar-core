package diagnostics

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yeixio/yggdrasil-core/internal/config"
	"github.com/yeixio/yggdrasil-core/internal/version"
)

// Options controls diagnostic bundle contents.
type Options struct {
	IncludeConversations bool
	Config               config.Config
	HardwareJSON         []byte
	ExtraNotes           string
}

// WriteBundle creates a zip diagnostic bundle at destPath.
// Secrets, private keys, and API key material are never included.
func WriteBundle(destPath string, opts Options) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

	meta := map[string]any{
		"product":    "Yggdrasil",
		"version":    version.Version,
		"commit":     version.Commit,
		"build_date": version.BuildDate,
		"created_at": time.Now().UTC().Format(time.RFC3339),
		"node_id":    opts.Config.NodeID,
		"node_name":  opts.Config.NodeName,
		"os_hints": map[string]string{
			"data_dir":     opts.Config.DataDir,
			"models_dir":   opts.Config.ModelsDir,
			"runtimes_dir": opts.Config.RuntimesDir,
			"logs_dir":     opts.Config.LogsDir,
		},
		"notes": opts.ExtraNotes,
	}
	if err := writeJSON(zw, "meta.json", meta); err != nil {
		return err
	}

	safeCfg := map[string]any{
		"api_host":          opts.Config.APIHost,
		"api_port":          opts.Config.APIPort,
		"internal_host":     opts.Config.InternalHost,
		"internal_port":     opts.Config.InternalPort,
		"lan_api_enabled":   opts.Config.LANAPIEnabled,
		"web_ui_enabled":    opts.Config.WebUIEnabled,
		"discovery_enabled": opts.Config.DiscoveryEnabled,
		"node_name":         opts.Config.NodeName,
		"node_id":           opts.Config.NodeID,
		// Paths only — no secrets.
		"data_dir":     opts.Config.DataDir,
		"models_dir":   opts.Config.ModelsDir,
		"runtimes_dir": opts.Config.RuntimesDir,
		"logs_dir":     opts.Config.LogsDir,
	}
	if err := writeJSON(zw, "config_redacted.json", safeCfg); err != nil {
		return err
	}

	if len(opts.HardwareJSON) > 0 {
		if err := writeBytes(zw, "hardware.json", opts.HardwareJSON); err != nil {
			return err
		}
	}

	_ = addDirFiltered(zw, opts.Config.LogsDir, "logs", func(name string) bool {
		lower := strings.ToLower(name)
		return !strings.Contains(lower, "secret") && !strings.Contains(lower, "key") && !strings.HasSuffix(lower, ".pem")
	})

	if err := writeProfiles(zw); err != nil {
		return err
	}

	if opts.IncludeConversations {
		dbPath := opts.Config.DBPath
		if st, err := os.Stat(dbPath); err == nil && st.Size() < 32<<20 {
			_ = addFile(zw, dbPath, "optional/"+filepath.Base(dbPath))
		}
	}

	readme := `Toskar diagnostic bundle
===========================
This archive excludes API secrets and private keys by default.
Conversation/task database is included only when explicitly requested.
runtime.json and profiles/ describe the daemon's memory and goroutines
(function names, counts, and sizes; no prompts or files). Read heap.pb.gz
with: go tool pprof profiles/heap.pb.gz
`
	return writeBytes(zw, "README.txt", []byte(readme))
}

func writeJSON(zw *zip.Writer, name string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeBytes(zw, name, data)
}

func writeBytes(zw *zip.Writer, name string, data []byte) error {
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

func addFile(zw *zip.Writer, src, name string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, in)
	return err
}

func addDirFiltered(zw *zip.Writer, dir, prefix string, keep func(string) bool) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if e.IsDir() || !keep(e.Name()) {
			continue
		}
		path := filepath.Join(dir, e.Name())
		st, err := os.Stat(path)
		if err != nil || st.Size() > 5<<20 {
			continue
		}
		_ = addFile(zw, path, filepath.Join(prefix, e.Name()))
	}
	return nil
}

// DefaultBundlePath returns a timestamped path under the logs directory.
func DefaultBundlePath(cfg config.Config) string {
	name := fmt.Sprintf("yggdrasil-diagnostics-%s.zip", time.Now().UTC().Format("20060102-150405"))
	return filepath.Join(cfg.LogsDir, name)
}
