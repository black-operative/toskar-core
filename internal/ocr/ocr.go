// Package ocr reads the text of scanned PDF pages, which have no text layer,
// so Mimir can index them. It runs RapidOCR (Apache-2.0) with pypdfium2 in a
// Python environment the daemon installs on first use; the recognition
// models ship inside the package, so nothing else is downloaded.
package ocr

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/yggdrasil-core/internal/pyenv"
)

// Languages are the languages the bundled recognition models read (multilingual
// spec §20): RapidOCR's Chinese and English models. Accented letters of other
// languages may be misread, and other scripts are not recognized.
var Languages = []string{"zh", "en"}

//go:embed ocr_pdf.py
var script []byte

// Requirements pins every package the OCR environment needs. It is installed
// without dependency resolution so opencv-python-headless replaces
// opencv-python, which needs desktop graphics libraries that servers lack.
var Requirements = []string{
	"rapidocr==3.9.2",
	"onnxruntime==1.30.0",
	"pypdfium2==5.13.0",
	"opencv-python-headless==5.0.0.93",
	"numpy==2.5.3",
	"pyclipper==1.4.0",
	"shapely==2.1.2",
	"pyyaml==6.0.3",
	"pillow==12.3.0",
	"six==1.17.0",
	"tqdm==4.70.1",
	"omegaconf==2.3.1",
	"antlr4-python3-runtime==4.9.3",
	"colorlog==6.12.0",
	"colorama==0.4.6; sys_platform == 'win32'",
	"requests==2.34.2",
	"urllib3==2.8.0",
	"idna==3.20",
	"certifi==2026.7.22",
	"charset-normalizer==3.5.2",
	"flatbuffers==25.12.19",
	"packaging==26.3",
	"protobuf==7.36.2",
}

// Spec is the OCR Python environment.
func Spec() pyenv.Spec {
	return pyenv.Spec{Name: "ocr", Requirements: Requirements, Pinned: true}
}

// PythonEnv provides the environment. *pyenv.Manager implements it.
type PythonEnv interface {
	Ensure(ctx context.Context, spec pyenv.Spec, progress pyenv.Progress) (string, error)
	Env() []string
}

// Recognizer reads scanned pages.
type Recognizer struct {
	Python PythonEnv
	// WorkDir holds the script and each job's files while it runs.
	WorkDir string
	// DPI is the resolution pages are rendered at. Zero uses 200.
	DPI int

	// mu runs one job at a time; recognition uses every core.
	mu sync.Mutex
}

// perPage bounds how long one page may take, on top of a fixed allowance
// for starting Python and loading the models.
const (
	startup = 60 * time.Second
	perPage = 30 * time.Second
)

// RecognizePDF returns the recognized text of the given pages (1-based), or
// of every page when pages is empty, keyed by page number.
func (r *Recognizer) RecognizePDF(ctx context.Context, raw []byte, pages []int) (map[int]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	py, err := r.Python.Ensure(ctx, Spec(), nil)
	if errors.Is(err, pyenv.ErrSandboxed) {
		return nil, errNotIncluded{}
	}
	if err != nil {
		return nil, fmt.Errorf("text recognition could not be installed: %w", err)
	}
	if err := os.MkdirAll(r.WorkDir, 0o755); err != nil {
		return nil, err
	}
	job, err := os.MkdirTemp(r.WorkDir, "job-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(job)
	pdfPath := filepath.Join(job, "input.pdf")
	scriptPath := filepath.Join(job, "ocr_pdf.py")
	cfgPath := filepath.Join(job, "config.json")
	dpi := r.DPI
	if dpi <= 0 {
		dpi = 200
	}
	cfg, _ := json.Marshal(map[string]any{"pdf": pdfPath, "pages": pages, "dpi": dpi})
	for path, data := range map[string][]byte{pdfPath: raw, scriptPath: script, cfgPath: cfg} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return nil, err
		}
	}

	budget := startup + perPage*time.Duration(max(len(pages), 1))
	if len(pages) == 0 {
		budget = startup + perPage*50
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	cmd := exec.CommandContext(ctx, py, "-u", scriptPath, cfgPath)
	cmd.Dir = job
	cmd.Env = append(os.Environ(), r.Python.Env()...)
	cmd.WaitDelay = 5 * time.Second
	var stderr tail
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	texts := map[int]string{}
	done := false
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 0, 64<<10), 16<<20)
	for sc.Scan() {
		var ev struct {
			Event string `json:"event"`
			Page  int    `json:"page"`
			Text  string `json:"text"`
		}
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		switch ev.Event {
		case "page":
			texts[ev.Page] = ev.Text
		case "done":
			done = true
		}
	}
	waitErr := cmd.Wait()
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		return nil, fmt.Errorf("text recognition took longer than %s", budget.Round(time.Second))
	case ctx.Err() != nil:
		return nil, ctx.Err()
	case waitErr != nil || !done:
		return nil, fmt.Errorf("text recognition failed: %s", stderr.lastLine())
	}
	return texts, nil
}

// errNotIncluded is returned by a sandboxed app that does not bundle the
// OCR environment. It unwraps to pyenv.ErrSandboxed.
type errNotIncluded struct{}

func (errNotIncluded) Error() string {
	return "text recognition is not included in this copy of Toskar, which runs in the macOS App Sandbox"
}

func (errNotIncluded) Unwrap() error { return pyenv.ErrSandboxed }

// tail keeps the end of a process's stderr for error messages.
type tail struct{ b bytes.Buffer }

func (t *tail) Write(p []byte) (int, error) {
	t.b.Write(p)
	if t.b.Len() > 8192 {
		rest := t.b.Bytes()[t.b.Len()-8192:]
		t.b = *bytes.NewBuffer(append([]byte(nil), rest...))
	}
	return len(p), nil
}

// lastLine is the Python exception, or the last thing printed.
func (t *tail) lastLine() string {
	lines := strings.Split(strings.TrimSpace(t.b.String()), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return "no output"
}
