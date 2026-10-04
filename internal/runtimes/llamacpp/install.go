package llamacpp

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/yeixio/yggdrasil-core/pkg/pluginapi"
)

const runtimeID = "llamacpp"

// Runtime implements the llama.cpp managed runtime.
type Runtime struct {
	runtimesDir string
	logsDir     string
	sup         *ProcessSupervisor
}

// New creates a llama.cpp runtime adapter.
func New(runtimesDir, logsDir string) *Runtime {
	return &Runtime{runtimesDir: runtimesDir, logsDir: logsDir}
}

func (r *Runtime) ID() string          { return runtimeID }
func (r *Runtime) DisplayName() string { return "llama.cpp (llama-server)" }

func (r *Runtime) binaryPath() string {
	// The Mac App Store sandbox rejects fork/exec of a binary downloaded into
	// the app container. A store build ships a signed llama-server beside the daemon.
	if bundled := bundledLlamaServer(); bundled != "" {
		return bundled
	}
	name := "llama-server"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(r.runtimesDir, "llamacpp", name)
}

// Tool returns a llama.cpp program installed beside llama-server, such as
// llama-export-lora. Builds that ship only llama-server, like the Mac App
// Store build, do not have one.
func (r *Runtime) Tool(name string) (string, error) {
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	server := r.binaryPath()
	if resolved, err := filepath.EvalSymlinks(server); err == nil {
		server = resolved
	}
	path := filepath.Join(filepath.Dir(server), name)
	if st, err := os.Stat(path); err != nil || st.IsDir() {
		return "", fmt.Errorf("this llama.cpp install has no %s. Reinstall llama.cpp from the Runtimes page", name)
	}
	return path, nil
}

func bundledLlamaServer() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return bundledLlamaServerBeside(exe)
}

func bundledLlamaServerBeside(exe string) string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	candidate := filepath.Join(filepath.Dir(exe), "llamacpp", "llama-server")
	st, err := os.Stat(candidate)
	if err != nil || st.IsDir() {
		return ""
	}
	return candidate
}

func (r *Runtime) Detect(ctx context.Context) (pluginapi.RuntimeDetection, error) {
	path := r.binaryPath()
	if st, err := os.Stat(path); err == nil && !st.IsDir() {
		return pluginapi.RuntimeDetection{
			Installed: true,
			Path:      path,
			Version:   readVersion(path),
		}, nil
	}
	return pluginapi.RuntimeDetection{
		Installed: false,
		Message:   "llama-server is not installed. Install from the Runtimes page or place llama-server in " + filepath.Join(r.runtimesDir, "llamacpp"),
	}, nil
}

func (r *Runtime) Install(ctx context.Context, opts pluginapi.InstallOptions) error {
	asset, err := latestAsset(ctx)
	if err != nil {
		return fmt.Errorf("find llama.cpp release: %w", err)
	}
	destDir := filepath.Join(r.runtimesDir, "llamacpp")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(destDir, "download-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download llama.cpp: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	lower := strings.ToLower(asset.Name)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		if err := extractZipAll(tmpPath, destDir); err != nil {
			return err
		}
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		if err := extractTarGzAll(tmpPath, destDir); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported release archive: %s (install llama-server manually to %s)", asset.Name, destDir)
	}

	binPath := r.binaryPath()
	if _, err := os.Stat(binPath); err != nil {
		// Some archives nest binaries one level deep; promote llama-server (+ libs) to destDir.
		found, err := findBinary(destDir, filepath.Base(binPath))
		if err != nil {
			return err
		}
		if err := flattenRuntimeDir(filepath.Dir(found), destDir); err != nil {
			return err
		}
	}
	return os.Chmod(r.binaryPath(), 0o755)
}

func (r *Runtime) Update(ctx context.Context) error {
	return r.Install(ctx, pluginapi.InstallOptions{Force: true})
}

func (r *Runtime) Capabilities(ctx context.Context) (pluginapi.RuntimeCapabilities, error) {
	return Capabilities(ctx)
}

type ghRelease struct {
	Assets []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

type assetInfo struct {
	Name string
	URL  string
}

func latestAsset(ctx context.Context) (assetInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/ggml-org/llama.cpp/releases?per_page=20", nil)
	if err != nil {
		return assetInfo{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "toskar-daemon")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return assetInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return assetInfo{}, fmt.Errorf("GitHub API HTTP %d", resp.StatusCode)
	}
	var rels []ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rels); err != nil {
		return assetInfo{}, err
	}
	want := platformAssetSuffix()
	for _, rel := range rels {
		for _, a := range rel.Assets {
			name := strings.ToLower(a.Name)
			if strings.Contains(name, want) && (strings.HasSuffix(name, ".tar.gz") || strings.HasSuffix(name, ".tgz") || strings.HasSuffix(name, ".zip")) {
				// Prefer the main llama-b* bin archives over cuda runtime packs.
				if strings.Contains(name, "cudart") {
					continue
				}
				return assetInfo{Name: a.Name, URL: a.BrowserDownloadURL}, nil
			}
		}
	}
	return assetInfo{}, fmt.Errorf("no release asset matching %q found; install llama-server manually", want)
}

func platformAssetSuffix() string {
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	switch {
	case goos == "darwin" && goarch == "arm64":
		return "bin-macos-arm64"
	case goos == "darwin" && goarch == "amd64":
		return "bin-macos-x64"
	case goos == "linux" && goarch == "amd64":
		return "bin-ubuntu-x64"
	case goos == "linux" && goarch == "arm64":
		return "bin-ubuntu-arm64"
	case goos == "windows" && goarch == "amd64":
		return "bin-win-cpu-x64"
	default:
		return goos + "-" + goarch
	}
}

func extractZipAll(archivePath, destDir string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		target, err := safeJoin(destDir, f.Name)
		if err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		if err := writeFile(target, rc, f.Mode()); err != nil {
			_ = rc.Close()
			return err
		}
		_ = rc.Close()
	}
	return nil
}

func extractTarGzAll(archivePath, destDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		target, err := safeJoin(destDir, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, 'A': // tar.TypeRegA, still a regular file
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(hdr.Mode)
			if mode == 0 {
				mode = 0o644
			}
			if err := writeFile(target, tr, mode); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			_ = os.RemoveAll(target)
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return fmt.Errorf("symlink %s -> %s: %w", target, hdr.Linkname, err)
			}
		case tar.TypeLink:
			linkTarget, err := safeJoin(destDir, hdr.Linkname)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			_ = os.RemoveAll(target)
			if err := os.Link(linkTarget, target); err != nil {
				return fmt.Errorf("hardlink %s -> %s: %w", target, linkTarget, err)
			}
		}
	}
	return nil
}

func safeJoin(base, name string) (string, error) {
	clean := filepath.Clean("/" + name)
	clean = strings.TrimPrefix(clean, "/")
	target := filepath.Join(base, clean)
	if !strings.HasPrefix(target, filepath.Clean(base)+string(os.PathSeparator)) && target != filepath.Clean(base) {
		return "", fmt.Errorf("invalid archive path: %s", name)
	}
	return target, nil
}

func writeFile(path string, r io.Reader, mode os.FileMode) error {
	out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, r)
	return err
}

func findBinary(root, binName string) (string, error) {
	var found string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if info.Name() == binName {
			found = path
			return io.EOF
		}
		return nil
	})
	if found == "" {
		return "", fmt.Errorf("%s not found after extract", binName)
	}
	return found, nil
}

func flattenRuntimeDir(srcDir, destDir string) error {
	if filepath.Clean(srcDir) == filepath.Clean(destDir) {
		return nil
	}
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		from := filepath.Join(srcDir, e.Name())
		to := filepath.Join(destDir, e.Name())
		_ = os.RemoveAll(to)
		if err := os.Rename(from, to); err != nil {
			return err
		}
	}
	return nil
}

func readVersion(path string) string {
	// Best-effort; full version probing would exec --version.
	if st, err := os.Stat(path); err == nil {
		return st.ModTime().Format("2006-01-02")
	}
	return ""
}
