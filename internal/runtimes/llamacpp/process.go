package llamacpp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// ProcessSupervisor manages llama-server child processes.
type ProcessSupervisor struct {
	logsDir string
	mu      sync.Mutex
	procs   map[string]*managedProcess
	onExit  func(instanceID, modelID string, exitCode int, stderrTail string)
}

type managedProcess struct {
	cmd         *exec.Cmd
	endpoint    string
	modelID     string
	adapters    []string
	mode        string
	logFile     *os.File
	logPath     string
	intentional bool
	// exited is closed when the process ends.
	exited chan struct{}
}

// NewSupervisor creates a process supervisor.
func NewSupervisor(logsDir string) *ProcessSupervisor {
	return &ProcessSupervisor{
		logsDir: logsDir,
		procs:   make(map[string]*managedProcess),
	}
}

func (r *Runtime) ensureSupervisor() {
	if r.sup == nil {
		r.sup = NewSupervisor(r.logsDir)
	}
}

// OnProcessExit reports a llama-server that exited on its own.
func (r *Runtime) OnProcessExit(fn func(instanceID, modelID string, exitCode int, stderrTail string)) {
	r.ensureSupervisor()
	r.sup.mu.Lock()
	r.sup.onExit = fn
	r.sup.mu.Unlock()
}

func (r *Runtime) StartModel(ctx context.Context, cfg pluginapi.ModelStartConfig) (pluginapi.RunningModel, error) {
	det, err := r.Detect(ctx)
	if err != nil {
		return pluginapi.RunningModel{}, err
	}
	if !det.Installed {
		return pluginapi.RunningModel{}, contracts.Errorf("RUNTIME_NOT_INSTALLED", map[string]any{"runtime": "llama-server"}, "llama-server not installed: %s", det.Message)
	}
	if cfg.ModelPath == "" {
		return pluginapi.RunningModel{}, fmt.Errorf("model path required")
	}

	// Reuse an already-running instance for the same model. LoRA adapters are
	// fixed when llama-server starts, so an instance without the requested
	// adapters is replaced.
	wantAdapters := adapterIDs(cfg.Adapters)
	if running, err := r.ListRunning(ctx); err == nil {
		for _, m := range running {
			if m.ModelID != cfg.ModelID || m.Status != "running" {
				continue
			}
			if m.Mode == cfg.Mode && sameAdapters(m.Adapters, wantAdapters) {
				return m, nil
			}
			_ = r.StopModel(ctx, m.ID)
		}
	}
	for _, a := range cfg.Adapters {
		if _, err := os.Stat(a.Path); err != nil {
			return pluginapi.RunningModel{}, fmt.Errorf("adapter %s: %w", a.ID, err)
		}
	}

	port := cfg.Port
	if port == 0 {
		port = freePort()
	}
	instanceID := uuid.NewString()
	logPath := filepath.Join(r.logsDir, "llamacpp-"+instanceID+".log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return pluginapi.RunningModel{}, err
	}

	args := startArgs(cfg, port)
	gpuLayers := cfg.GPULayers
	if gpuLayers == 0 {
		gpuLayers = defaultGPULayers()
	}
	if gpuLayers > 0 {
		args = append(args, "-ngl", fmt.Sprintf("%d", gpuLayers))
	}
	for _, a := range cfg.Adapters {
		args = append(args, "--lora", a.Path)
	}
	if len(cfg.Adapters) > 0 {
		args = append(args, "--lora-init-without-apply")
	}

	cmd := exec.Command(det.Path, args...)
	cmd.Dir = filepath.Dir(det.Path)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	configureLlamaProcess(cmd)
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return pluginapi.RunningModel{}, fmt.Errorf("start llama-server: %w", err)
	}

	endpoint := fmt.Sprintf("http://127.0.0.1:%d", port)
	r.ensureSupervisor()
	r.sup.mu.Lock()
	r.sup.procs[instanceID] = &managedProcess{
		cmd:      cmd,
		endpoint: endpoint,
		modelID:  cfg.ModelID,
		adapters: wantAdapters,
		mode:     cfg.Mode,
		logFile:  logFile,
		logPath:  logPath,
		exited:   make(chan struct{}),
	}
	exited := r.sup.procs[instanceID].exited
	r.sup.mu.Unlock()
	registerAdapters(endpoint, wantAdapters)

	go r.sup.watch(instanceID)

	if err := waitReady(ctx, endpoint, 120*time.Second, exited); err != nil {
		_ = r.StopModel(ctx, instanceID)
		if ctx.Err() != nil {
			return pluginapi.RunningModel{}, err
		}
		return pluginapi.RunningModel{}, pluginapi.LoadFailed(err)
	}

	return pluginapi.RunningModel{
		ID:        instanceID,
		ModelID:   cfg.ModelID,
		Endpoint:  endpoint,
		Status:    "running",
		RuntimeID: runtimeID,
		Adapters:  wantAdapters,
		Mode:      cfg.Mode,
	}, nil
}

// startArgs are llama-server's model, address, and mode flags.
func startArgs(cfg pluginapi.ModelStartConfig, port int) []string {
	args := []string{
		"-m", cfg.ModelPath,
		"--host", "127.0.0.1",
		"--port", fmt.Sprintf("%d", port),
	}
	switch cfg.Mode {
	case pluginapi.ModeEmbedding, pluginapi.ModeReranking:
		// Embedding and reranking read each input in one batch, so the batch
		// is as large as the window. Supporting models are small; a short
		// window keeps them light.
		n := cfg.Context
		if n <= 0 || n > supportContext {
			n = supportContext
		}
		flag := "--embedding"
		if cfg.Mode == pluginapi.ModeReranking {
			flag = "--reranking"
		}
		return append(args, flag,
			"--ctx-size", fmt.Sprintf("%d", n),
			"--batch-size", fmt.Sprintf("%d", n),
			"--ubatch-size", fmt.Sprintf("%d", n))
	}
	return append(args, "--ctx-size", fmt.Sprintf("%d", defaultContext(cfg.Context)))
}

// supportContext is the window for embedding and reranking models. Mimir
// passages are about 1,200 characters, well within it.
const supportContext = 2048

func (r *Runtime) StopModel(ctx context.Context, id string) error {
	r.ensureSupervisor()
	r.sup.mu.Lock()
	p, ok := r.sup.procs[id]
	if ok {
		p.intentional = true
		delete(r.sup.procs, id)
	}
	r.sup.mu.Unlock()
	if !ok {
		return fmt.Errorf("instance %q not found", id)
	}
	stopLlamaProcess(p.cmd, 1500*time.Millisecond)
	registerAdapters(p.endpoint, nil)
	if p.logFile != nil {
		_ = p.logFile.Close()
	}
	return nil
}

func (r *Runtime) ListRunning(ctx context.Context) ([]pluginapi.RunningModel, error) {
	r.ensureSupervisor()
	r.sup.mu.Lock()
	defer r.sup.mu.Unlock()
	out := make([]pluginapi.RunningModel, 0, len(r.sup.procs))
	for id, p := range r.sup.procs {
		status := "running"
		if p.cmd.ProcessState != nil && p.cmd.ProcessState.Exited() {
			status = "exited"
		}
		out = append(out, pluginapi.RunningModel{
			ID:        id,
			ModelID:   p.modelID,
			Endpoint:  p.endpoint,
			Status:    status,
			RuntimeID: runtimeID,
			Adapters:  append([]string(nil), p.adapters...),
			Mode:      p.mode,
		})
	}
	return out, nil
}

func (r *Runtime) Health(ctx context.Context) error {
	det, err := r.Detect(ctx)
	if err != nil {
		return err
	}
	if !det.Installed {
		return contracts.Errorf("RUNTIME_NOT_INSTALLED", map[string]any{"runtime": "llama-server"}, "llama-server not installed: %s", det.Message)
	}
	return nil
}

func (ps *ProcessSupervisor) watch(instanceID string) {
	ps.mu.Lock()
	p, ok := ps.procs[instanceID]
	ps.mu.Unlock()
	if !ok {
		return
	}
	err := p.cmd.Wait()
	code := 0
	if p.cmd.ProcessState != nil {
		code = p.cmd.ProcessState.ExitCode()
	}
	if err != nil && code == 0 {
		code = 1
	}
	if p.exited != nil {
		close(p.exited)
	}
	tail := tailFile(p.logPath, 800)
	ps.mu.Lock()
	intentional := p.intentional
	onExit := ps.onExit
	modelID := p.modelID
	if cur, ok := ps.procs[instanceID]; ok && cur == p {
		delete(ps.procs, instanceID)
	}
	ps.mu.Unlock()
	registerAdapters(p.endpoint, nil)
	if p.logFile != nil {
		_ = p.logFile.Close()
	}
	if !intentional && onExit != nil {
		onExit(instanceID, modelID, code, tail)
	}
}

// Probe asks llama-server whether it is still answering. It does not run inference.
func Probe(ctx context.Context, endpoint string) error {
	client := &http.Client{Timeout: 3 * time.Second}
	base := strings.TrimRight(endpoint, "/")
	if err := probeURL(ctx, client, base+"/health"); err == nil {
		return nil
	}
	return probeURL(ctx, client, base+"/v1/models")
}

func probeURL(ctx context.Context, client *http.Client, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

func tailFile(path string, limit int) string {
	if path == "" || limit <= 0 {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return ""
	}
	start := int64(0)
	if info.Size() > int64(limit) {
		start = info.Size() - int64(limit)
	}
	buf := make([]byte, info.Size()-start)
	_, _ = f.ReadAt(buf, start)
	return string(buf)
}

func freePort() int {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 8080
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func defaultContext(c int) int {
	if c <= 0 {
		return 8192
	}
	return c
}

// ContextWindow is the token window a local model is actually running with.
// llama-server is started at 8192 unless a smaller catalog context is set.
// The model-fit estimator uses the same window (models runningContextTokens).
func ContextWindow(catalog int) int {
	running := defaultContext(0)
	if catalog <= 0 || catalog > running {
		return running
	}
	return catalog
}

func defaultGPULayers() int {
	// Prefer GPU offload when available; llama-server ignores unsupported -ngl.
	switch runtime.GOOS {
	case "darwin":
		return 99
	default:
		return 99
	}
}

// errExitedWhileLoading is returned when llama-server ends before it is
// ready, for example because the model file is damaged or does not fit.
var errExitedWhileLoading = errors.New("llama-server stopped while loading the model")

// waitReady polls until llama-server answers. It returns at once if the
// process exits, instead of waiting out the timeout.
func waitReady(ctx context.Context, endpoint string, timeout time.Duration, exited <-chan struct{}) error {
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-exited:
			return errExitedWhileLoading
		default:
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/health", nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		// Fallback for builds without /health.
		req2, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/v1/models", nil)
		resp2, err2 := client.Do(req2)
		if err2 == nil {
			_, _ = io.Copy(io.Discard, resp2.Body)
			resp2.Body.Close()
			if resp2.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-exited:
			return errExitedWhileLoading
		case <-time.After(200 * time.Millisecond):
		}
	}
	return fmt.Errorf("llama-server did not become ready at %s", endpoint)
}
