package imagegen

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
)

// Setup installs stable-diffusion.cpp and image models, and knows which
// are ready.
type Setup struct {
	// ProgramDir holds the stable-diffusion.cpp builds (runtimes/sdcpp).
	ProgramDir string
	// ModelsDir holds one folder per image model (models/images).
	ModelsDir string
	HTTP      *http.Client
	// Memory returns this computer's memory, for the recommendation.
	Memory func() int64
	// FreeBytes returns the free space where models are kept; nil skips the
	// check.
	FreeBytes func() (int64, error)
	// Sandboxed refuses setup: the Mac App Store build cannot run a program
	// it downloads.
	Sandboxed bool
	// Changed is called when what is installed changes: a setup ended or a
	// model was removed.
	Changed func()
	// Catalog lists the models this setup offers; nil is the image models.
	Catalog func() []Model
	// archive overrides this platform's build; tests set it.
	archive *Archive
	// fileURL overrides where model files come from; tests set it.
	fileURL func(File) string

	mu     sync.Mutex
	job    *job
	cancel context.CancelFunc
}

type job struct {
	model   string
	stage   string
	total   int64
	done    atomic.Int64
	running bool
	err     string
}

// Job is the setup in progress, or the last one.
type Job struct {
	ModelID string `json:"model_id"`
	// Stage is program or model.
	Stage string `json:"stage"`
	Done  int64  `json:"done_bytes"`
	Total int64  `json:"total_bytes"`
	// Running is false once it finished or failed.
	Running bool   `json:"running"`
	Error   string `json:"error,omitempty"`
}

// ModelStatus is a catalog model and whether it is here.
type ModelStatus struct {
	Model
	SizeBytes   int64 `json:"size_bytes"`
	Installed   bool  `json:"installed"`
	Recommended bool  `json:"recommended"`
}

// Status is what is set up.
type Status struct {
	// Supported is false where there is no build or setup cannot run;
	// Unsupported says why.
	Supported   bool   `json:"supported"`
	Unsupported string `json:"unsupported,omitempty"`
	// Ready means images can be made now.
	Ready bool `json:"ready"`
	// Program reports stable-diffusion.cpp installed.
	Program bool   `json:"program"`
	Release string `json:"release"`
	// Active is the model used for images.
	Active string        `json:"active,omitempty"`
	Models []ModelStatus `json:"models"`
	Job    *Job          `json:"job,omitempty"`
}

func (s *Setup) models() []Model {
	if s.Catalog != nil {
		return s.Catalog()
	}
	return Catalog()
}

func (s *Setup) lookup(id string) (Model, bool) { return lookupIn(s.models(), id) }

// programMu keeps two setups, such as images and video, from installing
// the same stable-diffusion.cpp at once.
var programMu sync.Mutex

func (s *Setup) platform() (Archive, bool) {
	if s.archive != nil {
		return *s.archive, true
	}
	return platformArchive()
}

func (s *Setup) unsupported() string {
	if s.Sandboxed {
		return "this copy of Toskar runs in the macOS App Sandbox, which cannot run a program it downloads"
	}
	if _, ok := s.platform(); !ok {
		return "stable-diffusion.cpp has no build for this kind of computer"
	}
	return ""
}

// Status reports what is installed and any setup in progress.
func (s *Setup) Status() Status {
	why := s.unsupported()
	st := Status{Supported: why == "", Unsupported: why, Release: Release, Program: s.CLI() != "", Active: s.ActiveModel()}
	st.Ready = st.Supported && st.Program && st.Active != ""
	var memory int64
	if s.Memory != nil {
		memory = s.Memory()
	}
	rec := recommendIn(s.models(), memory).ID
	for _, m := range s.models() {
		st.Models = append(st.Models, ModelStatus{Model: m, SizeBytes: m.SizeBytes(), Installed: s.installed(m.ID), Recommended: m.ID == rec})
	}
	s.mu.Lock()
	if j := s.job; j != nil {
		st.Job = &Job{ModelID: j.model, Stage: j.stage, Done: j.done.Load(), Total: j.total, Running: j.running, Error: j.err}
	}
	s.mu.Unlock()
	return st
}

// CLI returns the installed sd-cli, or "".
func (s *Setup) CLI() string {
	raw, err := os.ReadFile(filepath.Join(s.ProgramDir, Release, "cli"))
	if err != nil {
		return ""
	}
	path := filepath.Join(s.ProgramDir, Release, strings.TrimSpace(string(raw)))
	if st, err := os.Stat(path); err != nil || st.IsDir() {
		return ""
	}
	return path
}

func (s *Setup) modelDir(id string) string { return filepath.Join(s.ModelsDir, id) }

func (s *Setup) installed(id string) bool {
	_, err := os.Stat(filepath.Join(s.modelDir(id), "ready"))
	return err == nil
}

// ActiveModel is the model images are made with: the one chosen last, or
// the first installed one.
func (s *Setup) ActiveModel() string {
	if raw, err := os.ReadFile(filepath.Join(s.ModelsDir, "active")); err == nil {
		if id := strings.TrimSpace(string(raw)); s.installed(id) {
			return id
		}
	}
	for _, m := range s.models() {
		if s.installed(m.ID) {
			return m.ID
		}
	}
	return ""
}

// paths returns the active model and its files by role.
func (s *Setup) paths() (Model, map[string]string, error) {
	id := s.ActiveModel()
	m, ok := s.lookup(id)
	if !ok {
		return Model{}, nil, errors.New("no model is installed")
	}
	out := map[string]string{}
	for _, f := range m.Files {
		out[f.Role] = filepath.Join(s.modelDir(id), filepath.Base(f.Path))
	}
	return m, out, nil
}

// Start installs what the model needs, in the background. Status reports
// its progress.
func (s *Setup) Start(id string) error {
	if why := s.unsupported(); why != "" {
		return errors.New(why)
	}
	m, ok := s.lookup(id)
	if !ok {
		return fmt.Errorf("unknown model %q", id)
	}
	need := int64(0)
	if !s.installed(id) {
		need = m.SizeBytes()
	}
	archive, _ := s.platform()
	program := s.CLI() == ""
	if program {
		need += archive.Size * 3 // the archive and what it unpacks to
	}
	if s.FreeBytes != nil && need > 0 {
		// 0 means the space could not be measured.
		if free, err := s.FreeBytes(); err == nil && free > 0 && free < need+(1<<30) {
			return fmt.Errorf("setting up %s needs %.1f GB of free space; %.1f GB is free", m.Name, gb(need+(1<<30)), gb(free))
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.job != nil && s.job.running {
		return errors.New("image generation is already being set up")
	}
	j := &job{model: id, running: true}
	if program {
		j.total += archive.Size
	}
	if !s.installed(id) {
		j.total += m.SizeBytes()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.job, s.cancel = j, cancel
	go s.run(ctx, j, m, program, archive)
	return nil
}

func gb(n int64) float64 { return float64(n) / (1 << 30) }

func (s *Setup) run(ctx context.Context, j *job, m Model, program bool, archive Archive) {
	err := s.install(ctx, j, m, program, archive)
	if s.Changed != nil {
		defer s.Changed()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	j.running = false
	switch {
	case errors.Is(err, context.Canceled):
		j.err = "Setup was stopped. Starting it again picks up where it left off."
	case err != nil:
		j.err = err.Error()
	}
	s.cancel = nil
}

func (s *Setup) setStage(j *job, stage string) {
	s.mu.Lock()
	j.stage = stage
	s.mu.Unlock()
}

func (s *Setup) client() *http.Client {
	if s.HTTP != nil {
		return s.HTTP
	}
	return http.DefaultClient
}

func (s *Setup) install(ctx context.Context, j *job, m Model, program bool, archive Archive) error {
	add := func(n int64) { j.done.Add(n) }
	if program {
		s.setStage(j, "program")
		if err := s.installProgram(ctx, archive, add); err != nil {
			return fmt.Errorf("stable-diffusion.cpp could not be installed: %w", err)
		}
	}
	if !s.installed(m.ID) {
		s.setStage(j, "model")
		dir := s.modelDir(m.ID)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		for _, f := range m.Files {
			url := f.URL(revisions[f.Repo])
			if s.fileURL != nil {
				url = s.fileURL(f)
			}
			if err := download(ctx, s.client(), url, filepath.Join(dir, filepath.Base(f.Path)), f.Size, f.SHA256, add); err != nil {
				return fmt.Errorf("%s could not be downloaded: %w", f.Path, err)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, "ready"), []byte(m.ID+"\n"), 0o644); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(s.ModelsDir, "active"), []byte(m.ID+"\n"), 0o644)
}

func (s *Setup) installProgram(ctx context.Context, archive Archive, add func(int64)) error {
	programMu.Lock()
	defer programMu.Unlock()
	if s.CLI() != "" {
		// The other setup installed it meanwhile.
		add(archive.Size)
		return nil
	}
	dir := filepath.Join(s.ProgramDir, Release)
	if err := os.MkdirAll(s.ProgramDir, 0o755); err != nil {
		return err
	}
	zipPath := filepath.Join(s.ProgramDir, archive.Name)
	url := archive.URL()
	if s.archive != nil && s.fileURL != nil {
		url = s.fileURL(File{Path: archive.Name})
	}
	if err := download(ctx, s.client(), url, zipPath, archive.Size, archive.SHA256, add); err != nil {
		return err
	}
	_ = os.RemoveAll(dir)
	cli, err := extractZip(zipPath, dir)
	if err != nil {
		return err
	}
	_ = os.Remove(zipPath)
	rel, err := filepath.Rel(dir, cli)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "cli"), []byte(rel+"\n"), 0o644)
}

// Cancel stops the setup in progress; downloaded parts are kept.
func (s *Setup) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}

// Remove deletes an installed model to free its space.
func (s *Setup) Remove(id string) error {
	if _, ok := s.lookup(id); !ok {
		return fmt.Errorf("unknown model %q", id)
	}
	s.mu.Lock()
	busy := s.job != nil && s.job.running && s.job.model == id
	s.mu.Unlock()
	if busy {
		return errors.New("this model is being set up; stop the setup first")
	}
	err := os.RemoveAll(s.modelDir(id))
	if s.Changed != nil {
		s.Changed()
	}
	return err
}
