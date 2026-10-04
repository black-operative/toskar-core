// Package pyenv manages isolated Python environments for components that
// need Python, such as model trainers. It downloads a pinned uv binary into
// the runtimes directory, lets uv install a private Python, and creates one
// virtual environment per component. Nothing is installed system-wide.
package pyenv

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/yeixio/yggdrasil-core/internal/config"
	"github.com/yeixio/yggdrasil-core/pkg/contracts"
)

// UVVersion is the uv release the daemon installs.
const UVVersion = "0.12.21"

// PythonVersion is the interpreter uv installs for managed environments.
const PythonVersion = "3.12"

// Spec describes one environment.
type Spec struct {
	// Name is the environment directory, such as "trainer-mlx".
	Name string
	// Requirements are pinned pip requirement strings.
	Requirements []string
	// Pinned installs exactly Requirements and nothing they depend on, so
	// the list must be complete. It lets an environment swap a dependency,
	// such as a headless build of a package for the full one.
	Pinned bool
	// InstallArgs are extra uv pip install flags, such as
	// --torch-backend=auto, which picks the PyTorch build for the computer's
	// GPU driver.
	InstallArgs []string
}

// Status reports whether an environment is ready.
type Status struct {
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	Python    string `json:"python,omitempty"`
	// Bundled is true when the environment ships with the app instead of
	// being installed by the daemon.
	Bundled bool `json:"bundled,omitempty"`
	// Stale is true when the environment exists but was built from different
	// requirements, for example after an upgrade.
	Stale bool `json:"stale,omitempty"`
}

// Progress reports install steps.
type Progress func(step, detail string)

// ErrSandboxed means the daemon runs in the macOS App Sandbox, which refuses
// to run programs downloaded into the app's container, so environments can
// only come bundled with the app.
var ErrSandboxed = contracts.NewError("SANDBOXED", nil, errors.New("this copy of Yggdrasil runs in the macOS App Sandbox, which cannot run a Python environment it downloads"))

// Sandboxed reports whether the daemon runs in the macOS App Sandbox. macOS
// sets APP_SANDBOX_CONTAINER_ID in every sandboxed process;
// TOSKAR_SANDBOXED=1 simulates it for testing.
func Sandboxed() bool {
	return os.Getenv("APP_SANDBOX_CONTAINER_ID") != "" || config.Env("SANDBOXED") == "1"
}

// Manager owns the uv binary and the environments under Root.
type Manager struct {
	Root string
	// Bundled is a directory of environments shipped with the app, one
	// folder per environment name, each with the requirements marker. A
	// current bundled environment is used instead of installing one.
	Bundled string
	// Sandboxed refuses to download and install environments.
	Sandboxed bool
	// ReleaseBase is where uv release assets are downloaded. Tests replace it.
	ReleaseBase string
	HTTP        *http.Client

	mu sync.Mutex
}

// New returns a manager rooted at dir (normally <runtimes>/python). It uses
// environments bundled in a "python" folder beside the daemon, and does not
// install any when the daemon is sandboxed.
func New(dir string) *Manager {
	return &Manager{
		Root:        dir,
		Bundled:     bundledDir(),
		Sandboxed:   Sandboxed(),
		ReleaseBase: "https://github.com/astral-sh/uv/releases/download/" + UVVersion,
		HTTP:        http.DefaultClient,
	}
}

func (m *Manager) uvPath() string {
	name := "uv"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(m.Root, "uv", UVVersion, name)
}

func (m *Manager) envDir(name string) string { return filepath.Join(m.Root, "envs", name) }

// bundledDir is the "python" folder beside the daemon executable, where a
// packaged app (such as the Mac App Store build) places signed environments.
func bundledDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Join(filepath.Dir(exe), "python")
}

func interpreter(env string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(env, "Scripts", "python.exe")
	}
	return filepath.Join(env, "bin", "python")
}

// PythonPath is the interpreter inside an environment the manager installs.
func (m *Manager) PythonPath(name string) string { return interpreter(m.envDir(name)) }

func (m *Manager) markerPath(name string) string {
	return filepath.Join(m.envDir(name), ".yggdrasil-requirements")
}

func requirementsKey(spec Spec) string {
	reqs := append([]string(nil), spec.Requirements...)
	sort.Strings(reqs)
	key := "python " + PythonVersion + "\n"
	if spec.Pinned {
		key += "pinned\n"
	}
	if len(spec.InstallArgs) > 0 {
		key += "args " + strings.Join(spec.InstallArgs, " ") + "\n"
	}
	return key + strings.Join(reqs, "\n") + "\n"
}

// RequirementsKey is the content of an environment's requirements marker,
// ".yggdrasil-requirements". A bundled environment must carry it, so the
// daemon can tell it was built from the requirements it expects.
func RequirementsKey(spec Spec) string { return requirementsKey(spec) }

// Status reports whether spec's environment is installed and current. A
// current bundled environment comes first.
func (m *Manager) Status(spec Spec) Status {
	if m.Bundled != "" {
		if st := envStatus(spec, filepath.Join(m.Bundled, spec.Name)); st.Installed {
			st.Bundled = true
			return st
		}
	}
	return envStatus(spec, m.envDir(spec.Name))
}

func envStatus(spec Spec, dir string) Status {
	st := Status{Name: spec.Name}
	py := interpreter(dir)
	if _, err := os.Stat(py); err != nil {
		return st
	}
	marker, err := os.ReadFile(filepath.Join(dir, ".yggdrasil-requirements"))
	if err != nil || string(marker) != requirementsKey(spec) {
		st.Stale = true
		return st
	}
	st.Installed = true
	st.Python = py
	return st
}

// Unavailable says why spec's environment cannot be used or installed here,
// or returns "" when it can.
func (m *Manager) Unavailable(spec Spec) string {
	if m.Sandboxed && !m.Status(spec).Installed {
		return ErrSandboxed.Error()
	}
	return ""
}

// Ensure installs uv, Python, and spec's packages when they are missing or
// stale, and returns the environment's interpreter.
func (m *Manager) Ensure(ctx context.Context, spec Spec, progress Progress) (string, error) {
	if progress == nil {
		progress = func(string, string) {}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if st := m.Status(spec); st.Installed {
		return st.Python, nil
	}
	if m.Sandboxed {
		return "", ErrSandboxed
	}
	uv, err := m.ensureUV(ctx, progress)
	if err != nil {
		return "", err
	}
	dir := m.envDir(spec.Name)
	// A stale or half-built environment is rebuilt from scratch.
	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", err
	}
	progress("python", "Installing Python "+PythonVersion)
	if err := m.runUV(ctx, uv, progress, "venv", "--python", PythonVersion, "--seed", dir); err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("create Python environment: %w", err)
	}
	progress("packages", "Installing "+strings.Join(spec.Requirements, ", "))
	args := []string{"pip", "install", "--python", m.PythonPath(spec.Name)}
	if spec.Pinned {
		args = append(args, "--no-deps")
	}
	args = append(args, spec.InstallArgs...)
	args = append(args, spec.Requirements...)
	if err := m.runUV(ctx, uv, progress, args...); err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("install packages: %w", err)
	}
	if err := os.WriteFile(m.markerPath(spec.Name), []byte(requirementsKey(spec)), 0o644); err != nil {
		return "", err
	}
	progress("ready", "Environment ready")
	return m.PythonPath(spec.Name), nil
}

// Remove deletes an environment. The uv binary and cached Python stay.
func (m *Manager) Remove(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return os.RemoveAll(m.envDir(name))
}

// Env returns the variables that keep uv and Python inside Root.
func (m *Manager) Env() []string {
	return []string{
		"UV_CACHE_DIR=" + filepath.Join(m.Root, "cache"),
		"UV_PYTHON_INSTALL_DIR=" + filepath.Join(m.Root, "pythons"),
		"UV_PYTHON_PREFERENCE=only-managed",
		"UV_NO_CONFIG=1",
		"PYTHONNOUSERSITE=1",
	}
}

func (m *Manager) runUV(ctx context.Context, uv string, progress Progress, args ...string) error {
	cmd := exec.CommandContext(ctx, uv, args...)
	cmd.Env = append(os.Environ(), m.Env()...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return err
	}
	var tail []string
	sc := bufio.NewScanner(out)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		progress("log", line)
		tail = append(tail, line)
		if len(tail) > 8 {
			tail = tail[1:]
		}
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.Join(tail, " | "))
	}
	return nil
}

func uvAsset() (string, error) {
	var triple string
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "darwin/arm64":
		triple = "aarch64-apple-darwin"
	case "darwin/amd64":
		triple = "x86_64-apple-darwin"
	case "linux/amd64":
		triple = "x86_64-unknown-linux-gnu"
	case "linux/arm64":
		triple = "aarch64-unknown-linux-gnu"
	case "windows/amd64":
		return "uv-x86_64-pc-windows-msvc.zip", nil
	default:
		return "", fmt.Errorf("no managed Python for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	return "uv-" + triple + ".tar.gz", nil
}

// ensureUV downloads the pinned uv release and checks it against the
// release's published SHA-256.
func (m *Manager) ensureUV(ctx context.Context, progress Progress) (string, error) {
	path := m.uvPath()
	if st, err := os.Stat(path); err == nil && !st.IsDir() {
		return path, nil
	}
	asset, err := uvAsset()
	if err != nil {
		return "", err
	}
	progress("uv", "Downloading uv "+UVVersion)
	want, err := m.fetch(ctx, asset+".sha256")
	if err != nil {
		return "", fmt.Errorf("download uv checksum: %w", err)
	}
	fields := strings.Fields(string(want))
	if len(fields) == 0 {
		return "", fmt.Errorf("empty uv checksum")
	}
	archive, err := m.fetch(ctx, asset)
	if err != nil {
		return "", fmt.Errorf("download uv: %w", err)
	}
	sum := sha256.Sum256(archive)
	if got := hex.EncodeToString(sum[:]); !strings.EqualFold(got, fields[0]) {
		return "", fmt.Errorf("uv checksum mismatch: got %s, want %s", got, fields[0])
	}
	bin, err := extractBinary(asset, archive, filepath.Base(path))
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, bin, 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	return path, nil
}

func (m *Manager) fetch(ctx context.Context, name string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(m.ReleaseBase, "/")+"/"+name, nil)
	if err != nil {
		return nil, err
	}
	resp, err := m.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, name)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 200<<20))
}

func extractBinary(asset string, archive []byte, want string) ([]byte, error) {
	if strings.HasSuffix(asset, ".zip") {
		zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if filepath.Base(f.Name) == want {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return io.ReadAll(rc)
			}
		}
		return nil, fmt.Errorf("%s not found in %s", want, asset)
	}
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("%s not found in %s", want, asset)
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag == tar.TypeReg && filepath.Base(hdr.Name) == want {
			return io.ReadAll(tr)
		}
	}
}
