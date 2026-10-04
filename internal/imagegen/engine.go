package imagegen

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/jpeg" // DecodeConfig reads the size of an image to edit
	_ "image/png"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/yeixio/toskar-core/internal/remotetools"
)

// Limits.
const (
	maxPrompt   = 2000
	defaultSide = 1024
	minSide     = 256
	maxSide     = 1536
	maxPixels   = 1536 * 1024
	// runLimit allows for a slow computer making a large image on the CPU.
	runLimit = 20 * time.Minute
)

// Engine runs sd-cli.
type Engine struct {
	Setup *Setup
	// WorkDir holds each job's files while it runs.
	WorkDir string
	// What names what it makes in messages: image generation (the
	// default) or video generation.
	What string

	// mu runs one image at a time; each uses all of the memory it can.
	mu sync.Mutex
}

func (e *Engine) what() string {
	if e.What != "" {
		return e.What
	}
	return "image generation"
}

// Available reports whether images can be made, and what would set it up.
func (e *Engine) Available() (bool, string) {
	st := e.Setup.Status()
	if !st.Supported {
		return false, st.Unsupported
	}
	if st.Ready {
		return true, ""
	}
	for _, m := range st.Models {
		if m.Recommended {
			return false, fmt.Sprintf("%s isn't set up yet. Set it up on the Tools page: %s, %.1f GB to download", e.what(), m.Name, float64(m.SizeBytes)/1e9)
		}
	}
	return false, e.what() + " isn't set up yet. Set it up on the Tools page"
}

// provider describes this computer's image provider (Gungnir §16). Only the
// macOS build uses the GPU (Metal); the others run on the CPU.
func (e *Engine) provider(tool string) remotetools.Provider {
	p := remotetools.Provider{Tool: tool, Accelerated: runtime.GOOS == "darwin"}
	st := e.Setup.Status()
	if m, ok := e.Setup.lookup(st.Active); ok {
		p.Name = m.Name
	}
	switch {
	case st.Ready:
		p.State = remotetools.Healthy
	case st.Job != nil && st.Job.Running:
		p.State = remotetools.Installing
	case st.Job != nil && st.Job.Error != "":
		p.State, p.Reason = remotetools.Failed, st.Job.Error
	default:
		_, p.Reason = e.Available()
		p.State = remotetools.Unavailable
	}
	return p
}

// Request is one image to make, or to edit when Reference is set.
type Request struct {
	Prompt string
	Width  int
	Height int
	// Seed repeats an image; 0 picks one.
	Seed int64
	// Reference is the image to edit, and RefName its file name.
	Reference []byte
	RefName   string
}

// Result is a made image.
type Result struct {
	PNG     []byte
	Width   int
	Height  int
	Seed    int64
	Seconds float64
	Model   string
}

// side rounds a dimension to what the model takes: a multiple of 64 in range.
func side(n int) int {
	if n <= 0 {
		n = defaultSide
	}
	n = (n + 32) / 64 * 64
	return min(max(n, minSide), maxSide)
}

// fit returns the size to make an image of, keeping the aspect of w×h
// within the pixel budget.
func fit(w, h int) (int, int) {
	w, h = side(w), side(h)
	if w*h > maxPixels {
		scale := math.Sqrt(float64(maxPixels) / float64(w*h))
		w = max(int(float64(w)*scale)/64*64, minSide)
		h = max(int(float64(h)*scale)/64*64, minSide)
	}
	return w, h
}

// Generate makes an image.
func (e *Engine) Generate(ctx context.Context, req Request) (Result, error) {
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return Result{}, fmt.Errorf("prompt required: describe the image")
	}
	if utf8.RuneCountInString(prompt) > maxPrompt {
		return Result{}, fmt.Errorf("the prompt is longer than %d characters", maxPrompt)
	}
	if ok, why := e.Available(); !ok {
		return Result{}, fmt.Errorf("%s", why)
	}
	cli := e.Setup.CLI()
	model, files, err := e.Setup.paths()
	if err != nil {
		return Result{}, err
	}
	w, h := req.Width, req.Height
	if req.Reference != nil {
		if !model.Edits {
			return Result{}, fmt.Errorf("%s cannot edit images", model.Name)
		}
		cfg, _, err := image.DecodeConfig(bytes.NewReader(req.Reference))
		if err != nil {
			return Result{}, fmt.Errorf("%s is not a PNG or JPEG image", req.RefName)
		}
		// An edit keeps the picture's shape unless a size is asked for.
		if w == 0 && h == 0 {
			w, h = cfg.Width, cfg.Height
			if w > defaultSide || h > defaultSide {
				scale := float64(defaultSide) / float64(max(w, h))
				w, h = int(float64(w)*scale), int(float64(h)*scale)
			}
		}
	}
	w, h = fit(w, h)
	seed := req.Seed
	if seed <= 0 {
		seed = rand.Int64N(1 << 31)
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if err := os.MkdirAll(e.WorkDir, 0o700); err != nil {
		return Result{}, err
	}
	dir, err := os.MkdirTemp(e.WorkDir, "image-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(dir)
	out := filepath.Join(dir, "out.png")
	args := []string{
		"--diffusion-model", files["diffusion"], "--vae", files["vae"], "--llm", files["llm"],
		"-p", prompt, "-W", strconv.Itoa(w), "-H", strconv.Itoa(h),
		"--steps", strconv.Itoa(model.Steps), "--cfg-scale", strconv.FormatFloat(model.CFG, 'f', -1, 64),
		"--sampling-method", "euler", "-s", strconv.FormatInt(seed, 10), "-o", out,
		// Weights wait in memory and move to the GPU when used, so a
		// computer with less graphics memory can still run the model.
		"--offload-to-cpu", "--diffusion-fa",
	}
	if req.Reference != nil {
		ext := strings.ToLower(filepath.Ext(req.RefName))
		if ext != ".jpg" && ext != ".jpeg" {
			ext = ".png"
		}
		ref := filepath.Join(dir, "reference"+ext)
		if err := os.WriteFile(ref, req.Reference, 0o600); err != nil {
			return Result{}, err
		}
		args = append(args, "-r", ref)
	}
	start := time.Now()
	data, err := runCLI(ctx, cli, dir, args, out, "image", runLimit)
	if err != nil {
		return Result{}, err
	}
	return Result{PNG: data, Width: w, Height: h, Seed: seed, Seconds: time.Since(start).Seconds(), Model: model.Name}, nil
}

// runCLI runs sd-cli in dir and returns the file it wrote to out.
func runCLI(ctx context.Context, cli, dir string, args []string, out, what string, limit time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	cmd := exec.CommandContext(ctx, cli, args...)
	cmd.Dir = dir
	cmd.WaitDelay = 5 * time.Second
	var stderr tail
	cmd.Stdout, cmd.Stderr = &stderr, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("the %s took longer than %d minutes and was stopped", what, int(limit.Minutes()))
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("the %s could not be made: %s", what, stderr.lastLine(err))
	}
	data, err := os.ReadFile(out)
	if err != nil {
		return nil, fmt.Errorf("the %s could not be made: %s", what, stderr.lastLine(err))
	}
	return data, nil
}

// tail keeps the end of sd-cli's output, for its error.
type tail struct {
	b []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.b = append(t.b, p...)
	if len(t.b) > 16<<10 {
		t.b = t.b[len(t.b)-8<<10:]
	}
	return len(p), nil
}

func (t *tail) lastLine(err error) string {
	lines := strings.Split(strings.TrimSpace(string(t.b)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		// Progress bars redraw with carriage returns; keep the last frame.
		parts := strings.Split(lines[i], "\r")
		if s := strings.TrimSpace(parts[len(parts)-1]); s != "" {
			if len(s) > 300 {
				s = s[:300]
			}
			return s
		}
	}
	return err.Error()
}
