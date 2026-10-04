package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/mimir"
	"github.com/yeixio/toskar-core/internal/runtimes/llamacpp"
	"github.com/yeixio/toskar-core/internal/share"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// knowledgeModels runs installed embedding and reranker models for Mimir
// (spec §61). Each runs in its own small llama-server on this computer,
// started when first needed and unloaded by the idle sweeper like any model.
//
// Loading follows the share gate (§60): a search inside a chat may load a
// model, because chat comes first, but nothing is loaded while training
// holds the computer. Then search uses keywords, and background embedding
// waits until training ends.
type knowledgeModels struct {
	a *App
	// startMu keeps two searches from starting the same model twice.
	startMu sync.Mutex
	// failed maps a model id to when it last failed to start.
	failed sync.Map
	// installed lists installed models; tests replace it.
	installed func(ctx context.Context) []contracts.Model
	// start loads a model in a mode and returns its endpoint; tests replace it.
	start func(ctx context.Context, modelID, mode string) (string, error)
}

// supportRetry is how long a supporting model that failed to start is left
// alone, so a broken file does not slow every search.
const supportRetry = 10 * time.Minute

func newKnowledgeModels(a *App) *knowledgeModels {
	k := &knowledgeModels{a: a, installed: a.installedModels}
	k.start = k.startLocal
	return k
}

func (k *knowledgeModels) Embedder(ctx context.Context) (mimir.Embedder, error) {
	m, endpoint, err := k.endpoint(ctx, contracts.SupportEmbedding, pluginapi.ModeEmbedding)
	if err != nil || endpoint == "" {
		return nil, err
	}
	doc, query := embedPrefixes(m)
	return &llamaEmbedder{k: k, model: m.ID, endpoint: endpoint, docPrefix: doc, queryPrefix: query}, nil
}

func (k *knowledgeModels) Reranker(ctx context.Context) (mimir.Reranker, error) {
	m, endpoint, err := k.endpoint(ctx, contracts.SupportReranker, pluginapi.ModeReranking)
	if err != nil || endpoint == "" {
		return nil, err
	}
	return &llamaReranker{k: k, model: m.ID, endpoint: endpoint}, nil
}

// pick chooses the installed model for a role: a catalog model before one
// installed by URL, then by id, so the choice (and the stored vectors) stays
// the same from one search to the next.
func (k *knowledgeModels) pick(ctx context.Context, role string) (contracts.Model, bool) {
	var list []contracts.Model
	for _, m := range k.installed(ctx) {
		if m.Status == "installed" && contracts.SupportRoleOf(m) == role {
			list = append(list, m)
		}
	}
	if len(list) == 0 {
		return contracts.Model{}, false
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Dynamic != list[j].Dynamic {
			return !list[i].Dynamic
		}
		// A multilingual model finds passages across languages (multilingual
		// spec §19), so the one rated in the most languages is used.
		if len(list[i].Languages) != len(list[j].Languages) {
			return len(list[i].Languages) > len(list[j].Languages)
		}
		return list[i].ID < list[j].ID
	})
	return list[0], true
}

// endpoint returns where the model for role is answering, loading it when
// that is allowed now. An empty endpoint with no error means no such model
// is installed.
func (k *knowledgeModels) endpoint(ctx context.Context, role, mode string) (contracts.Model, string, error) {
	a := k.a
	if a.stubInference {
		return contracts.Model{}, "", nil
	}
	m, ok := k.pick(ctx, role)
	if !ok {
		return contracts.Model{}, "", nil
	}
	if ep := k.running(ctx, m.ID, mode); ep != "" {
		return m, ep, nil
	}
	if t, ok := k.failed.Load(m.ID); ok && time.Since(t.(time.Time)) < supportRetry {
		return m, "", mimir.ErrNotNow
	}
	if a.Share != nil {
		if _, busy := a.Share.Running(share.Training); busy {
			return m, "", mimir.ErrNotNow
		}
	}

	k.startMu.Lock()
	defer k.startMu.Unlock()
	if ep := k.running(ctx, m.ID, mode); ep != "" {
		return m, ep, nil
	}
	ep, err := k.start(ctx, m.ID, mode)
	if err != nil {
		if ctx.Err() == nil {
			k.failed.Store(m.ID, time.Now())
			if a.Logger != nil {
				a.Logger.Warn("supporting model did not start; knowledge search uses keywords", "model", m.ID, "mode", mode, "error", err)
			}
		}
		return m, "", mimir.ErrNotNow
	}
	k.failed.Delete(m.ID)
	if a.Logger != nil {
		a.Logger.Info("supporting model loaded for knowledge search", "model", m.ID, "mode", mode)
	}
	return m, ep, nil
}

// running returns the endpoint of a loaded instance of the model in mode.
func (k *knowledgeModels) running(ctx context.Context, modelID, mode string) string {
	if k.a.Runtimes == nil {
		return ""
	}
	list, err := k.a.Runtimes.ListRunning(ctx, "llamacpp")
	if err != nil {
		return ""
	}
	for _, r := range list {
		if r.ModelID == modelID && r.Mode == mode && r.Status == "running" {
			return r.Endpoint
		}
	}
	return ""
}

func (k *knowledgeModels) startLocal(ctx context.Context, modelID, mode string) (string, error) {
	a := k.a
	if a.Models == nil || a.Runtimes == nil {
		return "", fmt.Errorf("models are not available")
	}
	path, err := a.Models.Path(ctx, modelID)
	if err != nil {
		return "", err
	}
	running, err := a.Runtimes.StartModel(ctx, "llamacpp", pluginapi.ModelStartConfig{ModelID: modelID, ModelPath: path, Mode: mode})
	if err != nil {
		return "", err
	}
	a.trackRunning(running)
	a.markModelUsed(modelID)
	return running.Endpoint, nil
}

// used keeps a supporting model from being unloaded while it is in use, and
// lets a failure be retried later instead of on every search.
func (k *knowledgeModels) used(modelID string, err error) {
	if err == nil {
		k.a.markModelUsed(modelID)
		return
	}
	if k.a.Logger != nil {
		k.a.Logger.Warn("supporting model failed; knowledge search uses keywords", "model", modelID, "error", err)
	}
}

// embedPrefixes are the task prefixes an embedding family was trained with.
// Without them, retrieval quality drops noticeably.
func embedPrefixes(m contracts.Model) (doc, query string) {
	name := strings.ToLower(m.ID + " " + m.DisplayName + " " + m.Family)
	switch {
	case strings.Contains(name, "nomic"):
		return "search_document: ", "search_query: "
	case strings.Contains(name, "e5"):
		return "passage: ", "query: "
	case strings.Contains(name, "mxbai"), strings.Contains(name, "bge") && !strings.Contains(name, "m3"):
		return "", "Represent this sentence for searching relevant passages: "
	}
	return "", ""
}

type llamaEmbedder struct {
	k                      *knowledgeModels
	model, endpoint        string
	docPrefix, queryPrefix string
}

func (e *llamaEmbedder) ModelID() string { return e.model }

func (e *llamaEmbedder) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	in := make([]string, len(texts))
	for i, t := range texts {
		in[i] = e.docPrefix + t
	}
	vecs, err := llamacpp.Embed(ctx, e.endpoint, in)
	e.k.used(e.model, err)
	return vecs, err
}

func (e *llamaEmbedder) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	vecs, err := llamacpp.Embed(ctx, e.endpoint, []string{e.queryPrefix + text})
	e.k.used(e.model, err)
	if err != nil {
		return nil, err
	}
	return vecs[0], nil
}

type llamaReranker struct {
	k               *knowledgeModels
	model, endpoint string
}

func (r *llamaReranker) Rerank(ctx context.Context, query string, passages []string) ([]float64, error) {
	scores, err := llamacpp.Rerank(ctx, r.endpoint, query, passages)
	r.k.used(r.model, err)
	return scores, err
}

// admitIndexing lets background embedding use the computer for one batch
// (§60): after chat and automations, before benchmarks and training. While
// training runs it does not start at all, because loading a model then could
// take memory training needs.
func (a *App) admitIndexing(ctx context.Context) (func(), error) {
	if a.Share == nil {
		return func() {}, nil
	}
	if _, busy := a.Share.Running(share.Training); busy {
		return nil, mimir.ErrNotNow
	}
	w, err := a.Share.Enter(ctx, share.Indexing, "knowledge", nil)
	if err != nil {
		return nil, err
	}
	return w.Done, nil
}

// indexKnowledge starts background embedding, and restarts it when a model
// finishes downloading, since it may be an embedding model.
func (a *App) indexKnowledge(ctx context.Context) {
	if a.Mimir == nil {
		return
	}
	a.Mimir.StartIndexing(ctx, a.admitIndexing)
	if a.Bus == nil {
		return
	}
	id, ch := a.Bus.Subscribe()
	go func() {
		defer a.Bus.Unsubscribe(id)
		for {
			select {
			case <-ctx.Done():
				return
			case evt, ok := <-ch:
				if !ok {
					return
				}
				if evt.Type == events.ModelDownloadCompleted {
					a.Mimir.Kick()
				}
			}
		}
	}()
}
