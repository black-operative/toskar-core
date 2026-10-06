package speech

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/pyenv"
	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// fakePython is a stand-in interpreter: it ignores the script and answers
// as Whisper or Piper would, so the tests need no download.
const fakePython = `#!/usr/bin/env python3
import json, sys, wave
cfg = json.load(open(sys.argv[3]))
if "audio" in cfg:
    open(cfg["models_dir"] + ".seen", "w").write(cfg["model"] + " " + (cfg["language"] or "auto") + " " + open(cfg["audio"], "rb").read().decode())
    json.dump({"language": cfg["language"] or "en", "duration": 2.5, "text": "Hello there. Buy milk.",
               "segments": [{"start": 0, "end": 1.2, "text": "Hello there."}, {"start": 1.2, "end": 2.5, "text": "Buy milk."}]}, sys.stdout)
else:
    with wave.open(cfg["out"], "wb") as w:
        w.setnchannels(1); w.setsampwidth(2); w.setframerate(16000); w.writeframes(b"\0\0" * 8000)
    json.dump({"seconds": 0.5}, sys.stdout)
`

type fakeEnv struct{ path string }

func (f fakeEnv) Ensure(context.Context, pyenv.Spec, pyenv.Progress) (string, error) {
	return f.path, nil
}
func (f fakeEnv) Env() []string                 { return nil }
func (f fakeEnv) Unavailable(pyenv.Spec) string { return "" }

func setup(t *testing.T) (*Engine, *artifacts.Store, context.Context) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is not installed")
	}
	dir := t.TempDir()
	py := filepath.Join(dir, "python")
	if err := os.WriteFile(py, []byte(fakePython), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.SQL.Exec(`INSERT INTO conversations (id, title, created_at, updated_at) VALUES ('c1', 't', '2026-01-01', '2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	st := artifacts.NewStore(db.SQL, filepath.Join(dir, "artifacts"))
	return &Engine{Python: fakeEnv{py}, Dir: filepath.Join(dir, "speech")}, st, artifacts.WithConversation(context.Background(), "c1")
}

// An audio file in the chat is transcribed with the chosen quality and
// language (Gungnir §18).
func TestTranscribeTool(t *testing.T) {
	eng, st, ctx := setup(t)
	if _, err := st.Save(ctx, artifacts.Input{ConversationID: "c1", Name: "memo.m4a", Producer: artifacts.ProducerUser, Data: []byte("AUDIO")}); err != nil {
		t.Fatal(err)
	}
	tool := &TranscribeTool{Engine: eng, Store: st}
	res, err := tool.Execute(ctx, map[string]any{"file": "memo.m4a", "quality": "accurate", "language": "DE"})
	if err != nil {
		t.Fatal(err)
	}
	if res["text"] != "Hello there. Buy milk." || res["language"] != "de" || res["duration_seconds"] != 2.5 {
		t.Fatalf("result %v", res)
	}
	if segs, _ := res["segments"].([]Segment); len(segs) != 2 || segs[1].Text != "Buy milk." {
		t.Fatalf("segments %v", res["segments"])
	}
	seen, _ := os.ReadFile(filepath.Join(eng.Dir, "whisper.seen"))
	if string(seen) != "small de AUDIO" {
		t.Fatalf("the model saw %q", seen)
	}
	for _, bad := range []map[string]any{{"file": "missing.mp3"}, {"file": ""}, {"file": "memo.m4a", "language": "german"}} {
		if _, err := tool.Execute(ctx, bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
	if _, err := st.Save(ctx, artifacts.Input{ConversationID: "c1", Name: "notes.txt", Producer: artifacts.ProducerUser, Data: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(ctx, map[string]any{"file": "notes.txt"}); err == nil {
		t.Fatal("a text file was transcribed")
	}
}

// Text is read aloud as a WAV file attached to the chat (§19).
func TestSynthesizeTool(t *testing.T) {
	eng, st, ctx := setup(t)
	tool := &SynthesizeTool{Engine: eng, Store: st}
	res, err := tool.Execute(ctx, map[string]any{"text": "Your report is ready.", "name": "summary"})
	if err != nil {
		t.Fatal(err)
	}
	if res["name"] != "summary.wav" || res["kind"] != "audio" || res["seconds"] != 0.5 {
		t.Fatalf("result %v", res)
	}
	a, data, err := st.Read(ctx, res["id"].(string))
	if err != nil || a.MimeType != "audio/wav" || !strings.HasPrefix(string(data), "RIFF") {
		t.Fatalf("audio %+v %v", a, err)
	}
	for _, bad := range []map[string]any{{"text": ""}, {"text": "hi", "voice": "../../etc/passwd"}, {"text": strings.Repeat("a", maxTextRunes+1)}} {
		if _, err := tool.Execute(ctx, bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}

func TestVoiceNames(t *testing.T) {
	for v, ok := range map[string]bool{"en_US-lessac-medium": true, "de_DE-thorsten-high": true, "en_GB-alba-x_low": true, "lessac": false, "en_US-lessac-medium.onnx": false} {
		if voiceRe.MatchString(v) != ok {
			t.Errorf("%s: %v", v, !ok)
		}
	}
}

// Read-aloud speaks a text's language (multilingual spec §20).
func TestVoiceFollowsTheLanguage(t *testing.T) {
	cases := []struct{ text, voice, language, want string }{
		{"Kannst du mir bitte erklären, wie das funktioniert?", "", "", "de_DE-thorsten-medium"},
		{"¿Puedes explicarme cómo funciona esto, por favor?", "", "", "es_ES-davefx-medium"},
		{"请解释一下这个是怎么工作的。", "", "", "zh_CN-huayan-medium"},
		{"ok", "", "", DefaultVoice}, // too short to tell
		{"Kannst du mir bitte helfen?", "en_GB-alba-medium", "", "en_GB-alba-medium"}, // a voice asked for
		{"Hello there, how are you doing today?", "", "pt-BR", "pt_BR-faber-medium"},  // a language asked for
	}
	for _, c := range cases {
		if got, err := voiceFor(c.text, c.voice, c.language); err != nil || got != c.want {
			t.Errorf("voiceFor(%q, %q, %q) = %q, %v; want %q", c.text, c.voice, c.language, got, err, c.want)
		}
	}
	_, err := voiceFor("これがどのように動作するか説明してください。", "", "")
	if code, params := contracts.ErrorCode(err); code != "SPEECH_NO_VOICE" || params["language"] != "ja" {
		t.Fatalf("Japanese: %v (%s %v)", err, code, params)
	}
	for lang, v := range voices {
		if !voiceRe.MatchString(v) {
			t.Errorf("%s voice %q is not a Piper voice name", lang, v)
		}
	}
}

func TestProvidersSayTheirLanguages(t *testing.T) {
	e := &Engine{Python: fakeEnv{}}
	tr := e.provider("speech.transcribe", "Whisper")
	if !tr.AutoDetect || !tr.Speaks("ja") || !tr.Speaks("de") {
		t.Errorf("transcribe = %+v", tr)
	}
	sy := e.provider("speech.synthesize", "Piper")
	if sy.AutoDetect || !sy.Speaks("de") || sy.Speaks("ja") {
		t.Errorf("synthesize languages = %v", sy.Languages)
	}
}

type failingEnv struct{ fakeEnv }

func (failingEnv) Ensure(context.Context, pyenv.Spec, pyenv.Progress) (string, error) {
	return "", errors.New("create Python environment: fork/exec /Users/sam/Library/Containers/x/Data/uv: operation not permitted")
}

// A failed install carries a code the app shows as a plain message, never
// the path and Go error chain (#279).
func TestFailedInstallHasAPlainCode(t *testing.T) {
	e := &Engine{Python: failingEnv{}, Dir: t.TempDir()}
	_, _, err := e.Synthesize(context.Background(), "Hello", "")
	if code, _ := contracts.ErrorCode(err); code != "SPEECH_SETUP_FAILED" {
		t.Fatalf("code = %q (%v), want SPEECH_SETUP_FAILED", code, err)
	}
}
