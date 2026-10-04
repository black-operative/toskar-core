package app

import (
	"context"
	"strings"
	"testing"
	"time"

	modelhealth "github.com/yeixio/toskar-core/internal/models/health"
	"github.com/yeixio/toskar-core/internal/share"
)

func TestChatExplainsTrainingOnThisComputer(t *testing.T) {
	a := &App{Share: share.New(0)}
	if _, ok := a.trainingStep("en"); ok {
		t.Fatal("training reported with none running")
	}
	oom := modelhealth.Encode(modelhealth.Failure{LikelyMemoryPressure: true, ModelID: "big"})
	if got := a.explainFailure("en", oom); got != oom {
		t.Fatal("error rewritten with no training running")
	}

	w, err := a.Share.Enter(context.Background(), share.Training, "Tire shop", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Done()
	if step, _ := a.trainingStep("en"); step != `Training “Tire shop” is using this computer, so this answer may be slower.` {
		t.Fatalf("step = %q", step)
	}
	w.SetRemaining(12*time.Minute + 10*time.Second)
	if step, _ := a.trainingStep("en"); step != `Training “Tire shop” is using this computer, so this answer may be slower. About 12 minutes left.` {
		t.Fatalf("step = %q", step)
	}

	// A memory failure keeps its structure but says why and when to retry.
	got := a.explainFailure("en", oom)
	f, isHealth := modelhealth.Parse(got)
	if !isHealth || f.ModelID != "big" || f.Message != `Training “Tire shop” is using this computer, so there was not enough memory for this model. About 12 minutes left. Try again when training finishes, choose a smaller model, or cancel training on the Train page.` {
		t.Fatalf("explained = %q", got)
	}
	// A plain error becomes a memory failure, so clients show the sentence.
	f, isHealth = modelhealth.Parse(a.explainFailure("en", "llama-server: failed to allocate buffer"))
	if !isHealth || !f.LikelyMemoryPressure || !strings.Contains(f.Message, "Try again when training finishes") {
		t.Fatalf("plain error = %+v", f)
	}
	// In the App language.
	f, _ = modelhealth.Parse(a.explainFailure("de", oom))
	if f.Message != `Das Training „Tire shop“ nutzt diesen Computer, deshalb war nicht genug Speicher für dieses Modell frei. Noch etwa 12 Minuten. Versuche es erneut, wenn das Training fertig ist, wähle ein kleineres Modell oder brich das Training auf der Seite „Trainieren“ ab.` {
		t.Fatalf("German = %q", f.Message)
	}
	// Other failures are left alone.
	if got := a.explainFailure("en", "connection refused"); got != "connection refused" {
		t.Fatalf("unrelated error = %q", got)
	}
}

func TestTimeLeft(t *testing.T) {
	g := share.New(0)
	w, _ := g.Enter(context.Background(), share.Training, "", nil)
	defer w.Done()
	if len(timeLeft(w)) != 0 {
		t.Fatal("no estimate yet")
	}
	for d, want := range map[time.Duration]string{
		20 * time.Second: "Less than a minute left.",
		70 * time.Second: "About 1 minute left.",
		3 * time.Hour:    "About 3 hours left.",
	} {
		w.SetRemaining(d)
		if got := timeLeft(w)[0].Render("en"); got != want {
			t.Errorf("%v: %q, want %q", d, got, want)
		}
	}
}

// A model failure's generic sentence is shown in the App language; one that
// already says more is kept.
func TestModelFailureInAppLanguage(t *testing.T) {
	a := &App{Share: share.New(0)}
	stopped := modelhealth.Encode(modelhealth.Failure{Reason: modelhealth.ReasonProcessExit, ModelID: "big"})
	if got := a.explainFailure("en", stopped); got != stopped {
		t.Fatalf("English = %q", got)
	}
	f, _ := modelhealth.Parse(a.explainFailure("de", stopped))
	if f.Message != "Das Modell hat nicht mehr geantwortet. Toskar hat es deshalb gestoppt und den fehlgeschlagenen Prozess bereinigt." || f.ModelID != "big" {
		t.Fatalf("German = %+v", f)
	}
	oom := modelhealth.Encode(modelhealth.Failure{Reason: modelhealth.ReasonOOM, LikelyMemoryPressure: true})
	if f, _ := modelhealth.Parse(a.explainFailure("ja", oom)); !strings.Contains(f.Message, "メモリが不足") {
		t.Fatalf("Japanese = %q", f.Message)
	}
	custom := modelhealth.Encode(modelhealth.Failure{Reason: modelhealth.ReasonRuntimeError, Message: "Something specific happened."})
	if got := a.explainFailure("de", custom); got != custom {
		t.Fatalf("custom = %q", got)
	}
}
