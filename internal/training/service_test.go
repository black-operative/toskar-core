package training

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/mimir"
	"github.com/yeixio/toskar-core/internal/models"
	"github.com/yeixio/toskar-core/internal/pyenv"
	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

type fakePython struct {
	ensured   atomic.Int32
	sandboxed bool
}

func (f *fakePython) Status(pyenv.Spec) pyenv.Status {
	return pyenv.Status{Installed: f.ensured.Load() > 0}
}
func (f *fakePython) Ensure(ctx context.Context, spec pyenv.Spec, p pyenv.Progress) (string, error) {
	f.ensured.Add(1)
	return "/fake/python", nil
}
func (f *fakePython) Env() []string { return nil }
func (f *fakePython) Unavailable(pyenv.Spec) string {
	if f.sandboxed {
		return pyenv.ErrSandboxed.Error()
	}
	return ""
}

// fakeTrainer writes an adapter, or blocks until cancelled, or fails.
type fakeTrainer struct {
	mode    string // "ok", "block", "fail"
	started chan struct{}
	spec    RunSpec
}

func (f *fakeTrainer) ID() string                                          { return "mlx" }
func (f *fakeTrainer) DisplayName() string                                 { return "fake" }
func (f *fakeTrainer) Supports(contracts.HardwareInventory) (bool, string) { return true, "" }
func (f *fakeTrainer) Environment() pyenv.Spec                             { return pyenv.Spec{Name: "fake"} }
func (f *fakeTrainer) Run(ctx context.Context, spec RunSpec, update func(Update)) (RunResult, error) {
	f.spec = spec
	update(Update{State: StateLoading, Detail: "Loading"})
	update(Update{State: StateTraining, Detail: "Training", Progress: Progress{Iter: 1, Iters: spec.Hyper.Iters}})
	if f.started != nil {
		select {
		case f.started <- struct{}{}:
		default:
		}
	}
	switch f.mode {
	case "block":
		<-ctx.Done()
		_ = os.WriteFile(spec.AdapterOut, []byte("partial"), 0o644)
		return RunResult{}, ErrCancelled
	case "fail":
		return RunResult{}, errors.New("MemoryError: out of memory (log: x)")
	}
	loss := 0.12
	if err := os.WriteFile(spec.AdapterOut, []byte("gguf"), 0o644); err != nil {
		return RunResult{}, err
	}
	return RunResult{AdapterPath: spec.AdapterOut, TrainLoss: &loss}, nil
}

type generated struct {
	mu    sync.Mutex
	calls []string
}

func (g *generated) fn(ctx context.Context, base, adapter string, loaded []pluginapi.Adapter, msgs []pluginapi.ChatMessage) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	ids := []string{}
	for _, a := range loaded {
		ids = append(ids, a.ID)
	}
	g.calls = append(g.calls, fmt.Sprintf("%s|%s|%v|%s", base, adapter, ids, msgs[0].Content[:min(20, len(msgs[0].Content))]))
	if adapter == "" {
		return "base answer", nil
	}
	return "Ahoy! What year, make, and model?", nil
}

type harness struct {
	svc     *Service
	kb      *mimir.Store
	trainer *fakeTrainer
	gen     *generated
	py      *fakePython
	dir     string
}

var testCatalog = map[string]models.CatalogEntry{
	"qwen2.5-7b-q4": {ID: "qwen2.5-7b-q4", DisplayName: "Qwen 2.5 7B", Parameters: "7B", Family: "qwen2.5", Training: &qwen7},
	"small-q4": {ID: "small-q4", DisplayName: "Small 0.5B", Parameters: "0.5B", Training: &models.TrainingInfo{
		BaseRepo: "Qwen/Qwen2.5-0.5B-Instruct", Architecture: "qwen2", License: "Apache-2.0", HiddenSize: 896, Layers: 24, VocabSize: 151936, BaseBytes: 988097824}},
	"research-q4": {ID: "research-q4", DisplayName: "Research 3B", Parameters: "3B", Training: &models.TrainingInfo{
		BaseRepo: "Qwen/Qwen2.5-3B-Instruct", Architecture: "qwen2", License: "Qwen Research License", LicenseNote: "Non-commercial use only.",
		HiddenSize: 2048, Layers: 36, VocabSize: 151936, BaseBytes: 6171877376}},
	"vision-q4": {ID: "vision-q4", DisplayName: "Vision"},
}

func newHarness(t *testing.T, mode string) *harness {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	h := &harness{kb: mimir.NewStore(db.SQL, filepath.Join(dir, "knowledge")), trainer: &fakeTrainer{mode: mode, started: make(chan struct{}, 1)},
		gen: &generated{}, py: &fakePython{}, dir: dir}
	logs := filepath.Join(dir, "logs")
	_ = os.MkdirAll(logs, 0o755)
	h.svc = NewService(Deps{
		Repo:      NewRepo(db.SQL),
		Knowledge: h.kb,
		Catalog:   func(id string) (models.CatalogEntry, bool) { e, ok := testCatalog[id]; return e, ok },
		Installed: func(ctx context.Context, id string) bool { return true },
		Nodes: func(ctx context.Context) ([]Node, error) {
			return []Node{{ID: "local", Name: "this Mac", Local: true, Online: true, Hardware: mac(64)}}, nil
		},
		Python:      h.py,
		Trainers:    []Trainer{h.trainer},
		DataDir:     filepath.Join(dir, "training"),
		HFHome:      filepath.Join(dir, "hf"),
		LogsDir:     logs,
		Generate:    h.gen.fn,
		LocalNodeID: "local",
	})
	return h
}

func tireExamples(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, `{"messages":[{"role":"user","content":"Question %d about tires"},{"role":"assistant","content":"Ahoy! What year, make, and model is car %d?"}]}`+"\n", i, i)
	}
	return b.String()
}

func waitJob(t *testing.T, h *harness, id string) Job {
	t.Helper()
	h.svc.Wait()
	j, err := h.svc.Job(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func TestBuildTrainEvaluateDeploy(t *testing.T) {
	h := newHarness(t, "ok")
	ctx := context.Background()
	ai, err := h.svc.CreateAI(ctx, CreateInput{Name: "Tire Assistant", Goal: "Help tire shop customers find the right tires", BaseModelID: "small-q4"})
	if err != nil {
		t.Fatal(err)
	}
	if ai.Slug != "tire-assistant" || !strings.HasPrefix(ai.Instructions, "You are Tire Assistant. Help tire shop customers") {
		t.Fatalf("created %+v", ai)
	}

	// Inventory becomes knowledge, not training.
	inv, err := h.svc.AddMaterial(ctx, ai.ID, MaterialInput{Filename: "inventory.csv", Text: "sku,size,price,in_stock\nMP-1,225/45R17,189.99,12\n"})
	if err != nil {
		t.Fatal(err)
	}
	if inv.Use != UseKnowledge || inv.KnowledgeSourceID == "" || inv.ExampleCount != 0 {
		t.Fatalf("inventory material = %+v", inv)
	}
	// Forcing training on a table with no examples is refused.
	if _, err := h.svc.AddMaterial(ctx, ai.ID, MaterialInput{Filename: "inventory.csv", Text: "sku,price\nA,1\n", Use: UseTraining}); err == nil {
		t.Fatal("training on a catalog must be refused")
	}

	plan, err := h.svc.Plan(ctx, ai.ID)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Ready || !strings.Contains(strings.Join(plan.Blockers, " "), "at least 10") {
		t.Fatalf("plan without examples = %+v", plan)
	}

	chats, err := h.svc.AddMaterial(ctx, ai.ID, MaterialInput{Filename: "chats.jsonl", Text: tireExamples(20)})
	if err != nil {
		t.Fatal(err)
	}
	if chats.Use != UseTraining || chats.ExampleCount != 20 {
		t.Fatalf("chat material = %+v", chats)
	}
	plan, err = h.svc.Plan(ctx, ai.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Ready || plan.Examples != 20 || plan.Chosen == nil || plan.Chosen.NodeName != "this Mac" || len(plan.Knowledge) != 1 {
		t.Fatalf("ready plan = %+v", plan)
	}

	job, err := h.svc.StartTraining(ctx, ai.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.StartTraining(ctx, ai.ID, ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("second start while training: %v", err)
	}
	job = waitJob(t, h, job.ID)
	if job.State != StateComplete || job.Revision != 1 || job.StartedAt == nil || job.FinishedAt == nil {
		t.Fatalf("job = %+v", job)
	}
	if _, err := os.Stat(h.svc.workDir(job.ID)); !os.IsNotExist(err) {
		t.Fatal("work directory was not cleaned up")
	}
	train, _ := os.ReadFile(filepath.Join(h.trainer.spec.DataDir, "train.jsonl"))
	_ = train // removed with the work dir; the spec still records where it was
	if h.trainer.spec.Repo != "Qwen/Qwen2.5-0.5B-Instruct" || h.trainer.spec.Architecture != "qwen2" {
		t.Fatalf("trainer spec = %+v", h.trainer.spec)
	}

	view, err := h.svc.View(ctx, ai.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Revisions) != 1 || !view.Revisions[0].Evaluated || len(view.EvalRuns) != 1 || len(view.Prompts) == 0 {
		t.Fatalf("view after training = revisions %+v evals %+v prompts %+v", view.Revisions, view.EvalRuns, view.Prompts)
	}
	run := view.EvalRuns[0]
	if run.Status != "complete" || run.Results[0].Base != "base answer" || !strings.HasPrefix(run.Results[0].Specialized, "Ahoy!") {
		t.Fatalf("eval = %+v", run)
	}
	// Both sides saw the same instructions, and the candidate adapter was loaded.
	adapter := AdapterID(ai.ID, 1)
	for _, c := range h.gen.calls {
		if !strings.Contains(c, "["+adapter+"]") || !strings.Contains(c, "You are Tire Assist") {
			t.Fatalf("generate call %q", c)
		}
	}

	// Chat cannot use it until it is deployed.
	if _, err := h.svc.Resolve(ctx, ModelPrefix+"tire-assistant"); err == nil {
		t.Fatal("undeployed AI resolved")
	}
	if models, _ := h.svc.DeployedModels(ctx); len(models) != 0 {
		t.Fatalf("undeployed AI listed: %+v", models)
	}
	if _, err := h.svc.Deploy(ctx, ai.ID, 1); err != nil {
		t.Fatal(err)
	}
	res, err := h.svc.Resolve(ctx, ModelPrefix+"tire-assistant")
	if err != nil {
		t.Fatal(err)
	}
	if res.BaseModelID != "small-q4" || res.Adapter != adapter || len(res.Loaded) != 1 || len(res.AI.Knowledge) != 1 {
		t.Fatalf("resolved %+v", res)
	}
	models, _ := h.svc.DeployedModels(ctx)
	if len(models) != 1 || models[0].ID != "sai:tire-assistant" || models[0].Capabilities.ToolCalling {
		t.Fatalf("deployed models = %+v", models)
	}
	// The base model's process loads the adapter for every deployed AI on it.
	loaded, _ := h.svc.AdaptersFor(ctx, "small-q4")
	if len(loaded) != 1 || loaded[0].ID != adapter {
		t.Fatalf("adapters for base = %+v", loaded)
	}
	if other, _ := h.svc.AdaptersFor(ctx, "qwen2.5-7b-q4"); len(other) != 0 {
		t.Fatalf("adapters leaked to another base: %+v", other)
	}

	// Retraining creates revision 2; revision 1 stays deployed until 2 is.
	job2, err := h.svc.StartTraining(ctx, ai.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if job2 = waitJob(t, h, job2.ID); job2.Revision != 2 {
		t.Fatalf("second revision = %d", job2.Revision)
	}
	if got, _ := h.svc.View(ctx, ai.ID); got.DeployedRevision != 1 || len(got.Revisions) != 2 {
		t.Fatalf("after retrain: deployed %d, revisions %d", got.DeployedRevision, len(got.Revisions))
	}

	// Deleting the AI removes its adapters and the knowledge its material created.
	if err := h.svc.DeleteAI(ctx, ai.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(h.svc.adaptersDir(ai.ID)); !os.IsNotExist(err) {
		t.Fatal("adapters were not deleted")
	}
	if _, err := h.kb.Get(ctx, inv.KnowledgeSourceID); !errors.Is(err, mimir.ErrNotFound) {
		t.Fatalf("knowledge source survived: %v", err)
	}
}

func TestDeployRequiresEvaluation(t *testing.T) {
	h := newHarness(t, "ok")
	ctx := context.Background()
	h.svc.d.Installed = func(context.Context, string) bool { return false } // no base GGUF, so no automatic evaluation
	ai, _ := h.svc.CreateAI(ctx, CreateInput{Name: "Bot", BaseModelID: "small-q4"})
	if _, err := h.svc.AddMaterial(ctx, ai.ID, MaterialInput{Filename: "c.jsonl", Text: tireExamples(12)}); err != nil {
		t.Fatal(err)
	}
	job, err := h.svc.StartTraining(ctx, ai.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	waitJob(t, h, job.ID)
	if _, err := h.svc.Deploy(ctx, ai.ID, 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("deploy without evaluation: %v", err)
	}
	if _, err := h.svc.Evaluate(ctx, ai.ID, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.Deploy(ctx, ai.ID, 1); err != nil {
		t.Fatalf("deploy after evaluation: %v", err)
	}
}

func TestCancelCleansUp(t *testing.T) {
	h := newHarness(t, "block")
	ctx := context.Background()
	ai, _ := h.svc.CreateAI(ctx, CreateInput{Name: "Bot", BaseModelID: "small-q4"})
	if _, err := h.svc.AddMaterial(ctx, ai.ID, MaterialInput{Filename: "c.jsonl", Text: tireExamples(12)}); err != nil {
		t.Fatal(err)
	}
	job, err := h.svc.StartTraining(ctx, ai.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-h.trainer.started:
	case <-time.After(5 * time.Second):
		t.Fatal("trainer did not start")
	}
	if _, err := h.svc.CancelJob(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	job = waitJob(t, h, job.ID)
	if job.State != StateCancelled {
		t.Fatalf("state = %s", job.State)
	}
	if _, err := os.Stat(h.trainer.spec.AdapterOut); !os.IsNotExist(err) {
		t.Fatal("partial adapter left behind")
	}
	if _, err := os.Stat(h.svc.workDir(job.ID)); !os.IsNotExist(err) {
		t.Fatal("work directory left behind")
	}
	if revs, _ := h.svc.d.Repo.ListRevisions(ctx, ai.ID); len(revs) != 0 {
		t.Fatalf("cancelled job created revisions: %+v", revs)
	}
	// Training can start again.
	h.trainer.mode = "ok"
	if _, err := h.svc.StartTraining(ctx, ai.ID, ""); err != nil {
		t.Fatalf("restart after cancel: %v", err)
	}
	h.svc.Wait()
}

func TestFailureIsReportedAndCleanedUp(t *testing.T) {
	h := newHarness(t, "fail")
	ctx := context.Background()
	ai, _ := h.svc.CreateAI(ctx, CreateInput{Name: "Bot", BaseModelID: "small-q4"})
	if _, err := h.svc.AddMaterial(ctx, ai.ID, MaterialInput{Filename: "c.jsonl", Text: tireExamples(12)}); err != nil {
		t.Fatal(err)
	}
	job, _ := h.svc.StartTraining(ctx, ai.ID, "")
	job = waitJob(t, h, job.ID)
	if job.State != StateFailed || !strings.Contains(job.Error, "out of memory") {
		t.Fatalf("job = %+v", job)
	}
	if _, err := os.Stat(h.svc.workDir(job.ID)); !os.IsNotExist(err) {
		t.Fatal("work directory left behind")
	}
}

func TestRecoverFailsInterruptedJobs(t *testing.T) {
	h := newHarness(t, "ok")
	ctx := context.Background()
	ai, _ := h.svc.CreateAI(ctx, CreateInput{Name: "Bot", BaseModelID: "small-q4"})
	job, err := h.svc.d.Repo.CreateJob(ctx, Job{AIID: ai.ID, NodeID: "local", Backend: "mlx"})
	if err != nil {
		t.Fatal(err)
	}
	job.State = StateTraining
	_ = h.svc.d.Repo.SaveJob(ctx, job)
	_ = os.MkdirAll(h.svc.workDir(job.ID), 0o755)
	if err := h.svc.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := h.svc.Job(ctx, job.ID)
	if got.State != StateFailed || !strings.Contains(got.Error, "stopped") {
		t.Fatalf("recovered job = %+v", got)
	}
	if _, err := os.Stat(h.svc.workDir(job.ID)); !os.IsNotExist(err) {
		t.Fatal("work directory left behind")
	}
}

func TestRecommendBases(t *testing.T) {
	h := newHarness(t, "ok")
	var catalog []models.CatalogEntry
	for _, e := range testCatalog {
		catalog = append(catalog, e)
	}
	got, err := h.svc.RecommendBases(context.Background(), "Answer customer questions for my tire shop", catalog)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 trainable models, got %d", len(got))
	}
	if got[0].ModelID != "small-q4" || !got[0].Recommended {
		t.Fatalf("first choice = %+v", got[0])
	}
	if got[len(got)-1].ModelID != "research-q4" {
		t.Fatalf("a non-commercial license should rank last for a business: %+v", got)
	}
}

func TestAddConversations(t *testing.T) {
	h := newHarness(t, "ok")
	ctx := context.Background()
	h.svc.d.Conversation = func(ctx context.Context, id string) ([]contracts.Message, error) {
		return []contracts.Message{
			{Role: "user", Content: "I need tires"}, {Role: "assistant", Content: "What year, make, and model?"},
			{Role: "user", Content: "2018 Camry"}, // unanswered tail is dropped
		}, nil
	}
	ai, _ := h.svc.CreateAI(ctx, CreateInput{Name: "Bot"})
	m, err := h.svc.AddConversations(ctx, ai.ID, []string{"c1", "c2"})
	if err != nil {
		t.Fatal(err)
	}
	examples, _, _ := h.svc.Examples(ctx, ai.ID)
	if m.ExampleCount != 2 || len(examples) != 2 || len(examples[0].Messages) != 2 {
		t.Fatalf("material %+v examples %+v", m, examples)
	}
	// Identical chats: the second is flagged as a duplicate.
	if len(examples[1].Flags) != 1 || examples[1].Flags[0] != FlagDuplicate {
		t.Fatalf("flags = %v", examples[1].Flags)
	}
}

func searchInput(q string, ids []string) mimir.SearchInput {
	return mimir.SearchInput{Query: q, SourceIDs: ids}
}

func TestAttachExistingKnowledge(t *testing.T) {
	h := newHarness(t, "ok")
	ctx := context.Background()
	ai, _ := h.svc.CreateAI(ctx, CreateInput{Name: "Bot"})
	src, err := h.kb.Create(ctx, mimir.CreateInput{Kind: mimir.KindText, Filename: "faq.md", Text: "Open 9 to 5."})
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{src.ID, src.ID}
	got, err := h.svc.UpdateAI(ctx, ai.ID, Patch{Knowledge: &ids})
	if err != nil || len(got.Knowledge) != 1 || got.Knowledge[0] != src.ID {
		t.Fatalf("attach: %+v %v", got.Knowledge, err)
	}
	bad := []string{"missing"}
	if _, err := h.svc.UpdateAI(ctx, ai.ID, Patch{Knowledge: &bad}); !errors.Is(err, mimir.ErrNotFound) {
		t.Fatalf("unknown source: %v", err)
	}
}

func TestSpreadsheetMaterial(t *testing.T) {
	h := newHarness(t, "ok")
	ctx := context.Background()
	ai, _ := h.svc.CreateAI(ctx, CreateInput{Name: "Bot"})
	load := func(name string) string {
		raw, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		return base64.StdEncoding.EncodeToString(raw)
	}
	faq, err := h.svc.AddMaterial(ctx, ai.ID, MaterialInput{Filename: "faq.xlsx", ContentBase64: load("faq.xlsx")})
	if err != nil {
		t.Fatal(err)
	}
	if faq.Use != UseTraining || faq.ExampleCount != 12 {
		t.Fatalf("faq material = %+v", faq)
	}
	inv, err := h.svc.AddMaterial(ctx, ai.ID, MaterialInput{Filename: "inventory.xlsx", ContentBase64: load("inventory.xlsx")})
	if err != nil {
		t.Fatal(err)
	}
	if inv.Use != UseKnowledge || inv.KnowledgeSourceID == "" {
		t.Fatalf("inventory material = %+v", inv)
	}
	hits, err := h.kb.Search(ctx, searchInput("price for 225/45R17", []string{inv.KnowledgeSourceID}))
	if err != nil || len(hits) == 0 || !strings.Contains(hits[0].Body, "Price: 189.99") {
		t.Fatalf("hits = %+v %v", hits, err)
	}
	if _, err := h.kb.Content(ctx, inv.KnowledgeSourceID); err == nil {
		t.Fatal("a workbook copy must not be editable as text")
	}
	raw, err := os.ReadFile("../mimir/testdata/warranty.pdf")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := h.svc.AddMaterial(ctx, ai.ID, MaterialInput{Filename: "warranty.pdf", ContentBase64: base64.StdEncoding.EncodeToString(raw)})
	if err != nil || doc.Use != UseKnowledge || doc.KnowledgeSourceID == "" {
		t.Fatalf("pdf material = %+v %v", doc, err)
	}
}
