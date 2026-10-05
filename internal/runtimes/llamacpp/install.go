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
	"sync"

	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

const runtimeID = "llamacpp"

// Runtime implements the llama.cpp managed runtime.
type Runtime struct {
	runtimesDir string
	logsDir     string
	sup         *ProcessSupervisor

	// installMu keeps a model from starting while its files are replaced.
	installMu sync.RWMutex

	// goos and goarch pick the release build; they are the host's.
	goos, goarch string
	// releases lists llama.cpp releases, newest first.
	releases func(ctx context.Context) ([]ghRelease, error)
	// vulkan reports whether the Vulkan build would use a GPU here.
	vulkan     func(ctx context.Context) bool
	vulkanOnce sync.Once
	vulkanOK   bool
}

// New creates a llama.cpp runtime adapter.
func New(runtimesDir, logsDir string) *Runtime {
	return &Runtime{
		runtimesDir: runtimesDir,
		logsDir:     logsDir,
		goos:        runtime.GOOS,
		goarch:      runtime.GOARCH,
		releases:    githubReleases,
		vulkan:      hostVulkanProbe().usable,
	}
}

// vulkanUsable reports, once per run, whether this computer has a GPU the
// Vulkan build can use.
func (r *Runtime) vulkanUsable(ctx context.Context) bool {
	r.vulkanOnce.Do(func() {
		if r.goos == "linux" || r.goos == "windows" {
			r.vulkanOK = r.vulkan(ctx)
		}
	})
	return r.vulkanOK
}

// wantBuilds lists the release builds to install here, best first.
func (r *Runtime) wantBuilds(ctx context.Context) []string {
	return platformBuilds(r.goos, r.goarch, r.vulkanUsable(ctx))
}

// chooseAsset picks the release archive to install on this computer.
func (r *Runtime) chooseAsset(ctx context.Context) (assetInfo, error) {
	rels, err := r.releases(ctx)
	if err != nil {
		return assetInfo{}, err
	}
	return pickAsset(rels, r.wantBuilds(ctx))
}

// installDir is where Install puts llama-server and its libraries.
func (r *Runtime) installDir() string {
	return filepath.Join(r.runtimesDir, "llamacpp")
}

// UpgradeAvailable reports an install of the CPU build on a computer where
// the GPU build would run: one installed before the GPU build was chosen, or
// before a graphics driver was.
func (r *Runtime) UpgradeAvailable(ctx context.Context) bool {
	path := r.binaryPath()
	if filepath.Dir(path) != r.installDir() {
		return false
	}
	if st, err := os.Stat(path); err != nil || st.IsDir() {
		return false
	}
	if len(installedGPUBackends(r.installDir())) > 0 {
		return false
	}
	builds := r.wantBuilds(ctx)
	return len(builds) > 0 && buildHasGPU(builds[0])
}

// UpgradeBuild replaces a CPU install with the GPU build when
// UpgradeAvailable says so, and reports whether it did. It waits for a
// moment when no model is running, so a llama-server is never left without
// its files.
func (r *Runtime) UpgradeBuild(ctx context.Context) (bool, error) {
	if !r.UpgradeAvailable(ctx) {
		return false, nil
	}
	return r.install(ctx, true)
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
		det := pluginapi.RuntimeDetection{
			Installed: true,
			Path:      path,
			Version:   readVersion(path),
		}
		if r.UpgradeAvailable(ctx) {
			det.Message = "This llama.cpp runs on the CPU only, but this computer has a GPU it can use. Install llama.cpp again from the Runtimes page to get the GPU build."
		}
		return det, nil
	}
	return pluginapi.RuntimeDetection{
		Installed: false,
		Message:   "llama-server is not installed. Install from the Runtimes page or place llama-server in " + r.installDir(),
	}, nil
}

func (r *Runtime) Install(ctx context.Context, opts pluginapi.InstallOptions) error {
	_, err := r.install(ctx, false)
	return err
}

// install downloads the build chosen for this computer and swaps it in. With
// idleOnly it gives up, reporting false, when a model is running.
func (r *Runtime) install(ctx context.Context, idleOnly bool) (bool, error) {
	asset, err := r.chooseAsset(ctx)
	if err != nil {
		return false, fmt.Errorf("find llama.cpp release: %w", err)
	}
	destDir := r.installDir()
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return false, err
	}
	tmp, err := os.CreateTemp(destDir, "download-*")
	if err != nil {
		return false, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return false, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("download llama.cpp: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		_ = tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}

	// Unpack beside the install, then swap it in, so a reinstall replaces
	// every file and a failed one leaves the old install as it was.
	staging, err := os.MkdirTemp(destDir, ".staging-*")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(staging)
	lower := strings.ToLower(asset.Name)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		if err := extractZipAll(tmpPath, staging); err != nil {
			return false, err
		}
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		if err := extractTarGzAll(tmpPath, staging); err != nil {
			return false, err
		}
	default:
		return false, fmt.Errorf("unsupported release archive: %s (install llama-server manually to %s)", asset.Name, destDir)
	}
	// Some archives nest binaries one level deep; promote llama-server (+ libs) to destDir.
	found, err := findBinary(staging, filepath.Base(r.binaryPath()))
	if err != nil {
		return false, err
	}

	r.installMu.Lock()
	defer r.installMu.Unlock()
	if idleOnly {
		if running, err := r.ListRunning(ctx); err != nil || len(running) > 0 {
			return false, err
		}
	}
	if err := removeBackendLibs(destDir); err != nil {
		return false, err
	}
	if err := flattenRuntimeDir(filepath.Dir(found), destDir); err != nil {
		return false, err
	}
	if err := os.Chmod(r.binaryPath(), 0o755); err != nil {
		return false, err
	}
	return true, nil
}

// removeBackendLibs deletes the ggml backend libraries in dir. llama-server
// loads every one it finds there, so a library the new build lacks, such as
// the Vulkan one when going back to the CPU build, must not stay behind.
func removeBackendLibs(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !isBackendLib(e.Name()) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runtime) Update(ctx context.Context) error {
	return r.Install(ctx, pluginapi.InstallOptions{Force: true})
}

func (r *Runtime) Capabilities(ctx context.Context) (pluginapi.RuntimeCapabilities, error) {
	return capabilities(r.goos, installedGPUBackends(filepath.Dir(r.binaryPath()))), nil
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
	// Build is what follows "-bin-" in the name, such as ubuntu-vulkan-x64.
	Build string
}

func githubReleases(ctx context.Context) ([]ghRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/ggml-org/llama.cpp/releases?per_page=20", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "toskar-daemon")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API HTTP %d", resp.StatusCode)
	}
	var rels []ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rels); err != nil {
		return nil, err
	}
	return rels, nil
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
