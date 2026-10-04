package speech

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/remotetools"
)

// TranscribeTool is speech.transcribe: what an audio file in this chat says.
type TranscribeTool struct {
	Engine *Engine
	Store  *artifacts.Store
}

func (t *TranscribeTool) ID() string                     { return "speech.transcribe" }
func (t *TranscribeTool) DisplayName() string            { return "Transcribe Audio" }
func (t *TranscribeTool) Description() string            { return "Transcribe an audio file in this chat" }
func (t *TranscribeTool) Available() (bool, string)      { return t.Engine.Available() }
func (t *TranscribeTool) Provider() remotetools.Provider { return t.Engine.provider(t.ID(), "Whisper") }
func (t *TranscribeTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	return remotetools.Execute(ctx, t, args)
}

// maxTranscriptRunes caps the transcript handed back to the model.
const maxTranscriptRunes = 40_000

// Prepare reads the audio file from the chat.
func (t *TranscribeTool) Prepare(ctx context.Context, args map[string]any) (remotetools.Job, error) {
	ref, _ := args["file"].(string)
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return remotetools.Job{}, fmt.Errorf("file required: the name of an audio file in this chat")
	}
	a, data, err := findAudio(ctx, t.Store, ref)
	if err != nil {
		return remotetools.Job{}, err
	}
	return remotetools.Job{Args: args, Files: []remotetools.File{{Name: a.Name, Data: data}}}, nil
}

// Run transcribes it.
func (t *TranscribeTool) Run(ctx context.Context, job remotetools.Job) (remotetools.Output, error) {
	if len(job.Files) != 1 {
		return remotetools.Output{}, fmt.Errorf("a transcription needs one audio file")
	}
	f := job.Files[0]
	quality, _ := job.Args["quality"].(string)
	language, _ := job.Args["language"].(string)
	tr, err := t.Engine.Transcribe(ctx, f.Name, f.Data, quality, strings.ToLower(strings.TrimSpace(language)))
	if err != nil {
		return remotetools.Output{}, err
	}
	text := tr.Text
	truncated := false
	if utf8.RuneCountInString(text) > maxTranscriptRunes {
		text, truncated = string([]rune(text)[:maxTranscriptRunes]), true
	}
	out := map[string]any{"file": f.Name, "language": tr.Language, "duration_seconds": tr.Duration, "text": text}
	if truncated {
		out["truncated"] = true
	} else {
		out["segments"] = tr.Segments
	}
	return remotetools.Output{Result: out}, nil
}

// Finish returns the transcript; nothing is saved.
func (t *TranscribeTool) Finish(_ context.Context, out remotetools.Output) (map[string]any, error) {
	return out.Result, nil
}

// findAudio returns an audio file by id, or the newest one in this chat with
// that name.
func findAudio(ctx context.Context, store *artifacts.Store, ref string) (artifacts.Artifact, []byte, error) {
	if a, data, err := store.Read(ctx, ref); err == nil && artifacts.IsAudio(a.Name) {
		return a, data, nil
	}
	list, err := store.List(ctx, artifacts.ConversationFrom(ctx))
	if err != nil {
		return artifacts.Artifact{}, nil, err
	}
	for i := len(list) - 1; i >= 0; i-- {
		if strings.EqualFold(list[i].Name, ref) && artifacts.IsAudio(list[i].Name) {
			return store.Read(ctx, list[i].ID)
		}
	}
	return artifacts.Artifact{}, nil, fmt.Errorf("no audio file named %q in this chat", ref)
}

// SynthesizeTool is speech.synthesize: read text aloud as an audio file.
type SynthesizeTool struct {
	Engine *Engine
	Store  *artifacts.Store
}

func (t *SynthesizeTool) ID() string                     { return "speech.synthesize" }
func (t *SynthesizeTool) DisplayName() string            { return "Read Aloud" }
func (t *SynthesizeTool) Description() string            { return "Read text aloud as an audio file" }
func (t *SynthesizeTool) Available() (bool, string)      { return t.Engine.Available() }
func (t *SynthesizeTool) Provider() remotetools.Provider { return t.Engine.provider(t.ID(), "Piper") }
func (t *SynthesizeTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	return remotetools.Execute(ctx, t, args)
}

// Prepare needs no chat files.
func (t *SynthesizeTool) Prepare(_ context.Context, args map[string]any) (remotetools.Job, error) {
	return remotetools.Job{Args: args}, nil
}

// Run reads the text aloud.
func (t *SynthesizeTool) Run(ctx context.Context, job remotetools.Job) (remotetools.Output, error) {
	text, _ := job.Args["text"].(string)
	voice, _ := job.Args["voice"].(string)
	name, _ := job.Args["name"].(string)
	language, _ := job.Args["language"].(string)
	data, seconds, err := t.Engine.SynthesizeIn(ctx, text, voice, strings.TrimSpace(language))
	if err != nil {
		return remotetools.Output{}, err
	}
	return remotetools.Output{Result: map[string]any{"seconds": seconds}, Files: []remotetools.File{{Name: speechName(name), Data: data}}}, nil
}

// Finish attaches the audio to the chat.
func (t *SynthesizeTool) Finish(ctx context.Context, out remotetools.Output) (map[string]any, error) {
	if len(out.Files) != 1 {
		return nil, fmt.Errorf("no audio came back")
	}
	f := out.Files[0]
	a, err := t.Store.Save(ctx, artifacts.Input{ConversationID: artifacts.ConversationFrom(ctx), Name: speechName(f.Name), Producer: artifacts.ProducerAssistant, Data: f.Data})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"id": a.ID, "name": a.Name, "kind": a.Kind, "seconds": out.Result["seconds"],
		"note": "The audio is attached to your answer, where it can be played. Do not repeat the text.",
	}, nil
}

// speechName is the file name for read-aloud audio: a .wav named as asked.
func speechName(name string) string {
	name = artifacts.CleanName(strings.TrimSpace(name))
	if name == "" || name == "file" {
		name = "speech"
	}
	if !strings.HasSuffix(strings.ToLower(name), ".wav") {
		name = strings.TrimSuffix(name, ".mp3") + ".wav"
	}
	return name
}

// SaveSpeech reads text aloud and keeps the audio as a file in this chat.
func SaveSpeech(ctx context.Context, engine *Engine, store *artifacts.Store, text, voice, name string) (artifacts.Artifact, float64, error) {
	data, seconds, err := engine.Synthesize(ctx, text, voice)
	if err != nil {
		return artifacts.Artifact{}, 0, err
	}
	a, err := store.Save(ctx, artifacts.Input{ConversationID: artifacts.ConversationFrom(ctx), Name: speechName(name), Producer: artifacts.ProducerAssistant, Data: data})
	return a, seconds, err
}
