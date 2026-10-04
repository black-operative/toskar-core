package ocr

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/pyenv"
)

// fakePython stands in for the OCR environment's interpreter. It saves the
// job config and answers according to MODE.
const fakePython = `#!/bin/sh
cp "$3" "$(dirname "$0")/last-config.json"
case "$MODE" in
  fail) echo "Traceback (most recent call last):" >&2; echo "RuntimeError: page 1 could not be rendered" >&2; exit 1 ;;
  hang) exec sleep 30 ;;
  partial) printf '%s\n' '{"event": "page", "page": 2, "text": "half"}' ;;
  *) printf '%s\n' 'RapidOCR log line, not JSON'
     printf '%s\n' '{"event": "page", "page": 2, "text": "Store Hours\nSunday: closed", "done": 1, "total": 2}'
     printf '%s\n' '{"event": "page", "page": 5, "text": "", "done": 2, "total": 2}'
     printf '%s\n' '{"event": "done"}' ;;
esac
`

type fakeEnv struct {
	python string
	err    error
	specs  []pyenv.Spec
}

func (f *fakeEnv) Ensure(_ context.Context, spec pyenv.Spec, _ pyenv.Progress) (string, error) {
	f.specs = append(f.specs, spec)
	return f.python, f.err
}

func (f *fakeEnv) Env() []string { return nil }

func newFake(t *testing.T) (*Recognizer, *fakeEnv) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake interpreter is a shell script")
	}
	dir := t.TempDir()
	py := filepath.Join(dir, "python")
	if err := os.WriteFile(py, []byte(fakePython), 0o755); err != nil {
		t.Fatal(err)
	}
	env := &fakeEnv{python: py}
	return &Recognizer{Python: env, WorkDir: filepath.Join(t.TempDir(), "jobs")}, env
}

func TestRecognizeReadsPagesFromTheScript(t *testing.T) {
	r, env := newFake(t)
	texts, err := r.RecognizePDF(context.Background(), []byte("%PDF-1.4"), []int{2, 5})
	if err != nil {
		t.Fatal(err)
	}
	if texts[2] != "Store Hours\nSunday: closed" || texts[5] != "" || len(texts) != 2 {
		t.Fatalf("texts = %v", texts)
	}
	if len(env.specs) != 1 || env.specs[0].Name != "ocr" || !env.specs[0].Pinned || !slices.Contains(env.specs[0].Requirements, "opencv-python-headless==5.0.0.93") {
		t.Fatalf("spec = %+v", env.specs)
	}
	cfg, _ := os.ReadFile(filepath.Join(filepath.Dir(env.python), "last-config.json"))
	if !strings.Contains(string(cfg), `"pages":[2,5]`) || !strings.Contains(string(cfg), `"dpi":200`) {
		t.Fatalf("config = %s", cfg)
	}
	if left, _ := os.ReadDir(r.WorkDir); len(left) != 0 {
		t.Fatalf("job files left behind: %v", left)
	}
}

func TestRecognizeReportsFailures(t *testing.T) {
	r, env := newFake(t)
	ctx := context.Background()
	t.Setenv("MODE", "fail")
	if _, err := r.RecognizePDF(ctx, []byte("%PDF"), []int{1}); err == nil || !strings.Contains(err.Error(), "could not be rendered") {
		t.Fatalf("got %v", err)
	}
	// Output that stops before "done" is not a result.
	t.Setenv("MODE", "partial")
	if _, err := r.RecognizePDF(ctx, []byte("%PDF"), []int{1, 2}); err == nil {
		t.Fatal("an unfinished run was accepted")
	}
	env.err = pyenv.ErrSandboxed
	if _, err := r.RecognizePDF(ctx, []byte("%PDF"), []int{1}); !errors.Is(err, pyenv.ErrSandboxed) || !strings.Contains(err.Error(), "not included in this copy") {
		t.Fatalf("got %v", err)
	}
	env.err = errors.New("uv: no network")
	if _, err := r.RecognizePDF(ctx, []byte("%PDF"), []int{1}); err == nil || !strings.Contains(err.Error(), "could not be installed") {
		t.Fatalf("got %v", err)
	}
}

func TestRecognizeStopsWhenCancelled(t *testing.T) {
	r, _ := newFake(t)
	t.Setenv("MODE", "hang")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := r.RecognizePDF(ctx, []byte("%PDF"), []int{1}); err == nil {
		t.Fatal("a cancelled run returned text")
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("the script was not stopped")
	}
}

// TestRealRecognition runs RapidOCR on a scanned page. It is skipped unless
// YGG_TEST_OCR_PYTHON is an interpreter with the packages in Requirements.
func TestRealRecognition(t *testing.T) {
	py := os.Getenv("YGG_TEST_OCR_PYTHON")
	if py == "" {
		t.Skip("set YGG_TEST_OCR_PYTHON to an interpreter with the OCR packages")
	}
	raw, err := os.ReadFile(filepath.Join("..", "mimir", "testdata", "scanned.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	r := &Recognizer{Python: &fakeEnv{python: py}, WorkDir: t.TempDir()}
	start := time.Now()
	texts, err := r.RecognizePDF(context.Background(), raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("recognized in %s: %q", time.Since(start).Round(time.Millisecond), texts[1])
	for _, want := range []string{"Store Hours", "Monday to Friday", "Sunday: closed"} {
		if !strings.Contains(texts[1], want) {
			t.Errorf("missing %q", want)
		}
	}
}
