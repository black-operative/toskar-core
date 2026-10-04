package pyenv

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeUV records its arguments and creates the interpreter a real venv would.
const fakeUV = `#!/bin/sh
echo "$@" >> "$(dirname "$0")/calls.log"
if [ "$1" = "venv" ]; then
  for last; do :; done
  mkdir -p "$last/bin"
  printf '#!/bin/sh\n' > "$last/bin/python"
  chmod +x "$last/bin/python"
fi
if [ "$1" = "pip" ] && [ -n "$FAIL_PIP" ]; then
  echo "resolution failed" >&2
  exit 1
fi
`

func uvRelease(t *testing.T, script string, corrupt bool) *httptest.Server {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "uv-dir/uv", Mode: 0o755, Size: int64(len(script)), Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte(script))
	_ = tw.Close()
	_ = gz.Close()
	archive := buf.Bytes()
	sum := sha256.Sum256(archive)
	digest := hex.EncodeToString(sum[:])
	if corrupt {
		digest = strings.Repeat("0", 64)
	}
	asset, err := uvAsset()
	if err != nil {
		t.Skip(err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + asset:
			_, _ = w.Write(archive)
		case "/" + asset + ".sha256":
			_, _ = w.Write([]byte(digest + "  " + asset + "\n"))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestEnsureInstallsOnceAndRebuildsWhenRequirementsChange(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uv is a shell script")
	}
	srv := uvRelease(t, fakeUV, false)
	defer srv.Close()
	m := New(t.TempDir())
	m.ReleaseBase = srv.URL
	ctx := context.Background()
	spec := Spec{Name: "trainer-test", Requirements: []string{"mlx-lm==0.31.3"}}

	var steps []string
	py, err := m.Ensure(ctx, spec, func(step, _ string) { steps = append(steps, step) })
	if err != nil {
		t.Fatal(err)
	}
	if py != m.PythonPath("trainer-test") {
		t.Fatalf("python = %s", py)
	}
	if st := m.Status(spec); !st.Installed {
		t.Fatalf("status after install = %+v", st)
	}
	if strings.Join(steps, ",") != "uv,python,packages,ready" {
		t.Fatalf("steps = %v", steps)
	}

	calls := func() string {
		b, _ := os.ReadFile(filepath.Join(filepath.Dir(m.uvPath()), "calls.log"))
		return string(b)
	}
	if !strings.Contains(calls(), "pip install --python "+py+" mlx-lm==0.31.3") {
		t.Fatalf("uv calls:\n%s", calls())
	}

	// A second Ensure with the same requirements does nothing.
	before := calls()
	if _, err := m.Ensure(ctx, spec, nil); err != nil {
		t.Fatal(err)
	}
	if calls() != before {
		t.Fatal("Ensure reinstalled an up-to-date environment")
	}

	// New pins mark the environment stale and rebuild it.
	spec.Requirements = []string{"mlx-lm==0.32.0"}
	if st := m.Status(spec); st.Installed || !st.Stale {
		t.Fatalf("status with new pins = %+v", st)
	}
	if _, err := m.Ensure(ctx, spec, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(calls(), "mlx-lm==0.32.0") {
		t.Fatalf("rebuild did not install new pins:\n%s", calls())
	}
}

func TestEnsureCleansUpAFailedInstall(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uv is a shell script")
	}
	srv := uvRelease(t, fakeUV, false)
	defer srv.Close()
	m := New(t.TempDir())
	m.ReleaseBase = srv.URL
	t.Setenv("FAIL_PIP", "1")
	spec := Spec{Name: "broken", Requirements: []string{"nope==1"}}
	_, err := m.Ensure(context.Background(), spec, nil)
	if err == nil || !strings.Contains(err.Error(), "resolution failed") {
		t.Fatalf("want the uv error, got %v", err)
	}
	if _, statErr := os.Stat(m.envDir("broken")); !os.IsNotExist(statErr) {
		t.Fatal("a failed install left a half-built environment")
	}
}

func TestEnsureRejectsAChecksumMismatch(t *testing.T) {
	srv := uvRelease(t, fakeUV, true)
	defer srv.Close()
	m := New(t.TempDir())
	m.ReleaseBase = srv.URL
	_, err := m.Ensure(context.Background(), Spec{Name: "x"}, nil)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("want checksum mismatch, got %v", err)
	}
	if _, statErr := os.Stat(m.uvPath()); !os.IsNotExist(statErr) {
		t.Fatal("an unverified uv binary was written")
	}
}

func TestPinnedEnvironmentInstallsWithoutDependencies(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uv is a shell script")
	}
	srv := uvRelease(t, fakeUV, false)
	defer srv.Close()
	m := New(t.TempDir())
	m.ReleaseBase = srv.URL
	spec := Spec{Name: "ocr", Requirements: []string{"rapidocr==3.9.2", "opencv-python-headless==5.0.0.93"}, Pinned: true}
	py, err := m.Ensure(context.Background(), spec, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(filepath.Dir(m.uvPath()), "calls.log"))
	if !strings.Contains(string(b), "pip install --python "+py+" --no-deps rapidocr==3.9.2") {
		t.Fatalf("uv calls:\n%s", b)
	}
	// The same packages without pinning are a different environment.
	spec.Pinned = false
	if st := m.Status(spec); st.Installed || !st.Stale {
		t.Fatalf("status = %+v", st)
	}
}

func TestInstallArgsArePassedAndPartOfTheEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uv is a shell script")
	}
	srv := uvRelease(t, fakeUV, false)
	defer srv.Close()
	m := New(t.TempDir())
	m.ReleaseBase = srv.URL
	spec := Spec{Name: "torch", Requirements: []string{"torch==2.14.1"}, InstallArgs: []string{"--torch-backend=auto"}}
	py, err := m.Ensure(context.Background(), spec, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(filepath.Dir(m.uvPath()), "calls.log"))
	if !strings.Contains(string(b), "pip install --python "+py+" --torch-backend=auto torch==2.14.1") {
		t.Fatalf("uv calls:\n%s", b)
	}
	spec.InstallArgs = nil
	if st := m.Status(spec); st.Installed || !st.Stale {
		t.Fatalf("status without the args = %+v", st)
	}
}

// bundleEnv lays out an environment the way a packaged app ships it.
func bundleEnv(t *testing.T, dir string, spec Spec, marker string) string {
	t.Helper()
	py := interpreter(filepath.Join(dir, spec.Name))
	if err := os.MkdirAll(filepath.Dir(py), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(py, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// The marker name from before the rename, as bundles built then carry (#237).
	if err := os.WriteFile(filepath.Join(dir, spec.Name, legacyMarkerName), []byte(marker), 0o644); err != nil {
		t.Fatal(err)
	}
	return py
}

func TestSandboxedManagerNeverDownloads(t *testing.T) {
	downloads := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { downloads++; http.NotFound(w, r) }))
	defer srv.Close()
	m := New(t.TempDir())
	m.ReleaseBase, m.Bundled, m.Sandboxed = srv.URL, t.TempDir(), true
	spec := Spec{Name: "trainer-mlx", Requirements: []string{"mlx-lm==0.31.3"}}
	if _, err := m.Ensure(context.Background(), spec, nil); !errors.Is(err, ErrSandboxed) {
		t.Fatalf("got %v", err)
	}
	if downloads != 0 {
		t.Fatalf("a sandboxed daemon downloaded %d times", downloads)
	}
	if why := m.Unavailable(spec); !strings.Contains(why, "App Sandbox") {
		t.Fatalf("unavailable = %q", why)
	}
}

func TestBundledEnvironmentIsUsedEvenWhenSandboxed(t *testing.T) {
	m := New(t.TempDir())
	m.Bundled, m.Sandboxed = t.TempDir(), true
	spec := Spec{Name: "ocr", Requirements: []string{"rapidocr==3.9.2"}, Pinned: true}
	py := bundleEnv(t, m.Bundled, spec, RequirementsKey(spec))
	st := m.Status(spec)
	if !st.Installed || !st.Bundled || st.Python != py {
		t.Fatalf("status = %+v", st)
	}
	got, err := m.Ensure(context.Background(), spec, nil)
	if err != nil || got != py {
		t.Fatalf("ensure = %q, %v", got, err)
	}
	if why := m.Unavailable(spec); why != "" {
		t.Fatalf("unavailable = %q", why)
	}
}

func TestOutdatedBundledEnvironmentIsNotUsed(t *testing.T) {
	m := New(t.TempDir())
	m.Bundled, m.Sandboxed = t.TempDir(), true
	spec := Spec{Name: "trainer-mlx", Requirements: []string{"mlx-lm==0.32.0"}}
	bundleEnv(t, m.Bundled, spec, RequirementsKey(Spec{Name: "trainer-mlx", Requirements: []string{"mlx-lm==0.31.3"}}))
	if st := m.Status(spec); st.Installed || st.Bundled {
		t.Fatalf("an environment built from other requirements was used: %+v", st)
	}
	if why := m.Unavailable(spec); why == "" {
		t.Fatal("an outdated bundle should leave training unavailable in the sandbox")
	}
}

func TestSandboxDetection(t *testing.T) {
	t.Setenv("APP_SANDBOX_CONTAINER_ID", "")
	t.Setenv("YGGDRASIL_SANDBOXED", "")
	if Sandboxed() {
		t.Fatal("sandboxed without the variables")
	}
	t.Setenv("APP_SANDBOX_CONTAINER_ID", "io.yeix.yggdrasil")
	if !Sandboxed() || !New(t.TempDir()).Sandboxed {
		t.Fatal("macOS sandbox not detected")
	}
}

// An environment installed now carries the new marker name, and stays
// current (#237).
func TestInstalledEnvironmentHasTheToskarMarker(t *testing.T) {
	dir := t.TempDir()
	spec := Spec{Name: "env"}
	py := interpreter(filepath.Join(dir, spec.Name))
	if err := os.MkdirAll(filepath.Dir(py), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(py, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, spec.Name, markerName), []byte(RequirementsKey(spec)), 0o644); err != nil {
		t.Fatal(err)
	}
	if st := envStatus(spec, filepath.Join(dir, spec.Name)); !st.Installed {
		t.Fatalf("status %+v", st)
	}
}
