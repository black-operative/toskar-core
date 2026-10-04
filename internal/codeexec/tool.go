package codeexec

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/yggdrasil-core/internal/artifacts"
	"github.com/yeixio/yggdrasil-core/internal/pyenv"
)

// Requirements are the packages in the code environment: numbers, tables,
// charts, and reading and writing spreadsheets.
var Requirements = []string{"numpy==2.1.3", "pandas==2.2.3", "matplotlib==3.9.2", "openpyxl==3.1.5"}

// Spec is the managed Python environment code runs in.
func Spec() pyenv.Spec { return pyenv.Spec{Name: "code", Requirements: Requirements} }

// Limits for one run (Gungnir §20).
const (
	timeLimit = 90 * time.Second
	// memoryLimit is bytes of address space, where the system enforces it
	// (Linux). Numerical libraries reserve much more than they use, so it
	// is generous; it stops runaway allocations, not normal analysis.
	memoryLimit  = 4 << 30
	maxCodeBytes = 64 << 10
	maxOutput    = 32 << 10
	maxFilesOut  = 10
	maxFileBytes = 25 << 20
	maxInputs    = 5
)

// outputKinds are the files a run may hand back as attachments.
var outputKinds = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".svg": true, ".pdf": true, ".csv": true, ".tsv": true,
	".xlsx": true, ".json": true, ".txt": true, ".md": true, ".html": true,
}

// PythonEnv provides the environment. *pyenv.Manager implements it.
type PythonEnv interface {
	Ensure(ctx context.Context, spec pyenv.Spec, progress pyenv.Progress) (string, error)
	Env() []string
	Unavailable(spec pyenv.Spec) string
}

// Tool is code.execute.
type Tool struct {
	Python  PythonEnv
	Sandbox Sandbox
	Store   *artifacts.Store
	// WorkDir holds each run's folder while it runs.
	WorkDir string
	// PythonRoot is the managed Python folder the sandbox may read.
	PythonRoot string
	// TimeLimit overrides the time limit; tests shorten it.
	TimeLimit time.Duration

	mu sync.Mutex
}

func (t *Tool) ID() string          { return "code.execute" }
func (t *Tool) DisplayName() string { return "Run Code" }
func (t *Tool) Description() string { return "Run Python in a sandbox" }

// Available reports whether code can run here, and why not.
func (t *Tool) Available() (bool, string) {
	if t.Sandbox == nil {
		return false, "no sandbox is configured"
	}
	if ok, why := t.Sandbox.Available(); !ok {
		return false, why
	}
	if why := t.Python.Unavailable(Spec()); why != "" {
		return false, why
	}
	return true, ""
}

// bootstrap sets limits and a non-interactive chart backend, then runs
// main.py as a script.
const bootstrap = `import resource, runpy, sys
for lim, val in ((getattr(resource, "RLIMIT_AS", None), %d), (resource.RLIMIT_CPU, %d)):
    if lim is not None:
        try:
            resource.setrlimit(lim, (val, val))
        except (ValueError, OSError):
            pass
import os
os.environ["MPLBACKEND"] = "Agg"
sys.dont_write_bytecode = True
sys.argv = ["main.py"]
runpy.run_path("main.py", run_name="__main__")
`

// Execute runs the code and returns what it printed and the files it made.
func (t *Tool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	code, _ := args["code"].(string)
	if strings.TrimSpace(code) == "" {
		return nil, fmt.Errorf("code required")
	}
	if len(code) > maxCodeBytes {
		return nil, fmt.Errorf("the code is longer than %d KB", maxCodeBytes>>10)
	}
	if ok, why := t.Available(); !ok {
		return nil, fmt.Errorf("code cannot run on this computer: %s", why)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	py, err := t.Python.Ensure(ctx, Spec(), nil)
	if errors.Is(err, pyenv.ErrSandboxed) {
		return nil, fmt.Errorf("code cannot run in this copy of Toskar: %w", err)
	}
	if err != nil {
		return nil, fmt.Errorf("the code environment could not be installed: %w", err)
	}
	if err := os.MkdirAll(t.WorkDir, 0o700); err != nil {
		return nil, err
	}
	work, err := os.MkdirTemp(t.WorkDir, "run-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)
	inputs, err := t.copyInputs(ctx, work, args["files"])
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(work, "main.py"), []byte(code), 0o600); err != nil {
		return nil, err
	}
	_ = os.MkdirAll(filepath.Join(work, ".tmp"), 0o700)
	before := snapshot(work)

	limit := timeLimit
	if t.TimeLimit > 0 {
		limit = t.TimeLimit
	}
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	env := append(t.Python.Env(),
		"HOME="+work, "TMPDIR="+filepath.Join(work, ".tmp"), "MPLCONFIGDIR="+filepath.Join(work, ".tmp"),
		"PATH=/usr/bin:/bin", "LANG=C.UTF-8",
		// Keep the math libraries from starting a thread per core.
		"OPENBLAS_NUM_THREADS=2", "OMP_NUM_THREADS=2", "MKL_NUM_THREADS=2")
	readOnly := []string{t.PythonRoot}
	cmd := t.Sandbox.Command(ctx, work, readOnly, env, []string{py, "-I", "-u", "-c", fmt.Sprintf(bootstrap, memoryLimit, int(limit.Seconds())+1)})
	cmd.WaitDelay = 3 * time.Second
	var stdout, stderr capped
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()
	result := map[string]any{"stdout": stdout.String(), "stderr": stderr.String(), "sandbox": t.Sandbox.Name()}
	if stdout.cut || stderr.cut {
		result["truncated"] = true
	}
	if ctx.Err() == context.DeadlineExceeded {
		return result, fmt.Errorf("the code ran longer than %d seconds and was stopped", int(limit.Seconds()))
	}
	exit := 0
	if runErr != nil {
		exit = -1
		if ee, ok := runErr.(interface{ ExitCode() int }); ok {
			exit = ee.ExitCode()
		}
	}
	result["exit_code"] = exit
	files, skipped := t.collect(ctx, work, before, inputs)
	if len(files) > 0 {
		result["files"] = files
		result["note"] = "The files are attached to your answer as downloads. Describe what they show; do not repeat their contents."
	}
	if len(skipped) > 0 {
		result["skipped_files"] = skipped
	}
	return result, nil
}

// copyInputs puts files from this chat into the working folder by name.
func (t *Tool) copyInputs(ctx context.Context, work string, raw any) (map[string]bool, error) {
	names := map[string]bool{"main.py": true}
	list, _ := raw.([]any)
	if len(list) > maxInputs {
		return nil, fmt.Errorf("at most %d files can be given to the code", maxInputs)
	}
	if len(list) == 0 || t.Store == nil {
		return names, nil
	}
	conv := artifacts.ConversationFrom(ctx)
	have, err := t.Store.List(ctx, conv)
	if err != nil {
		return nil, err
	}
	for _, item := range list {
		want, _ := item.(string)
		want = strings.TrimSpace(want)
		var found *artifacts.Artifact
		for i := len(have) - 1; i >= 0; i-- {
			if have[i].ID == want || strings.EqualFold(have[i].Name, want) {
				found = &have[i]
				break
			}
		}
		if found == nil {
			return nil, fmt.Errorf("no file named %q in this chat", want)
		}
		_, data, err := t.Store.Read(ctx, found.ID)
		if err != nil {
			return nil, err
		}
		name := artifacts.CleanName(found.Name)
		if err := os.WriteFile(filepath.Join(work, name), data, 0o600); err != nil {
			return nil, err
		}
		names[name] = true
	}
	return names, nil
}

type fileState struct {
	size int64
	mod  time.Time
}

func snapshot(dir string) map[string]fileState {
	out := map[string]fileState{}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if info, err := e.Info(); err == nil && !e.IsDir() {
			out[e.Name()] = fileState{info.Size(), info.ModTime()}
		}
	}
	return out
}

// collect saves new or changed files of the allowed kinds as attachments.
func (t *Tool) collect(ctx context.Context, work string, before map[string]fileState, inputs map[string]bool) ([]map[string]any, []string) {
	after := snapshot(work)
	var names []string
	for name, st := range after {
		if strings.HasPrefix(name, ".") || name == "main.py" {
			continue
		}
		if old, ok := before[name]; ok && old == st && inputs[name] {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	var files []map[string]any
	var skipped []string
	for _, name := range names {
		if !outputKinds[strings.ToLower(filepath.Ext(name))] || after[name].size > maxFileBytes || len(files) == maxFilesOut || t.Store == nil {
			skipped = append(skipped, name)
			continue
		}
		data, err := os.ReadFile(filepath.Join(work, name))
		if err != nil {
			skipped = append(skipped, name)
			continue
		}
		a, err := t.Store.Save(ctx, artifacts.Input{ConversationID: artifacts.ConversationFrom(ctx), Name: name, Producer: artifacts.ProducerAssistant, Data: data})
		if err != nil {
			skipped = append(skipped, name)
			continue
		}
		files = append(files, map[string]any{"id": a.ID, "name": a.Name, "kind": a.Kind, "size_bytes": a.Size})
	}
	return files, skipped
}

// capped keeps the first maxOutput bytes of a stream.
type capped struct {
	b   strings.Builder
	cut bool
}

func (c *capped) Write(p []byte) (int, error) {
	if room := maxOutput - c.b.Len(); room > 0 {
		if len(p) > room {
			c.b.Write(p[:room])
			c.cut = true
		} else {
			c.b.Write(p)
		}
	} else if len(p) > 0 {
		c.cut = true
	}
	return len(p), nil
}

func (c *capped) String() string { return c.b.String() }
