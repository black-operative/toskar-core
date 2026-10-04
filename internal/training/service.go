package training

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/toskar-core/internal/mimir"
	"github.com/yeixio/toskar-core/internal/models"
	"github.com/yeixio/toskar-core/internal/pyenv"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// Node is a computer the service may train on.
type Node struct {
	ID       string
	Name     string
	Local    bool
	Online   bool
	Hardware contracts.HardwareInventory
}

// Knowledge is the part of Mimir the service uses.
type Knowledge interface {
	Get(ctx context.Context, id string) (mimir.Source, error)
	Create(ctx context.Context, in mimir.CreateInput) (mimir.Source, error)
	Delete(ctx context.Context, id string) error
	Search(ctx context.Context, in mimir.SearchInput) ([]mimir.Hit, error)
}

// GenerateFunc runs one non-streaming chat turn on baseModelID with the
// given adapters loaded, applying adapter ("" for the base model).
type GenerateFunc func(ctx context.Context, baseModelID, adapter string, loaded []pluginapi.Adapter, messages []pluginapi.ChatMessage) (string, error)

// PythonEnv provides trainer environments. *pyenv.Manager implements it.
type PythonEnv interface {
	Status(spec pyenv.Spec) pyenv.Status
	Ensure(ctx context.Context, spec pyenv.Spec, progress pyenv.Progress) (string, error)
	Env() []string
	// Unavailable says why an environment cannot be used or installed on
	// this computer, such as in a sandboxed app, or returns "".
	Unavailable(spec pyenv.Spec) string
}

// Deps wires the service to the rest of the daemon.
type Deps struct {
	Repo      *Repo
	Knowledge Knowledge
	Catalog   func(modelID string) (models.CatalogEntry, bool)
	// Installed reports whether the base GGUF is on this computer.
	Installed func(ctx context.Context, modelID string) bool
	Nodes     func(ctx context.Context) ([]Node, error)
	Python    PythonEnv
	Trainers  []Trainer
	// DataDir holds adapters and job working directories.
	DataDir string
	// HFHome is the Hugging Face cache for base weights.
	HFHome  string
	LogsDir string
	Publish func(eventType string, payload map[string]any)
	// UnloadLocalModels frees memory before training starts.
	UnloadLocalModels func(ctx context.Context) int
	// Admit, when set, waits until chat, automations, and benchmarks are
	// done with this computer, then holds it for training until Done (§60).
	// Models are unloaded only after that.
	Admit        func(ctx context.Context, name string, waiting func(reason string)) (Hold, error)
	Generate     GenerateFunc
	Conversation func(ctx context.Context, id string) ([]contracts.Message, error)
	Logger       *slog.Logger
	// LocalNodeID identifies this computer among training nodes.
	LocalNodeID string
	// Peer returns a client for a paired computer, for remote training.
	Peer func(ctx context.Context, nodeID string) (Peer, error)
	// ModelPath returns an installed model's GGUF file, for export.
	ModelPath func(ctx context.Context, modelID string) (string, error)
	// ExportTool returns llama-export-lora, which merges an adapter into its
	// base model.
	ExportTool func(ctx context.Context) (string, error)
	// FreeDisk reports the free space where path is, when known.
	FreeDisk func(path string) (uint64, error)
	// Sent, when set, records training data sent to a paired computer (§63).
	Sent func(ctx context.Context, nodeName, detail string)
}

// Service builds, trains, evaluates, and deploys specialized AIs.
type Service struct {
	d Deps

	// slot allows one training run at a time on this computer.
	slot chan struct{}

	mu      sync.Mutex
	cancels map[string]context.CancelFunc
	// evaluating holds revisions whose adapters must stay loaded while an
	// evaluation runs, keyed by base model.
	evaluating map[string][]Revision
	// remote holds runs this computer executes for a paired coordinator.
	remote map[string]*remoteRun
	// questions caches each deployed revision's training questions for
	// routing, keyed by AdapterID.
	questions map[string][]string
	// exports tracks merges into standalone GGUFs, keyed by AdapterID.
	exports map[string]*exportRun
	wg      sync.WaitGroup
}

// NewService returns a service. Call Recover once at startup.
func NewService(d Deps) *Service {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	if d.Publish == nil {
		d.Publish = func(string, map[string]any) {}
	}
	return &Service{d: d, slot: make(chan struct{}, 1), cancels: map[string]context.CancelFunc{}, evaluating: map[string][]Revision{}, remote: map[string]*remoteRun{}, questions: map[string][]string{},
		exports: map[string]*exportRun{}}
}

// Wait blocks until running jobs and evaluations return. Used in tests and at shutdown.
func (s *Service) Wait() { s.wg.Wait() }

func (s *Service) adaptersDir(aiID string) string {
	return filepath.Join(s.d.DataDir, "adapters", aiID)
}
func (s *Service) workDir(jobID string) string { return filepath.Join(s.d.DataDir, "jobs", jobID) }

// Recover marks jobs that were running when the daemon stopped as failed and
// removes their working files.
func (s *Service) Recover(ctx context.Context) error {
	active, err := s.d.Repo.ActiveJobs(ctx)
	if err != nil {
		return err
	}
	for _, j := range active {
		now := time.Now()
		j.State, j.Error, j.FinishedAt = StateFailed, "Toskar stopped while this job was running. Start training again.", &now
		if err := s.d.Repo.SaveJob(ctx, j); err != nil {
			return err
		}
		_ = os.RemoveAll(s.workDir(j.ID))
	}
	return nil
}

// Backends lists trainers and whether this computer supports each.
func (s *Service) Backends(ctx context.Context) []map[string]any {
	var local contracts.HardwareInventory
	if nodes, err := s.d.Nodes(ctx); err == nil {
		for _, n := range nodes {
			if n.Local {
				local = n.Hardware
			}
		}
	}
	out := []map[string]any{}
	for _, t := range s.d.Trainers {
		ok, why := s.supportsHere(t, local)
		st := s.d.Python.Status(t.Environment())
		out = append(out, map[string]any{"id": t.ID(), "name": t.DisplayName(), "supported": ok, "reason": why, "installed": st.Installed})
	}
	return out
}

// CreateInput starts a new specialized AI.
type CreateInput struct {
	Name         string `json:"name"`
	Goal         string `json:"goal"`
	Instructions string `json:"instructions"`
	BaseModelID  string `json:"base_model_id"`
	Preset       Preset `json:"preset"`
}

// DraftInstructions turns the user's description into system instructions
// they can edit.
func DraftInstructions(name, goal string) string {
	name, goal = strings.TrimSpace(name), strings.TrimSpace(goal)
	var b strings.Builder
	fmt.Fprintf(&b, "You are %s.", name)
	if goal != "" {
		b.WriteString(" " + goal)
		if !strings.HasSuffix(goal, ".") {
			b.WriteString(".")
		}
	}
	b.WriteString(" Ask a short clarifying question when a request is missing details you need.")
	b.WriteString(" Use the connected knowledge for facts such as prices and availability, and say so when it does not have the answer.")
	return b.String()
}

// CreateAI stores a new specialized AI.
func (s *Service) CreateAI(ctx context.Context, in CreateInput) (SpecializedAI, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return SpecializedAI{}, fmt.Errorf("name your AI")
	}
	if in.BaseModelID != "" {
		if err := s.checkBase(in.BaseModelID); err != nil {
			return SpecializedAI{}, err
		}
	}
	if in.Preset != "" && in.Preset != PresetQuick && in.Preset != PresetBalanced && in.Preset != PresetQuality {
		return SpecializedAI{}, fmt.Errorf("preset must be quick, balanced, or quality")
	}
	if strings.TrimSpace(in.Instructions) == "" {
		in.Instructions = DraftInstructions(in.Name, in.Goal)
	}
	return s.d.Repo.CreateAI(ctx, SpecializedAI{Name: in.Name, Goal: strings.TrimSpace(in.Goal),
		Instructions: strings.TrimSpace(in.Instructions), BaseModelID: in.BaseModelID, Preset: in.Preset})
}

// Patch changes an AI's settings.
type Patch struct {
	Name         *string   `json:"name,omitempty"`
	Goal         *string   `json:"goal,omitempty"`
	Instructions *string   `json:"instructions,omitempty"`
	BaseModelID  *string   `json:"base_model_id,omitempty"`
	Preset       *Preset   `json:"preset,omitempty"`
	Advanced     *Hyper    `json:"advanced,omitempty"`
	ClearAdv     bool      `json:"clear_advanced,omitempty"`
	Knowledge    *[]string `json:"knowledge_sources,omitempty"`
}

// UpdateAI applies a patch. Changing the base model after training means the
// next revision trains against the new base; old revisions keep theirs.
func (s *Service) UpdateAI(ctx context.Context, id string, p Patch) (SpecializedAI, error) {
	ai, err := s.d.Repo.GetAI(ctx, id)
	if err != nil {
		return SpecializedAI{}, err
	}
	if p.Name != nil {
		if strings.TrimSpace(*p.Name) == "" {
			return SpecializedAI{}, fmt.Errorf("name your AI")
		}
		ai.Name = strings.TrimSpace(*p.Name)
	}
	if p.Goal != nil {
		ai.Goal = strings.TrimSpace(*p.Goal)
	}
	if p.Instructions != nil {
		ai.Instructions = strings.TrimSpace(*p.Instructions)
	}
	if p.BaseModelID != nil {
		if err := s.checkBase(*p.BaseModelID); err != nil {
			return SpecializedAI{}, err
		}
		ai.BaseModelID = *p.BaseModelID
	}
	if p.Preset != nil {
		ai.Preset = *p.Preset
	}
	if p.Advanced != nil {
		ai.Advanced = p.Advanced
	}
	if p.ClearAdv {
		ai.Advanced = nil
	}
	if p.Knowledge != nil {
		ids := []string{}
		for _, id := range *p.Knowledge {
			if _, err := s.d.Knowledge.Get(ctx, id); err != nil {
				return SpecializedAI{}, fmt.Errorf("knowledge source %s: %w", id, err)
			}
			ids = appendUnique(ids, id)
		}
		ai.Knowledge = ids
	}
	return s.d.Repo.UpdateAI(ctx, ai)
}

func (s *Service) checkBase(modelID string) error {
	entry, ok := s.d.Catalog(modelID)
	if !ok {
		return fmt.Errorf("model %q is not in the catalog", modelID)
	}
	if entry.Training == nil {
		return fmt.Errorf("%s cannot be trained yet. Choose one of the recommended base models", entry.DisplayName)
	}
	return nil
}

// DeleteAI removes an AI, its adapters, and the knowledge sources its
// material created.
func (s *Service) DeleteAI(ctx context.Context, id string) error {
	jobs, err := s.d.Repo.ListJobs(ctx, id)
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if !j.State.Terminal() {
			return fmt.Errorf("cancel the running training job first: %w", ErrConflict)
		}
	}
	mats, err := s.d.Repo.ListMaterials(ctx, id)
	if err != nil {
		return err
	}
	if err := s.d.Repo.DeleteAI(ctx, id); err != nil {
		return err
	}
	for _, m := range mats {
		if m.KnowledgeSourceID != "" {
			_ = s.d.Knowledge.Delete(ctx, m.KnowledgeSourceID)
		}
	}
	s.cancelExports(id, 0)
	_ = os.RemoveAll(s.exportsDir(id))
	return os.RemoveAll(s.adaptersDir(id))
}

// AIView is everything the Train page shows for one AI.
type AIView struct {
	SpecializedAI
	ModelID    string        `json:"model_id"`
	Materials  []Material    `json:"materials"`
	Dataset    DatasetStats  `json:"dataset"`
	Revisions  []Revision    `json:"revisions"`
	Jobs       []Job         `json:"jobs"`
	EvalRuns   []EvalRun     `json:"eval_runs"`
	Prompts    []EvalPrompt  `json:"test_prompts"`
	Base       *BaseModelRef `json:"base_model,omitempty"`
	Deployable []int         `json:"deployable_revisions"`
}

// BaseModelRef describes an AI's base model.
type BaseModelRef struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Parameters  string `json:"parameters"`
	License     string `json:"license"`
	LicenseNote string `json:"license_note,omitempty"`
	Installed   bool   `json:"installed"`
}

// View returns one AI with its material, dataset summary, revisions, jobs,
// and evaluations.
func (s *Service) View(ctx context.Context, id string) (AIView, error) {
	ai, err := s.d.Repo.GetAI(ctx, id)
	if err != nil {
		return AIView{}, err
	}
	v := AIView{SpecializedAI: ai, ModelID: ai.ModelID(), Deployable: []int{}}
	if v.Materials, err = s.d.Repo.ListMaterials(ctx, id); err != nil {
		return AIView{}, err
	}
	examples, err := s.validatedExamples(ctx, ai)
	if err != nil {
		return AIView{}, err
	}
	v.Dataset = Stats(examples)
	if v.Revisions, err = s.d.Repo.ListRevisions(ctx, id); err != nil {
		return AIView{}, err
	}
	for _, r := range v.Revisions {
		if r.Evaluated {
			v.Deployable = append(v.Deployable, r.Revision)
		}
	}
	if v.Jobs, err = s.d.Repo.ListJobs(ctx, id); err != nil {
		return AIView{}, err
	}
	if v.EvalRuns, err = s.d.Repo.ListEvalRuns(ctx, id); err != nil {
		return AIView{}, err
	}
	if v.Prompts, err = s.d.Repo.EvalPrompts(ctx, id); err != nil {
		return AIView{}, err
	}
	if ai.BaseModelID != "" {
		v.Base = s.baseRef(ctx, ai.BaseModelID)
	}
	return v, nil
}

func (s *Service) baseRef(ctx context.Context, modelID string) *BaseModelRef {
	entry, ok := s.d.Catalog(modelID)
	if !ok {
		return nil
	}
	ref := &BaseModelRef{ID: modelID, DisplayName: entry.DisplayName, Parameters: entry.Parameters}
	if entry.Training != nil {
		ref.License, ref.LicenseNote = entry.Training.License, entry.Training.LicenseNote
	}
	if s.d.Installed != nil {
		ref.Installed = s.d.Installed(ctx, modelID)
	}
	return ref
}

// ListAIs returns every specialized AI.
func (s *Service) ListAIs(ctx context.Context) ([]SpecializedAI, error) { return s.d.Repo.ListAIs(ctx) }

// Classify previews how material would be used, before it is added.
func (s *Service) Classify(filename, text string) Recommendation { return Classify(filename, text) }

// MaterialInput adds material.
type MaterialInput struct {
	Name     string `json:"name"`
	Filename string `json:"filename"`
	Text     string `json:"text"`
	// ContentBase64 carries binary files such as .xlsx, in place of Text.
	ContentBase64 string `json:"content_base64,omitempty"`
	// Use overrides the recommendation. Empty accepts it.
	Use Use `json:"use,omitempty"`
}

// MaterialText returns what the classifier and example parser read for a
// piece of material. A spreadsheet is read as CSV from its first sheet.
func MaterialText(filename, text, contentBase64 string) (string, string, error) {
	if contentBase64 == "" {
		if strings.TrimSpace(text) == "" {
			return "", "", fmt.Errorf("the material is empty")
		}
		if len(text) > mimir.MaxTextBytes {
			return "", "", fmt.Errorf("the material is larger than %d MB", mimir.MaxTextBytes>>20)
		}
		return filename, text, nil
	}
	if base64.StdEncoding.DecodedLen(len(contentBase64)) > mimir.MaxTextBytes {
		return "", "", fmt.Errorf("the file is larger than %d MB", mimir.MaxTextBytes>>20)
	}
	raw, err := base64.StdEncoding.DecodeString(contentBase64)
	if err != nil {
		return "", "", fmt.Errorf("the upload is not valid base64")
	}
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".xlsx":
		csvText, err := mimir.FirstSheetAsCSV(filename, raw)
		if err != nil {
			return "", "", err
		}
		return strings.TrimSuffix(filename, filepath.Ext(filename)) + ".csv", csvText, nil
	case ".pdf":
		text, err := mimir.PDFText(filename, raw)
		if errors.Is(err, mimir.ErrNoText) {
			// A scanned PDF has no examples to read, so it is classified as
			// knowledge; connecting it reads its pages with text recognition.
			text, err = "", nil
		}
		if err != nil {
			return "", "", err
		}
		return strings.TrimSuffix(filename, filepath.Ext(filename)) + ".txt", text, nil
	}
	return filename, string(raw), nil
}

// AddMaterial classifies material, then stores its examples and connects it
// as knowledge according to the chosen use.
func (s *Service) AddMaterial(ctx context.Context, aiID string, in MaterialInput) (Material, error) {
	ai, err := s.d.Repo.GetAI(ctx, aiID)
	if err != nil {
		return Material{}, err
	}
	in.Filename = filepath.Base(strings.TrimSpace(in.Filename))
	if in.Filename == "" || in.Filename == "." {
		in.Filename = "pasted.txt"
	}
	if strings.TrimSpace(in.Name) == "" {
		in.Name = in.Filename
	}
	readName, readText, err := MaterialText(in.Filename, in.Text, in.ContentBase64)
	if err != nil {
		return Material{}, err
	}
	rec := Classify(readName, readText)
	use := in.Use
	if use == "" {
		use = rec.Use
	}
	warning, err := ChoiceWarning(rec, use)
	if err != nil {
		return Material{}, err
	}
	var examples [][]Message
	if use.trains() {
		if examples, err = ParseExamples(readName, readText); err != nil {
			return Material{}, err
		}
	}
	m := Material{AIID: aiID, Name: in.Name, Filename: in.Filename, Use: use, Recommended: rec, Warning: warning}
	if use.connects() {
		src, err := s.d.Knowledge.Create(ctx, mimir.CreateInput{Name: ai.Name + ": " + in.Name, Kind: mimir.KindText,
			Filename: in.Filename, Text: in.Text, ContentBase64: in.ContentBase64})
		if err != nil {
			return Material{}, err
		}
		if src.Status == mimir.StatusFailed {
			_ = s.d.Knowledge.Delete(ctx, src.ID)
			return Material{}, fmt.Errorf("could not index %s: %s", in.Name, src.Error)
		}
		m.KnowledgeSourceID = src.ID
		ai.Knowledge = appendUnique(ai.Knowledge, src.ID)
		if _, err := s.d.Repo.UpdateAI(ctx, ai); err != nil {
			return Material{}, err
		}
	}
	saved, err := s.d.Repo.AddMaterial(ctx, m, examples)
	if err != nil && m.KnowledgeSourceID != "" {
		_ = s.d.Knowledge.Delete(ctx, m.KnowledgeSourceID)
	}
	return saved, err
}

// RemoveMaterial deletes material, its examples, and its knowledge source.
func (s *Service) RemoveMaterial(ctx context.Context, aiID, materialID string) error {
	m, err := s.d.Repo.GetMaterial(ctx, aiID, materialID)
	if err != nil {
		return err
	}
	if err := s.d.Repo.DeleteMaterial(ctx, aiID, materialID); err != nil {
		return err
	}
	if m.KnowledgeSourceID != "" {
		_ = s.d.Knowledge.Delete(ctx, m.KnowledgeSourceID)
		if ai, err := s.d.Repo.GetAI(ctx, aiID); err == nil {
			ai.Knowledge = removeString(ai.Knowledge, m.KnowledgeSourceID)
			_, _ = s.d.Repo.UpdateAI(ctx, ai)
		}
	}
	return nil
}

// AddConversations turns saved chats into training examples. Each chat
// becomes one multi-turn example ending on an assistant reply.
func (s *Service) AddConversations(ctx context.Context, aiID string, ids []string) (Material, error) {
	if _, err := s.d.Repo.GetAI(ctx, aiID); err != nil {
		return Material{}, err
	}
	if s.d.Conversation == nil || len(ids) == 0 {
		return Material{}, fmt.Errorf("choose at least one conversation")
	}
	var examples [][]Message
	for _, id := range ids {
		msgs, err := s.d.Conversation(ctx, id)
		if err != nil {
			return Material{}, err
		}
		var ex []Message
		for _, m := range msgs {
			if (m.Role == "user" || m.Role == "assistant") && strings.TrimSpace(m.Content) != "" {
				ex = append(ex, Message{Role: m.Role, Content: m.Content})
			}
		}
		for len(ex) > 0 && ex[len(ex)-1].Role != "assistant" {
			ex = ex[:len(ex)-1]
		}
		if len(ex) >= 2 {
			examples = append(examples, ex)
		}
	}
	if len(examples) == 0 {
		return Material{}, fmt.Errorf("those conversations have no answered questions")
	}
	name := fmt.Sprintf("%d saved chats", len(examples))
	if len(examples) == 1 {
		name = "1 saved chat"
	}
	rec := Recommendation{Use: UseTraining, CanTrain: true, ExampleCount: len(examples),
		Reasons: []string{"Chats you had with Toskar show the responses you want. Edit or remove any you do not want repeated."}}
	return s.d.Repo.AddMaterial(ctx, Material{AIID: aiID, Name: name, Use: UseTraining, Recommended: rec}, examples)
}

// AddExample stores one example the user wrote.
func (s *Service) AddExample(ctx context.Context, aiID string, messages []Message) error {
	if _, err := s.d.Repo.GetAI(ctx, aiID); err != nil {
		return err
	}
	if len(messages) < 2 {
		return fmt.Errorf("an example needs a question and an answer")
	}
	return s.d.Repo.AddExamples(ctx, aiID, [][]Message{messages})
}

// UpdateExample edits or excludes an example.
func (s *Service) UpdateExample(ctx context.Context, aiID, id string, messages []Message, excluded *bool) error {
	return s.d.Repo.UpdateExample(ctx, aiID, id, messages, excluded)
}

// DeleteExample removes an example.
func (s *Service) DeleteExample(ctx context.Context, aiID, id string) error {
	return s.d.Repo.DeleteExample(ctx, aiID, id)
}

// Examples returns an AI's examples with their flags.
func (s *Service) Examples(ctx context.Context, aiID string) ([]Example, DatasetStats, error) {
	ai, err := s.d.Repo.GetAI(ctx, aiID)
	if err != nil {
		return nil, DatasetStats{}, err
	}
	examples, err := s.validatedExamples(ctx, ai)
	if err != nil {
		return nil, DatasetStats{}, err
	}
	return examples, Stats(examples), nil
}

func (s *Service) validatedExamples(ctx context.Context, ai SpecializedAI) ([]Example, error) {
	examples, err := s.d.Repo.ListExamples(ctx, ai.ID)
	if err != nil {
		return nil, err
	}
	max := 2048
	if ai.Advanced != nil && ai.Advanced.MaxSeqLength > 0 {
		max = ai.Advanced.MaxSeqLength
	}
	// Count the instructions that training prepends to every example.
	instr := EstimateTokens(ai.Instructions)
	return ValidateExamples(examples, max-instr), nil
}

// BaseChoice is one recommended base model.
type BaseChoice struct {
	ModelID     string   `json:"model_id"`
	DisplayName string   `json:"display_name"`
	Parameters  string   `json:"parameters"`
	License     string   `json:"license"`
	LicenseNote string   `json:"license_note,omitempty"`
	Installed   bool     `json:"installed"`
	Recommended bool     `json:"recommended"`
	Reasons     []string `json:"reasons"`
	Fit         NodeFit  `json:"fit"`
}

// RecommendBases ranks trainable catalog models for a goal. Training fit is
// computed for the best computer; a model that cannot be trained anywhere is
// listed last with its reason.
func (s *Service) RecommendBases(ctx context.Context, goal string, catalog []models.CatalogEntry) ([]BaseChoice, error) {
	nodes, err := s.onlineNodes(ctx)
	if err != nil {
		return nil, err
	}
	goalLower := strings.ToLower(goal)
	coding := containsAny(goalLower, "code", "coding", "program", "sql", "script", "developer", "api")
	business := containsAny(goalLower, "customer", "business", "shop", "store", "sales", "support", "company", "client", "inventory")
	reasoning := containsAny(goalLower, "reason", "math", "logic", "analy")
	st := DatasetStats{Usable: RecommendedExamples, P95Tokens: 384, Tokens: RecommendedExamples * 200}

	var infos []models.TrainingInfo
	for _, e := range catalog {
		if e.Training != nil {
			infos = append(infos, *e.Training)
		}
	}
	caps := s.capsFor(ctx, nodes, trainingRepos(infos...))
	var out []BaseChoice
	for _, e := range catalog {
		if e.Training == nil {
			continue
		}
		h := PresetSettings(PresetBalanced, *e.Training, st)
		c := BaseChoice{ModelID: e.ID, DisplayName: e.DisplayName, Parameters: e.Parameters,
			License: e.Training.License, LicenseNote: e.Training.LicenseNote}
		if s.d.Installed != nil {
			c.Installed = s.d.Installed(ctx, e.ID)
		}
		c.Fit = s.bestFit(nodes, caps, *e.Training, h, st)
		out = append(out, c)
	}
	score := func(c BaseChoice) float64 {
		e, _ := s.d.Catalog(c.ModelID)
		sc := 0.0
		if !c.Fit.Eligible {
			sc -= 100
		}
		// Small models are cheap to train and fast to run; prefer them.
		sc -= float64(e.Training.BaseBytes) / (1 << 30)
		isCoder := strings.Contains(strings.ToLower(e.ID), "coder")
		isReasoner := strings.Contains(strings.ToLower(e.ID), "r1")
		if coding && isCoder {
			sc += 6
		}
		if !coding && isCoder {
			sc -= 3
		}
		if reasoning && isReasoner {
			sc += 4
		}
		if !reasoning && isReasoner {
			sc -= 4
		}
		if business && strings.Contains(c.LicenseNote, "Non-commercial") {
			sc -= 50
		}
		if c.Installed {
			sc += 1
		}
		return sc
	}
	sort.SliceStable(out, func(i, j int) bool { return score(out[i]) > score(out[j]) })
	for i := range out {
		e, _ := s.d.Catalog(out[i].ModelID)
		out[i].Reasons = baseReasons(out[i], e, coding, business)
		if i == 0 && out[i].Fit.Eligible {
			out[i].Recommended = true
		}
	}
	return out, nil
}

func baseReasons(c BaseChoice, e models.CatalogEntry, coding, business bool) []string {
	var r []string
	if !c.Fit.Eligible {
		return []string{c.Fit.Reason}
	}
	r = append(r, fmt.Sprintf("%s parameters. Trains on %s in about %s.", c.Parameters, c.Fit.NodeName, humanDuration(c.Fit.DurationSec)))
	if coding && strings.Contains(strings.ToLower(e.ID), "coder") {
		r = append(r, "Tuned for code, which suits your description.")
	}
	if business && c.LicenseNote == "" {
		r = append(r, c.License+" allows business use.")
	}
	if c.LicenseNote != "" {
		r = append(r, c.LicenseNote)
	}
	if c.Installed {
		r = append(r, "Already on this computer.")
	}
	return r
}

func humanDuration(sec int) string {
	switch {
	case sec < 90:
		return "a minute"
	case sec < 3600:
		return fmt.Sprintf("%d minutes", (sec+30)/60)
	default:
		return fmt.Sprintf("%.1f hours", float64(sec)/3600)
	}
}

func containsAny(s string, words ...string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

func (s *Service) onlineNodes(ctx context.Context) ([]Node, error) {
	all, err := s.d.Nodes(ctx)
	if err != nil {
		return nil, err
	}
	var out []Node
	for _, n := range all {
		if n.Local || n.Online {
			out = append(out, n)
		}
	}
	return out, nil
}

// peerCaps is a paired computer's answer to a capabilities request.
type peerCaps struct {
	caps RemoteCapabilities
	err  error
}

// capsFor asks each online paired computer whether it can train, once.
func (s *Service) capsFor(ctx context.Context, nodes []Node, repos []string) map[string]peerCaps {
	out := map[string]peerCaps{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, n := range nodes {
		if n.Local {
			continue
		}
		wg.Add(1)
		go func(n Node) {
			defer wg.Done()
			caps, err := s.remoteCapabilities(ctx, n.ID, repos)
			mu.Lock()
			out[n.ID] = peerCaps{caps: caps, err: err}
			mu.Unlock()
		}(n)
	}
	wg.Wait()
	return out
}

func trainingRepos(infos ...models.TrainingInfo) []string {
	var repos []string
	for _, i := range infos {
		repos = append(repos, i.BaseRepo)
		if i.QuantizedRepo != "" {
			repos = append(repos, i.QuantizedRepo)
		}
	}
	return repos
}

// supportsHere reports whether t can train on this computer: the hardware
// suits it, and its Python environment can be used here. A sandboxed app can
// only use an environment bundled with it (#95).
func (s *Service) supportsHere(t Trainer, hw contracts.HardwareInventory) (bool, string) {
	if ok, why := t.Supports(hw); !ok {
		return false, why
	}
	if why := s.d.Python.Unavailable(t.Environment()); why != "" {
		return false, sandboxReason(why)
	}
	return true, ""
}

// sandboxReason explains an environment that cannot run here, with what to
// do instead.
func sandboxReason(why string) string {
	return "Training can't run on this computer: " + why + ". Pair a computer running Toskar Core to train there."
}

func (s *Service) fitFor(n Node, caps map[string]peerCaps, info models.TrainingInfo, h Hyper, st DatasetStats, pinned bool) NodeFit {
	trainer := TrainerFor(s.d.Trainers, n.Hardware)
	in := FitInput{NodeID: n.ID, NodeName: n.Name, Local: n.Local, Hardware: n.Hardware, Info: info, Hyper: h, Stats: st,
		Trainer: trainer, Pinned: pinned}
	var busy bool
	if n.Local {
		in.Cached = s.cached
		if trainer != nil {
			in.EnvInstalled = s.d.Python.Status(trainer.Environment()).Installed
			if why := s.d.Python.Unavailable(trainer.Environment()); why != "" {
				in.Unavailable = sandboxReason(why)
			}
		}
	} else {
		pc, ok := caps[n.ID]
		switch {
		case !ok:
			return NodeFit{NodeID: n.ID, NodeName: n.Name, Label: FitUnsupported, Reason: "Toskar did not ask " + n.Name + " about training."}
		case errors.Is(pc.err, errOldPeer):
			return NodeFit{NodeID: n.ID, NodeName: n.Name, Label: FitUnsupported, Reason: "Update Toskar on " + n.Name + " to train there."}
		case pc.err != nil:
			return NodeFit{NodeID: n.ID, NodeName: n.Name, Label: FitUnsupported, Reason: "Could not reach " + n.Name + "."}
		}
		in.Cached = func(repo string) bool { return pc.caps.Cached[repo] }
		if trainer != nil {
			in.EnvInstalled = pc.caps.Installed[trainer.ID()]
			// The peer knows what it cannot run, such as a sandboxed app.
			for _, b := range pc.caps.Backends {
				if b.ID == trainer.ID() && !b.Supported && b.Reason != "" {
					in.Unavailable = strings.Replace(b.Reason, "this computer", n.Name, 1)
				}
			}
		}
		busy = pc.caps.Busy
	}
	fit := EstimateFit(in)
	if busy && fit.Eligible {
		fit.Notes = append(fit.Notes, n.Name+" is training something else. This run starts when that one ends.")
	}
	return fit
}

func (s *Service) bestFit(nodes []Node, caps map[string]peerCaps, info models.TrainingInfo, h Hyper, st DatasetStats) NodeFit {
	var fits []NodeFit
	for _, n := range nodes {
		fits = append(fits, s.fitFor(n, caps, info, h, st, false))
	}
	if best, err := PickNode(fits); err == nil {
		return best
	}
	for _, f := range fits {
		if f.Local {
			return f
		}
	}
	if len(fits) > 0 {
		return fits[0]
	}
	return NodeFit{Label: FitUnsupported, Reason: "No computer is available."}
}

// cached reports whether Hugging Face weights are already downloaded.
func (s *Service) cached(repo string) bool {
	dir := filepath.Join(s.d.HFHome, "hub", "models--"+strings.ReplaceAll(repo, "/", "--"), "snapshots")
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) > 0
}

// Plan is the review step: what trains, what stays connected, and where.
type Plan struct {
	Ready    bool     `json:"ready"`
	Blockers []string `json:"blockers"`
	Warnings []string `json:"warnings"`
	// Training
	Examples int    `json:"examples"`
	Preset   Preset `json:"preset"`
	Hyper    Hyper  `json:"hyper"`
	// Knowledge
	Knowledge []string `json:"knowledge_sources"`
	// Where
	Fits   []NodeFit `json:"fits"`
	Chosen *NodeFit  `json:"chosen,omitempty"`
	// Revision is the number the next training run will create.
	Revision int `json:"next_revision"`
}

// Plan computes the training plan for an AI.
func (s *Service) Plan(ctx context.Context, aiID string) (Plan, error) {
	ai, err := s.d.Repo.GetAI(ctx, aiID)
	if err != nil {
		return Plan{}, err
	}
	p := Plan{Preset: ai.Preset, Knowledge: ai.Knowledge, Blockers: []string{}, Warnings: []string{}}
	jobs, err := s.d.Repo.ListJobs(ctx, aiID)
	if err != nil {
		return Plan{}, err
	}
	p.Revision = 1
	for _, j := range jobs {
		if j.Revision >= p.Revision {
			p.Revision = j.Revision + 1
		}
		if !j.State.Terminal() {
			p.Blockers = append(p.Blockers, "This AI is already training.")
		}
	}
	examples, err := s.validatedExamples(ctx, ai)
	if err != nil {
		return Plan{}, err
	}
	st := Stats(examples)
	p.Examples = st.Usable
	p.Warnings = append(p.Warnings, st.Warnings...)
	st = WithInstructionTokens(st, ai.Instructions)
	if st.Usable < MinExamples {
		p.Blockers = append(p.Blockers, fmt.Sprintf("Add at least %d usable examples.", MinExamples))
	}
	if ai.BaseModelID == "" {
		p.Blockers = append(p.Blockers, "Choose a base model.")
		return p, nil
	}
	entry, ok := s.d.Catalog(ai.BaseModelID)
	if !ok || entry.Training == nil {
		p.Blockers = append(p.Blockers, "The base model cannot be trained.")
		return p, nil
	}
	if s.d.Installed != nil && !s.d.Installed(ctx, ai.BaseModelID) {
		p.Warnings = append(p.Warnings, entry.DisplayName+" is not installed. Install it from Models to test and use the result.")
	}
	h := ApplyAdvanced(PresetSettings(ai.Preset, *entry.Training, st), ai.Advanced, st.Usable)
	p.Hyper = h
	nodes, err := s.onlineNodes(ctx)
	if err != nil {
		return Plan{}, err
	}
	caps := s.capsFor(ctx, nodes, trainingRepos(*entry.Training))
	for _, n := range nodes {
		p.Fits = append(p.Fits, s.fitFor(n, caps, *entry.Training, h, st, ai.Advanced != nil && ai.Advanced.Method != ""))
	}
	best, err := PickNode(p.Fits)
	if err != nil {
		p.Blockers = append(p.Blockers, err.Error())
	} else {
		p.Chosen = &best
		p.Hyper = best.Hyper
	}
	if len(ai.Knowledge) == 0 {
		p.Warnings = append(p.Warnings, "No connected knowledge. Add catalogs, prices, or policies so answers use current facts.")
	}
	p.Ready = len(p.Blockers) == 0
	return p, nil
}

// StartTraining queues a training job for the AI on the computer Norn
// picks, or on nodeID when the person chose one.
func (s *Service) StartTraining(ctx context.Context, aiID, nodeID string) (Job, error) {
	plan, err := s.Plan(ctx, aiID)
	if err != nil {
		return Job{}, err
	}
	if nodeID != "" {
		var picked *NodeFit
		for i := range plan.Fits {
			if plan.Fits[i].NodeID == nodeID {
				picked = &plan.Fits[i]
			}
		}
		switch {
		case picked == nil:
			return Job{}, fmt.Errorf("computer %s is not online: %w", nodeID, ErrConflict)
		case !picked.Eligible:
			return Job{}, fmt.Errorf("%s cannot train this model: %s: %w", picked.NodeName, picked.Reason, ErrConflict)
		}
		plan.Chosen, plan.Hyper = picked, picked.Hyper
		// Only the choice of computer was blocking; train on the one picked.
		kept := plan.Blockers[:0]
		for _, b := range plan.Blockers {
			if !strings.HasPrefix(b, "no computer can train") {
				kept = append(kept, b)
			}
		}
		plan.Blockers = kept
		plan.Ready = len(kept) == 0
	}
	if !plan.Ready {
		return Job{}, fmt.Errorf("%s: %w", strings.Join(plan.Blockers, " "), ErrConflict)
	}
	ai, err := s.d.Repo.GetAI(ctx, aiID)
	if err != nil {
		return Job{}, err
	}
	entry, _ := s.d.Catalog(ai.BaseModelID)
	examples, err := s.validatedExamples(ctx, ai)
	if err != nil {
		return Job{}, err
	}
	trainer := s.trainerByID(plan.Chosen.Backend)
	if trainer == nil {
		return Job{}, fmt.Errorf("trainer %s is not available", plan.Chosen.Backend)
	}
	job, err := s.d.Repo.CreateJob(ctx, Job{AIID: aiID, NodeID: plan.Chosen.NodeID, NodeName: plan.Chosen.NodeName,
		Backend: trainer.ID(), Hyper: plan.Hyper, Progress: Progress{Detail: "Waiting to start", Iters: plan.Hyper.Iters, Epochs: plan.Hyper.Epochs}})
	if err != nil {
		return Job{}, err
	}
	jobCtx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.cancels[job.ID] = cancel
	s.mu.Unlock()
	s.publishJob(job)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer cancel()
		s.runJob(jobCtx, job, ai, *entry.Training, examples, trainer)
	}()
	return job, nil
}

func (s *Service) trainerByID(id string) Trainer {
	for _, t := range s.d.Trainers {
		if t.ID() == id {
			return t
		}
	}
	return nil
}

// CancelJob stops a queued or running job. Cleanup happens as the job exits.
func (s *Service) CancelJob(ctx context.Context, jobID string) (Job, error) {
	job, err := s.d.Repo.GetJob(ctx, jobID)
	if err != nil {
		return Job{}, err
	}
	if job.State.Terminal() {
		return job, nil
	}
	s.mu.Lock()
	cancel := s.cancels[jobID]
	s.mu.Unlock()
	if cancel == nil {
		// The daemon restarted; nothing is running for this row.
		now := time.Now()
		job.State, job.FinishedAt = StateCancelled, &now
		return job, s.d.Repo.SaveJob(ctx, job)
	}
	cancel()
	return job, nil
}

// Job returns one job.
func (s *Service) Job(ctx context.Context, id string) (Job, error) { return s.d.Repo.GetJob(ctx, id) }

// Jobs lists jobs for an AI, or all jobs when aiID is empty.
func (s *Service) Jobs(ctx context.Context, aiID string) ([]Job, error) {
	return s.d.Repo.ListJobs(ctx, aiID)
}

func (s *Service) publishJob(j Job) {
	s.d.Publish("training.job", map[string]any{"job": j, "ai_id": j.AIID, "state": string(j.State)})
}

// runJob carries a job from queued to a terminal state.
// execSpec is one trainer run on this computer.
// Hold is this computer, held for a training run. SetRemaining shares the
// estimate, so a chat that arrives meanwhile can say how long it will be.
type Hold interface {
	SetRemaining(d time.Duration)
	Done()
}

type execSpec struct {
	// name is what the run is called in messages, such as the AI's name.
	name                         string
	trainer                      Trainer
	repo, architecture           string
	hyper                        Hyper
	workDir, dataDir, adapterOut string
	logPath                      string
}

// execute installs the trainer environment when needed, frees memory, and
// runs the trainer. Local jobs and runs sent by a paired computer use it.
func (s *Service) execute(ctx context.Context, spec execSpec, setState func(State, string), progress func(Progress)) (RunResult, error) {
	env := spec.trainer.Environment()
	if !s.d.Python.Status(env).Installed {
		setState(StatePreparing, "Installing the trainer (one time, about 450 MB)")
	}
	python, err := s.d.Python.Ensure(ctx, env, func(step, detail string) {
		switch step {
		case "uv", "python":
			setState(StatePreparing, "Installing Python for the trainer (one time)")
		case "packages":
			setState(StatePreparing, "Installing the trainer (one time, about 450 MB)")
		}
	})
	if ctx.Err() != nil {
		return RunResult{}, ErrCancelled
	}
	if err != nil {
		return RunResult{}, fmt.Errorf("install the trainer: %w", err)
	}
	if s.d.Admit != nil {
		hold, err := s.d.Admit(ctx, spec.name, func(reason string) {
			setState(StatePreparing, reason+" before freeing memory for training")
		})
		if ctx.Err() != nil {
			return RunResult{}, ErrCancelled
		}
		if err != nil {
			return RunResult{}, err
		}
		defer hold.Done()
		inner := progress
		progress = func(p Progress) {
			if p.RemainingSec != nil {
				hold.SetRemaining(time.Duration(*p.RemainingSec) * time.Second)
			}
			inner(p)
		}
	}
	if s.d.UnloadLocalModels != nil {
		if n := s.d.UnloadLocalModels(ctx); n > 0 {
			setState(StatePreparing, fmt.Sprintf("Unloaded %d running model(s) to free memory", n))
		}
	}
	setState(StateLoading, "Loading the base model")
	return spec.trainer.Run(ctx, RunSpec{
		Python:       python,
		Env:          append(s.d.Python.Env(), "HF_HOME="+s.d.HFHome, "HF_HUB_DISABLE_TELEMETRY=1", "TOKENIZERS_PARALLELISM=false"),
		WorkDir:      spec.workDir,
		Repo:         spec.repo,
		Architecture: spec.architecture,
		Hyper:        spec.hyper,
		DataDir:      spec.dataDir,
		AdapterOut:   spec.adapterOut,
		LogPath:      spec.logPath,
	}, func(u Update) {
		p := u.Progress
		p.Detail = u.Detail
		progress(p)
		setState(u.State, u.Detail)
	})
}

// isLocal reports whether a job runs on this computer. Without a local node
// id, remote training is off and every job is local.
func (s *Service) isLocal(nodeID string) bool {
	return s.d.LocalNodeID == "" || nodeID == "" || nodeID == s.d.LocalNodeID
}

func (s *Service) runJob(ctx context.Context, job Job, ai SpecializedAI, info models.TrainingInfo, examples []Example, trainer Trainer) {
	bg := context.Background()
	var lastSave time.Time
	save := func(force bool) {
		if force || time.Since(lastSave) > 2*time.Second {
			lastSave = time.Now()
			if err := s.d.Repo.SaveJob(bg, job); err != nil {
				s.d.Logger.Warn("save training job", "job_id", job.ID, "error", err)
			}
		}
		s.publishJob(job)
	}
	setState := func(st State, detail string) {
		changed := job.State != st || job.Progress.Detail != detail
		job.State, job.Progress.Detail = st, detail
		save(changed)
	}
	work := s.workDir(job.ID)
	finish := func(st State, err error) {
		now := time.Now()
		job.State, job.FinishedAt = st, &now
		switch st {
		case StateFailed:
			job.Error = err.Error()
			job.Progress.Detail = "Failed"
		case StateCancelled:
			job.Progress.Detail = "Cancelled"
		case StateComplete:
			job.Progress.Detail = "Done"
		}
		job.Progress.RemainingSec = nil
		save(true)
		_ = os.RemoveAll(work)
		s.mu.Lock()
		delete(s.cancels, job.ID)
		s.mu.Unlock()
		s.d.Logger.Info("training job finished", "job_id", job.ID, "ai_id", job.AIID, "revision", job.Revision, "state", st)
	}

	train, valid := Split(examples)
	dataDir := filepath.Join(work, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		finish(StateFailed, err)
		return
	}
	trainJSONL, validJSONL := WriteJSONL(train, ai.Instructions), WriteJSONL(valid, ai.Instructions)
	for name, body := range map[string][]byte{"train.jsonl": trainJSONL, "valid.jsonl": validJSONL} {
		if err := os.WriteFile(filepath.Join(dataDir, name), body, 0o644); err != nil {
			finish(StateFailed, err)
			return
		}
	}
	repo, _ := trainingWeights(trainer, info, job.Hyper.Method)
	adapterDir := s.adaptersDir(ai.ID)
	if err := os.MkdirAll(adapterDir, 0o755); err != nil {
		finish(StateFailed, err)
		return
	}
	adapterPath := filepath.Join(adapterDir, fmt.Sprintf("rev-%d.gguf", job.Revision))
	progress := func(p Progress) {
		if p.Iters == 0 {
			p.Iters, p.Epochs = job.Progress.Iters, job.Progress.Epochs
		}
		job.Progress = p
	}

	var res RunResult
	var err error
	if s.isLocal(job.NodeID) {
		select {
		case s.slot <- struct{}{}:
		case <-ctx.Done():
			finish(StateCancelled, nil)
			return
		}
		now := time.Now()
		job.StartedAt = &now
		setState(StatePreparing, "Preparing examples")
		res, err = s.execute(ctx, execSpec{
			name: ai.Name, trainer: trainer, repo: repo, architecture: info.Architecture, hyper: job.Hyper,
			workDir: work, dataDir: dataDir, adapterOut: adapterPath,
			logPath: filepath.Join(s.d.LogsDir, "training-"+job.ID+".log"),
		}, setState, progress)
		<-s.slot
	} else {
		now := time.Now()
		job.StartedAt = &now
		res, err = s.runOnPeer(ctx, job, RemoteRunRequest{
			ID: job.ID, Repo: repo, Architecture: info.Architecture, Hyper: job.Hyper,
			TrainJSONL: string(trainJSONL), ValidJSONL: string(validJSONL),
		}, adapterPath, setState, progress)
	}
	if errors.Is(err, ErrCancelled) || ctx.Err() != nil {
		_ = os.Remove(adapterPath)
		finish(StateCancelled, nil)
		return
	}
	if err != nil {
		_ = os.Remove(adapterPath)
		finish(StateFailed, err)
		return
	}

	rev := Revision{AIID: ai.ID, Revision: job.Revision, JobID: job.ID, BaseModelID: ai.BaseModelID, Backend: trainer.ID(),
		Hyper: job.Hyper, ExampleCount: len(train) + len(valid), ExamplesHash: HashExamples(examples),
		FinalLoss: res.TrainLoss, ValLoss: res.ValLoss, adapterPath: res.AdapterPath}
	if err := s.d.Repo.AddRevision(bg, rev); err != nil {
		_ = os.Remove(adapterPath)
		finish(StateFailed, err)
		return
	}

	// Evaluate against the base model when the base GGUF is installed.
	if s.d.Installed == nil || s.d.Installed(bg, ai.BaseModelID) {
		setState(StateEvaluate, "Comparing base and specialized answers")
		if _, err := s.evaluate(ctx, ai, rev, valid); err != nil && ctx.Err() == nil {
			s.d.Logger.Warn("evaluation after training failed", "ai_id", ai.ID, "revision", rev.Revision, "error", err)
		}
	}
	finish(StateComplete, nil)
}

// SetTestPrompts replaces the AI's test set.
func (s *Service) SetTestPrompts(ctx context.Context, aiID string, prompts []string) ([]EvalPrompt, error) {
	if _, err := s.d.Repo.GetAI(ctx, aiID); err != nil {
		return nil, err
	}
	return s.d.Repo.SetEvalPrompts(ctx, aiID, prompts)
}

// Evaluate compares a revision with its base model on the AI's test set.
func (s *Service) Evaluate(ctx context.Context, aiID string, revision int) (EvalRun, error) {
	ai, err := s.d.Repo.GetAI(ctx, aiID)
	if err != nil {
		return EvalRun{}, err
	}
	rev, err := s.d.Repo.GetRevision(ctx, aiID, revision)
	if err != nil {
		return EvalRun{}, err
	}
	examples, err := s.validatedExamples(ctx, ai)
	if err != nil {
		return EvalRun{}, err
	}
	_, valid := Split(examples)
	return s.evaluate(ctx, ai, rev, valid)
}

// defaultTestPrompts takes held-out questions so the model is tested on
// prompts it did not train on.
func defaultTestPrompts(valid []Example) []string {
	var out []string
	for _, ex := range valid {
		for _, m := range ex.Messages {
			if m.Role == "user" && strings.TrimSpace(m.Content) != "" {
				out = append(out, m.Content)
				break
			}
		}
		if len(out) == 5 {
			break
		}
	}
	return out
}

func (s *Service) evaluate(ctx context.Context, ai SpecializedAI, rev Revision, valid []Example) (EvalRun, error) {
	if s.d.Generate == nil {
		return EvalRun{}, fmt.Errorf("evaluation is not available")
	}
	prompts, err := s.d.Repo.EvalPrompts(ctx, ai.ID)
	if err != nil {
		return EvalRun{}, err
	}
	if len(prompts) == 0 {
		if prompts, err = s.d.Repo.SetEvalPrompts(ctx, ai.ID, defaultTestPrompts(valid)); err != nil {
			return EvalRun{}, err
		}
	}
	if len(prompts) == 0 {
		return EvalRun{}, fmt.Errorf("add at least one test prompt")
	}
	run, err := s.d.Repo.SaveEvalRun(ctx, EvalRun{AIID: ai.ID, Revision: rev.Revision, Status: "running"})
	if err != nil {
		return EvalRun{}, err
	}
	s.d.Publish("training.eval", map[string]any{"ai_id": ai.ID, "run": run})

	s.mu.Lock()
	s.evaluating[ai.BaseModelID] = append(s.evaluating[ai.BaseModelID], rev)
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		list := s.evaluating[ai.BaseModelID]
		for i, r := range list {
			if r.AIID == rev.AIID && r.Revision == rev.Revision {
				list = append(list[:i], list[i+1:]...)
				break
			}
		}
		s.evaluating[ai.BaseModelID] = list
		s.mu.Unlock()
	}()

	loaded, err := s.AdaptersFor(ctx, ai.BaseModelID)
	if err != nil {
		return EvalRun{}, err
	}
	for _, p := range prompts {
		if ctx.Err() != nil {
			break
		}
		msgs := s.turnMessages(ctx, ai, p.Prompt)
		res := EvalResult{Prompt: p.Prompt}
		base, err := s.d.Generate(ctx, ai.BaseModelID, "", loaded, msgs)
		if err == nil {
			res.Base = base
			res.Specialized, err = s.d.Generate(ctx, ai.BaseModelID, rev.AdapterID(), loaded, msgs)
		}
		if err != nil {
			res.Error = err.Error()
		}
		run.Results = append(run.Results, res)
		s.d.Publish("training.eval", map[string]any{"ai_id": ai.ID, "run": run})
	}
	now := time.Now()
	run.FinishedAt = &now
	run.Status = "complete"
	failed := 0
	for _, r := range run.Results {
		if r.Error != "" {
			failed++
		}
	}
	if ctx.Err() != nil {
		run.Status, run.Error = "cancelled", "Stopped before every prompt ran."
	} else if failed == len(run.Results) {
		run.Status, run.Error = "failed", run.Results[0].Error
	}
	run, err = s.d.Repo.SaveEvalRun(context.Background(), run)
	s.d.Publish("training.eval", map[string]any{"ai_id": ai.ID, "run": run})
	return run, err
}

// EvaluateAsync starts an evaluation in the background.
func (s *Service) EvaluateAsync(aiID string, revision int) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if _, err := s.Evaluate(context.Background(), aiID, revision); err != nil {
			s.d.Logger.Warn("evaluation failed", "ai_id", aiID, "revision", revision, "error", err)
			s.d.Publish("training.eval", map[string]any{"ai_id": aiID, "error": err.Error()})
		}
	}()
}

// turnMessages builds the messages both sides of an evaluation see: the
// AI's instructions and its connected knowledge for the prompt.
func (s *Service) turnMessages(ctx context.Context, ai SpecializedAI, prompt string) []pluginapi.ChatMessage {
	reference := ""
	if len(ai.Knowledge) > 0 && s.d.Knowledge != nil {
		if hits, err := s.d.Knowledge.Search(ctx, mimir.SearchInput{Query: prompt, SourceIDs: ai.Knowledge}); err == nil {
			reference = mimir.ContextBlock(hits, 0)
		}
	}
	var msgs []pluginapi.ChatMessage
	if strings.TrimSpace(ai.Instructions) != "" {
		msgs = append(msgs, pluginapi.ChatMessage{Role: "system", Content: ai.Instructions})
	}
	return append(msgs, pluginapi.ChatMessage{Role: "user", Content: mimir.WithReference(prompt, reference)})
}

// Deploy makes a revision the one chat and the API use. The revision must
// have a finished evaluation.
func (s *Service) Deploy(ctx context.Context, aiID string, revision int) (SpecializedAI, error) {
	ai, err := s.d.Repo.GetAI(ctx, aiID)
	if err != nil {
		return SpecializedAI{}, err
	}
	rev, err := s.d.Repo.GetRevision(ctx, aiID, revision)
	if err != nil {
		return SpecializedAI{}, err
	}
	if !rev.Evaluated {
		return SpecializedAI{}, fmt.Errorf("compare revision %d with the base model before deploying it: %w", revision, ErrConflict)
	}
	if _, err := os.Stat(rev.adapterPath); err != nil {
		return SpecializedAI{}, fmt.Errorf("the adapter for revision %d is missing: %w", revision, err)
	}
	ai.DeployedRevision = revision
	ai, err = s.d.Repo.UpdateAI(ctx, ai)
	if err == nil {
		s.d.Publish("training.deployed", map[string]any{"ai_id": ai.ID, "revision": revision, "model_id": ai.ModelID()})
	}
	return ai, err
}

// Undeploy removes an AI from chat and the API. Its revisions are kept.
func (s *Service) Undeploy(ctx context.Context, aiID string) (SpecializedAI, error) {
	ai, err := s.d.Repo.GetAI(ctx, aiID)
	if err != nil {
		return SpecializedAI{}, err
	}
	ai.DeployedRevision = 0
	return s.d.Repo.UpdateAI(ctx, ai)
}

// Resolved is a deployed specialized AI ready for chat.
type Resolved struct {
	AI          SpecializedAI
	BaseModelID string
	Adapter     string
	// Loaded is every adapter the base model's process should hold.
	Loaded []pluginapi.Adapter
}

// Resolve turns a sai: model id into its base model, adapter, instructions,
// and knowledge.
func (s *Service) Resolve(ctx context.Context, modelID string) (Resolved, error) {
	slug := strings.TrimPrefix(modelID, ModelPrefix)
	ai, err := s.d.Repo.GetAIBySlug(ctx, slug)
	if err != nil {
		return Resolved{}, err
	}
	if ai.DeployedRevision == 0 {
		return Resolved{}, fmt.Errorf("%s is not deployed yet. Finish training and deploy it on the Train page", ai.Name)
	}
	rev, err := s.d.Repo.GetRevision(ctx, ai.ID, ai.DeployedRevision)
	if err != nil {
		return Resolved{}, err
	}
	if _, err := os.Stat(rev.adapterPath); err != nil {
		return Resolved{}, fmt.Errorf("%s's trained adapter is missing on this computer. Train and deploy it again on the Train page", ai.Name)
	}
	loaded, err := s.AdaptersFor(ctx, rev.BaseModelID)
	if err != nil {
		return Resolved{}, err
	}
	return Resolved{AI: ai, BaseModelID: rev.BaseModelID, Adapter: rev.AdapterID(), Loaded: loaded}, nil
}

// AdaptersFor lists the adapters a base model's process should load: every
// deployed revision on that base, plus revisions under evaluation. The order
// is stable so a running process is reused while the set is unchanged.
func (s *Service) AdaptersFor(ctx context.Context, baseModelID string) ([]pluginapi.Adapter, error) {
	ais, err := s.d.Repo.ListAIs(ctx)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []pluginapi.Adapter
	add := func(rev Revision) {
		id := rev.AdapterID()
		if seen[id] || rev.BaseModelID != baseModelID {
			return
		}
		if _, err := os.Stat(rev.adapterPath); err != nil {
			return
		}
		seen[id] = true
		out = append(out, pluginapi.Adapter{ID: id, Path: rev.adapterPath})
	}
	for _, ai := range ais {
		if ai.DeployedRevision == 0 {
			continue
		}
		if rev, err := s.d.Repo.GetRevision(ctx, ai.ID, ai.DeployedRevision); err == nil {
			add(rev)
		}
	}
	s.mu.Lock()
	pending := append([]Revision(nil), s.evaluating[baseModelID]...)
	s.mu.Unlock()
	for _, rev := range pending {
		if full, err := s.d.Repo.GetRevision(ctx, rev.AIID, rev.Revision); err == nil {
			add(full)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// DeployedModels lists deployed AIs as models for chat and /v1/models.
func (s *Service) DeployedModels(ctx context.Context) ([]contracts.Model, error) {
	ais, err := s.d.Repo.ListAIs(ctx)
	if err != nil {
		return nil, err
	}
	out := []contracts.Model{}
	for _, ai := range ais {
		if ai.DeployedRevision == 0 {
			continue
		}
		rev, err := s.d.Repo.GetRevision(ctx, ai.ID, ai.DeployedRevision)
		if err != nil {
			continue
		}
		m := contracts.Model{ID: ai.ModelID(), DisplayName: ai.Name, Summary: ai.Goal, Status: "installed", Installed: true,
			Tags: []string{"specialized"}}
		if entry, ok := s.d.Catalog(rev.BaseModelID); ok {
			m.Family, m.Parameters, m.Context, m.Capabilities = entry.Family, entry.Parameters, entry.Context, entry.Capabilities
			m.Capabilities.ToolCalling = false
			m.Variant = fmt.Sprintf("%s + revision %d", entry.DisplayName, rev.Revision)
		}
		if s.d.Installed != nil && !s.d.Installed(ctx, rev.BaseModelID) {
			m.Status, m.Installed = "base_missing", false
		}
		out = append(out, m)
	}
	return out, nil
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

func removeString(list []string, v string) []string {
	out := list[:0]
	for _, x := range list {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}
