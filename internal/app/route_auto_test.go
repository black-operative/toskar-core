package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/huginn"
	"github.com/yeixio/toskar-core/internal/mimir"
	modelhealth "github.com/yeixio/toskar-core/internal/models/health"
	"github.com/yeixio/toskar-core/internal/muninn"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

func installed(id, name string, mem uint64) contracts.Model {
	return contracts.Model{ID: id, DisplayName: name, Installed: true, MemoryNeeded: mem,
		Tags: []string{"general"}, Purpose: []string{"general"}, Capabilities: contracts.ModelCapabilities{ToolCalling: true}}
}

func TestFallbackExplainsAndWarnsWhenSmaller(t *testing.T) {
	models := []contracts.Model{installed("big", "Qwen 14B", 12e9), installed("small", "Llama 1B", 1.6e9)}
	oom := modelhealth.Encode(modelhealth.Failure{Kind: "model_health", Reason: "runtime_error", LikelyMemoryPressure: true})
	next, step, notice, ok := fallbackFrom("en", "big", oom, models, 24e9)
	if !ok || next.ID != "small" {
		t.Fatalf("next = %+v %v", next, ok)
	}
	if step != "Qwen 14B ran out of memory, so Llama 1B answered instead" {
		t.Fatalf("step = %q", step)
	}
	if !strings.Contains(notice, "smaller Llama 1B") || !strings.Contains(notice, "less detailed") {
		t.Fatalf("notice = %q", notice)
	}
	// A model of the same size answers as well, so there is no notice.
	models = append(models, installed("peer", "Mistral 7B", 11e9))
	if _, _, notice, _ := fallbackFrom("en", "big", "llama-server exited", models, 24e9); notice != "" {
		t.Fatalf("notice for a peer = %q", notice)
	}
	if _, _, _, ok := fallbackFrom("en", "big", "x", models[:1], 24e9); ok {
		t.Fatal("nothing to fall back to")
	}
}

func TestRecoverableOnlyBeforeAnythingHappened(t *testing.T) {
	ctx := context.Background()
	env := &chatExecEnv{trace: &turnTrace{}}
	if !recoverable(ctx, "llama-server exited", "", env) {
		t.Fatal("a failure before any output should be retried")
	}
	if recoverable(ctx, "llama-server exited", "partial answer", env) {
		t.Fatal("a failure after output must not be retried")
	}
	if recoverable(ctx, "context canceled", "", env) {
		t.Fatal("a stopped turn must not be retried")
	}
	stopped, cancel := context.WithCancel(ctx)
	cancel()
	if recoverable(stopped, "llama-server exited", "", env) {
		t.Fatal("a cancelled turn must not be retried")
	}
	env.trace.tool("filesystem.write", map[string]any{"path": "notes.txt"}, nil)
	if recoverable(ctx, "llama-server exited", "", env) {
		t.Fatal("a turn that changed a file must not be repeated")
	}
}

func TestTraceCarriesRouteAndNotice(t *testing.T) {
	tr := &turnTrace{}
	tr.routed("Auto chose Qwen 7B for a coding question")
	tr.recovered("Qwen 7B stopped responding, so Llama 1B answered instead", "Qwen 7B could not answer…")
	meta := tr.meta()
	if meta == nil || len(meta.Steps) != 2 || meta.Steps[0].Kind != "route" || meta.Steps[1].Kind != "recover" || meta.Notice == "" {
		t.Fatalf("meta = %+v", meta)
	}
}

func TestAutoAvoidsAModelThatJustFailed(t *testing.T) {
	a := &App{}
	if a.recentlyFailed("big") {
		t.Fatal("nothing has failed yet")
	}
	a.noteModelFailed("big")
	if !a.recentlyFailed("big") || a.recentlyFailed("small") {
		t.Fatal("only the failed model is avoided")
	}
	a.failedModels.Store("big", time.Now().Add(-failedFor-time.Second))
	if a.recentlyFailed("big") {
		t.Fatal("a model is tried again after a while")
	}
}

func TestSmallModelNote(t *testing.T) {
	tiny := installed("llama-1b", "Llama 3.2 1B", 1.6e9)
	tiny.Parameters = "1B"
	big := installed("qwen-7b", "Qwen 2.5 7B", 6.4e9)
	big.Parameters = "7B"

	note := smallModelNote("en", "file", "llama-1b", []contracts.Model{tiny, big}, 24e9, false)
	if !strings.Contains(note, "Llama 3.2 1B is a small model") || !strings.Contains(note, "from your files") || !strings.Contains(note, "choose Qwen 2.5 7B or Auto") {
		t.Fatalf("note = %q", note)
	}
	// With nothing larger installed, point to the Models page.
	if note := smallModelNote("en", "knowledge", "llama-1b", []contracts.Model{tiny}, 24e9, false); !strings.Contains(note, "your knowledge") || !strings.Contains(note, "Models page") {
		t.Fatalf("note = %q", note)
	}
	// No data, or a model that is not small: no note.
	if smallModelNote("en", "", "llama-1b", []contracts.Model{tiny}, 24e9, false) != "" || smallModelNote("en", "file", "qwen-7b", []contracts.Model{tiny, big}, 24e9, false) != "" {
		t.Fatal("unexpected note")
	}
}

func TestTraceKnowsWhenDataWasUsed(t *testing.T) {
	tr := &turnTrace{}
	if tr.dataKind() != "" {
		t.Fatal("nothing used yet")
	}
	tr.knowledge([]mimir.Hit{{Title: "row 1", SourceName: "inventory.csv"}})
	if tr.dataKind() != "knowledge" {
		t.Fatal("knowledge")
	}
	tr.attachment(artifacts.Artifact{ID: "art-1", Name: "tires.xlsx", Producer: artifacts.ProducerAssistant}, 1, 1)
	if tr.dataKind() != "file" {
		t.Fatal("files outrank knowledge in the note")
	}
	// A file source names its file, so the app can save it (#281).
	if src := tr.meta().Sources; src[len(src)-1].ArtifactID != "art-1" {
		t.Fatalf("file source = %+v", src[len(src)-1])
	}
	tr.noticeIfNone("first")
	tr.noticeIfNone("second")
	if tr.meta().Notice != "first" {
		t.Fatal("an existing notice is kept")
	}
}

func TestTraceRecordsPlansAndChecks(t *testing.T) {
	tr := &turnTrace{}
	tr.planned(3, true)
	tr.verified(2, 1, []string{"20"})
	meta := tr.meta()
	if meta.Steps[0].Text != "Split the request into 3 parts and looked them up side by side" ||
		meta.Steps[1].Text != "Checked the figures; some could not be confirmed" ||
		meta.Notice != "Toskar could not confirm 20 in the sources. Check before relying on it." {
		t.Fatalf("meta = %+v", meta)
	}
	tr = &turnTrace{}
	tr.verified(1, 1, nil)
	tr.planned(2, false)
	if meta := tr.meta(); meta.Steps[0].Text != "Checked the figures and corrected 1 figure" || meta.Steps[1].Text != "Worked through the request in 2 parts" || meta.Notice != "" {
		t.Fatalf("meta = %+v", meta)
	}
}

// A profile's fallback order comes before Yggdrasil's own pick (§20).
func TestFallbackPrefersProfileOrder(t *testing.T) {
	models := []contracts.Model{installed("big", "Qwen 14B", 12e9), installed("mid", "Qwen 7B", 6e9), installed("tiny", "Llama 1B", 1.6e9)}
	next, _, _, ok := fallbackFrom("en", "big", "x", models, 24e9, "missing", "big", "tiny")
	if !ok || next.ID != "tiny" {
		t.Fatalf("next = %q, %v; want the profile's tiny", next.ID, ok)
	}
	next, _, _, _ = fallbackFrom("en", "big", "x", models, 24e9, "missing")
	if next.ID == "" || next.ID == "big" {
		t.Fatalf("without an installed preference, Yggdrasil picks: %q", next.ID)
	}
}

// Notices shown with an answer are in the App language (multilingual spec §16).
func TestNoticesInAppLanguage(t *testing.T) {
	tr := &turnTrace{lang: "de"}
	tr.verified(3, 0, []string{"20", "30", "$9"})
	if n := tr.meta().Notice; n != "Toskar konnte 20, 30 und $9 in den Quellen nicht bestätigen. Prüfe das, bevor du dich darauf verlässt." {
		t.Fatalf("notice = %q", n)
	}
	tr = &turnTrace{lang: "de"}
	tr.stopped(true, false)
	if n := tr.meta().Notice; n != "Gestoppt, bevor die Antwort fertig war." {
		t.Fatalf("notice = %q", n)
	}
	models := []contracts.Model{installed("big", "Qwen 14B", 12e9), installed("small", "Llama 1B", 1.6e9)}
	if _, _, notice, _ := fallbackFrom("de", "big", "x", models, 24e9); !strings.Contains(notice, "das kleinere Llama 1B") {
		t.Fatalf("notice = %q", notice)
	}
	tiny := installed("llama-1b", "Llama 3.2 1B", 1.6e9)
	tiny.Parameters = "1B"
	if note := smallModelNote("de", "knowledge", "llama-1b", []contracts.Model{tiny}, 24e9, false); !strings.Contains(note, "deinem Wissen") || !strings.Contains(note, "„Modelle“") {
		t.Fatalf("note = %q", note)
	}
}

// The steps listed with an answer are in the App language too.
func TestStepsInAppLanguage(t *testing.T) {
	tr := &turnTrace{lang: "de"}
	tr.knowledge([]mimir.Hit{{Title: "row 1", SourceName: "inventory.csv"}, {Title: "row 2", SourceName: "inventory.csv"}})
	tr.stopped(false, false)
	tr.memories([]muninn.Memory{{Content: "a"}, {Content: "b"}})
	steps := tr.meta().Steps
	for i, want := range []string{"inventory.csv", "Von dir", "2"} {
		if !strings.Contains(steps[i].Text, want) || strings.Contains(steps[i].Text, "Found") || strings.Contains(steps[i].Text, "Used") {
			t.Errorf("step %d = %q, want %q in it", i, steps[i].Text, want)
		}
	}
	models := []contracts.Model{installed("big", "Qwen 14B", 12e9), installed("small", "Llama 1B", 1.6e9)}
	if _, step, _, _ := fallbackFrom("de", "big", "x", models, 24e9); !strings.Contains(step, "Qwen 14B") || strings.Contains(step, "answered instead") {
		t.Fatalf("step = %q", step)
	}
	c := huginn.Choice{Model: models[0], Kind: huginn.Coding, Effort: huginn.EffortThorough, Lang: "es"}
	if r := c.Reason("de"); !strings.Contains(r, "Spanisch") || !strings.Contains(r, "Gründlich") {
		t.Fatalf("reason = %q", r)
	}
	if r := c.Reason("en"); r != "Auto chose Qwen 14B for a coding question in Spanish at Thorough effort" {
		t.Fatalf("reason = %q", r)
	}
}

func quantized(id, quant string, mem uint64) contracts.Model {
	m := installed(id, "Qwen 2.5 7B", mem)
	m.Variant = quant
	m.Source = contracts.ModelSource{URL: "https://huggingface.co/Qwen/Qwen2.5-7B-Instruct-GGUF/resolve/main/qwen2.5-7b-instruct-" + strings.ToLower(quant) + ".gguf"}
	return m
}

func TestFallbackTriesAnotherQuantizationFirst(t *testing.T) {
	q8, q5, q4 := quantized("qwen-7b-q8", "Q8_0", 9e9), quantized("qwen-7b-q5", "Q5_K_M", 6e9), quantized("qwen-7b-q4", "Q4_K_M", 5e9)
	other := installed("mistral-7b", "Mistral 7B", 6e9)
	models := []contracts.Model{q8, q5, q4, other}
	oom := modelhealth.Encode(modelhealth.Failure{Kind: "model_health", Reason: "oom", LikelyMemoryPressure: true})

	// Out of memory: the largest smaller version that fits, before another model.
	next, step, notice, ok := fallbackFrom("en", "qwen-7b-q8", oom, models, 24e9)
	if !ok || next.ID != "qwen-7b-q5" {
		t.Fatalf("next = %s %v", next.ID, ok)
	}
	if step != "Qwen 2.5 7B at Q8_0 ran out of memory, so a smaller version at Q5_K_M answered instead" {
		t.Fatalf("step = %q", step)
	}
	if !strings.Contains(notice, "(Q5_K_M instead of Q8_0)") {
		t.Fatalf("notice = %q", notice)
	}
	// Any other failure: the largest other version that fits, even a larger one, with no notice.
	next, step, notice, ok = fallbackFrom("en", "qwen-7b-q4", "llama-server exited", models, 24e9)
	if !ok || next.ID != "qwen-7b-q8" || notice != "" || !strings.Contains(step, "the same model at Q8_0") {
		t.Fatalf("crash: %s %q %q %v", next.ID, step, notice, ok)
	}
	// Out of memory with no smaller version: another model, never a larger
	// version of the same one.
	next, _, _, ok = fallbackFrom("en", "qwen-7b-q4", oom, models, 24e9)
	if !ok || next.ID != "mistral-7b" {
		t.Fatalf("no smaller version: %s %v", next.ID, ok)
	}
	// A version too big for this computer is passed over.
	if next, _, _, _ := fallbackFrom("en", "qwen-7b-q4", "llama-server exited", models, 10e9); next.ID == "qwen-7b-q8" {
		t.Fatal("a version that doesn't fit was chosen")
	}
	// A different model with a similar name is not another quantization.
	if _, _, _, ok := otherQuantization("mistral-7b", models, 24e9, false); ok {
		t.Fatal("a model without a Hugging Face source matched")
	}
}
