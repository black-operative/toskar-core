// Package speech transcribes audio and reads text aloud on this computer
// (Gungnir §18–19): Whisper (faster-whisper) for speech to text and Piper for
// text to speech, in a managed Python environment. Audio never leaves the
// computer; the models are downloaded from Hugging Face the first time they
// are used.
package speech

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/toskar-core/internal/pyenv"
	"github.com/yeixio/toskar-core/internal/remotetools"
)

//go:embed transcribe.py
var transcribeScript []byte

//go:embed synthesize.py
var synthesizeScript []byte

// Requirements are the speech environment's packages. PyAV is pinned
// because 19 removed an argument faster-whisper 1.2.1 passes.
var Requirements = []string{"faster-whisper==1.2.1", "av==18.0.0", "piper-tts==1.8.0"}

// Spec is the managed Python environment speech runs in.
func Spec() pyenv.Spec { return pyenv.Spec{Name: "speech", Requirements: Requirements} }

// Whisper models by quality. base is about 145 MB, small about 480 MB.
var models = map[string]string{"fast": "base", "accurate": "small"}

// DefaultVoice is Piper's voice when none is chosen.
const DefaultVoice = "en_US-lessac-medium"

// voiceRe is a Piper voice name, such as de_DE-thorsten-medium.
var voiceRe = regexp.MustCompile(`^[a-z]{2,3}_[A-Z]{2}-[a-z0-9_]+-(x_low|low|medium|high)$`)

// languageRe is a Whisper language code, such as de.
var languageRe = regexp.MustCompile(`^[a-z]{2,3}$`)

// Limits.
const (
	maxAudioBytes = 25 << 20
	maxTextRunes  = 5000
	// Each run allows for installing the environment and downloading a
	// model the first time.
	runLimit = 15 * time.Minute
)

// PythonEnv provides the environment. *pyenv.Manager implements it.
type PythonEnv interface {
	Ensure(ctx context.Context, spec pyenv.Spec, progress pyenv.Progress) (string, error)
	Env() []string
	Unavailable(spec pyenv.Spec) string
}

// Engine runs Whisper and Piper.
type Engine struct {
	Python PythonEnv
	// Dir holds the downloaded models and each job's files.
	Dir string

	// mu runs one job at a time; both use every core.
	mu sync.Mutex
}

// Available reports whether speech can run here, and why not.
func (e *Engine) Available() (bool, string) {
	if why := e.Python.Unavailable(Spec()); why != "" {
		return false, why
	}
	return true, ""
}

// provider describes this computer's speech provider (Gungnir §16). Speech
// runs on the CPU.
func (e *Engine) provider(tool, name string) remotetools.Provider {
	if ok, why := e.Available(); !ok {
		return remotetools.Provider{Tool: tool, Name: name, State: remotetools.Unavailable, Reason: why}
	}
	p := remotetools.Provider{Tool: tool, Name: name, State: remotetools.Healthy}
	// The languages each works in (multilingual spec §20).
	switch tool {
	case "speech.transcribe":
		p.Languages, p.AutoDetect = whisperLanguages, true
	case "speech.synthesize":
		p.Languages = voiceLanguages()
	}
	return p
}

// Segment is a timed part of a transcript.
type Segment struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

// Transcript is what an audio file says.
type Transcript struct {
	Language string    `json:"language"`
	Duration float64   `json:"duration"`
	Text     string    `json:"text"`
	Segments []Segment `json:"segments"`
}

// run runs a script with a JSON config in a job folder and decodes its output.
func (e *Engine) run(ctx context.Context, script []byte, cfg map[string]any, files map[string][]byte, out any) error {
	py, err := e.Python.Ensure(ctx, Spec(), nil)
	if errors.Is(err, pyenv.ErrSandboxed) {
		return fmt.Errorf("speech is not included in this copy of Toskar: %w", err)
	}
	if err != nil {
		return fmt.Errorf("speech could not be installed: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(e.Dir, "jobs"), 0o700); err != nil {
		return err
	}
	job, err := os.MkdirTemp(filepath.Join(e.Dir, "jobs"), "job-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(job)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(job, name), data, 0o600); err != nil {
			return err
		}
		cfg[strings.TrimSuffix(name, filepath.Ext(name))] = filepath.Join(job, name)
	}
	if v, ok := cfg["out"].(string); ok {
		cfg["out"] = filepath.Join(job, v)
	}
	raw, _ := json.Marshal(cfg)
	cfgPath := filepath.Join(job, "config.json")
	scriptPath := filepath.Join(job, "script.py")
	if err := os.WriteFile(cfgPath, raw, 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(scriptPath, script, 0o600); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, runLimit)
	defer cancel()
	cmd := exec.CommandContext(ctx, py, "-u", scriptPath, cfgPath)
	cmd.Dir = job
	cmd.Env = append(os.Environ(), e.Python.Env()...)
	// Hugging Face's own files (logs, caches) stay with the models, not in
	// the user's home folder.
	cmd.Env = append(cmd.Env, "HF_HOME="+filepath.Join(e.Dir, "hf"), "HF_HUB_DISABLE_TELEMETRY=1", "HF_HUB_DISABLE_PROGRESS_BARS=1")
	cmd.WaitDelay = 5 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("speech took longer than %d minutes and was stopped", int(runLimit.Minutes()))
		}
		return fmt.Errorf("speech failed: %s", lastLine(stderr.String(), err))
	}
	if err := json.Unmarshal(stdout.Bytes(), out); err != nil {
		return fmt.Errorf("speech returned something unexpected: %w", err)
	}
	if v, ok := cfg["out"].(string); ok {
		data, err := os.ReadFile(v)
		if err != nil {
			return err
		}
		if b, ok := out.(*audioResult); ok {
			b.data = data
		}
	}
	return nil
}

func lastLine(stderr string, err error) string {
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	if s := strings.TrimSpace(lines[len(lines)-1]); s != "" {
		if len(s) > 300 {
			s = s[:300]
		}
		return s
	}
	return err.Error()
}

// Transcribe returns what an audio file says. quality is fast (the
// default) or accurate; language is an ISO code such as "de", or empty to
// detect it.
func (e *Engine) Transcribe(ctx context.Context, name string, audio []byte, quality, language string) (Transcript, error) {
	if len(audio) > maxAudioBytes {
		return Transcript{}, fmt.Errorf("%s is larger than %d MB", name, maxAudioBytes>>20)
	}
	model := models[quality]
	if model == "" {
		model = models["fast"]
	}
	if language != "" && !languageRe.MatchString(language) {
		return Transcript{}, fmt.Errorf("language must be a code such as en or de")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	var t Transcript
	err := e.run(ctx, transcribeScript,
		map[string]any{"model": model, "models_dir": filepath.Join(e.Dir, "whisper"), "language": language},
		map[string][]byte{"audio" + strings.ToLower(filepath.Ext(name)): audio}, &t)
	return t, err
}

type audioResult struct {
	Seconds float64 `json:"seconds"`
	data    []byte
}

// Synthesize reads text aloud and returns a WAV file and its length.
//
// Without a voice, the voice is the one for the language the text is written
// in (multilingual spec §20), so a German answer is read by a German voice.
func (e *Engine) Synthesize(ctx context.Context, text, voice string) ([]byte, float64, error) {
	return e.SynthesizeIn(ctx, text, voice, "")
}

// SynthesizeIn is Synthesize with the text's language, when it is known.
func (e *Engine) SynthesizeIn(ctx context.Context, text, voice, language string) ([]byte, float64, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, 0, fmt.Errorf("text required")
	}
	if n := len([]rune(text)); n > maxTextRunes {
		return nil, 0, fmt.Errorf("the text is longer than %d characters", maxTextRunes)
	}
	voice, err := voiceFor(text, voice, language)
	if err != nil {
		return nil, 0, err
	}
	if !voiceRe.MatchString(voice) {
		return nil, 0, fmt.Errorf("%q is not a Piper voice name, such as %s", voice, DefaultVoice)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	var r audioResult
	err = e.run(ctx, synthesizeScript,
		map[string]any{"text": text, "voice": voice, "voices_dir": filepath.Join(e.Dir, "voices"), "out": "speech.wav"}, nil, &r)
	return r.data, r.Seconds, err
}
