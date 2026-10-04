package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/yeixio/toskar-core/internal/mimir"
	"github.com/yeixio/toskar-core/internal/models"
	"github.com/yeixio/toskar-core/internal/training"
)

// BindTraining attaches the Train Your Own AI routes. catalog lists the
// model catalog for base-model recommendations.
func (s *Server) BindTraining(svc *training.Service, catalog func() []models.CatalogEntry) {
	s.training = svc
	s.trainingCatalog = catalog
}

func (s *Server) trainingRoutes(api *mux.Router) {
	t := api.PathPrefix("/training").Subrouter()
	t.HandleFunc("/backends", s.trainingHandler(s.handleTrainingBackends)).Methods(http.MethodGet, http.MethodOptions)
	t.HandleFunc("/base-models", s.trainingHandler(s.handleBaseModels)).Methods(http.MethodGet, http.MethodOptions)
	t.HandleFunc("/classify", s.trainingHandler(s.handleClassifyMaterial)).Methods(http.MethodPost, http.MethodOptions)
	t.HandleFunc("/samples", s.trainingHandler(s.handleSamples)).Methods(http.MethodGet, http.MethodOptions)
	t.HandleFunc("/example", s.trainingHandler(s.handleCreateExample)).Methods(http.MethodPost)
	t.HandleFunc("/deployed", s.trainingHandler(s.handleDeployedAIs)).Methods(http.MethodGet, http.MethodOptions)
	t.HandleFunc("/ais", s.trainingHandler(s.handleListAIs)).Methods(http.MethodGet, http.MethodOptions)
	t.HandleFunc("/ais", s.trainingHandler(s.handleCreateAI)).Methods(http.MethodPost)
	t.HandleFunc("/ais/{id}", s.trainingHandler(s.handleGetAI)).Methods(http.MethodGet, http.MethodOptions)
	t.HandleFunc("/ais/{id}", s.trainingHandler(s.handlePatchAI)).Methods(http.MethodPatch)
	t.HandleFunc("/ais/{id}", s.trainingHandler(s.handleDeleteAI)).Methods(http.MethodDelete)
	t.HandleFunc("/ais/{id}/materials", s.trainingHandler(s.handleAddMaterial)).Methods(http.MethodPost)
	t.HandleFunc("/ais/{id}/materials/{mid}", s.trainingHandler(s.handleDeleteMaterial)).Methods(http.MethodDelete)
	t.HandleFunc("/ais/{id}/conversations", s.trainingHandler(s.handleAddConversations)).Methods(http.MethodPost)
	t.HandleFunc("/ais/{id}/examples", s.trainingHandler(s.handleListExamples)).Methods(http.MethodGet, http.MethodOptions)
	t.HandleFunc("/ais/{id}/examples", s.trainingHandler(s.handleAddExample)).Methods(http.MethodPost)
	t.HandleFunc("/ais/{id}/examples/{eid}", s.trainingHandler(s.handlePatchExample)).Methods(http.MethodPatch)
	t.HandleFunc("/ais/{id}/examples/{eid}", s.trainingHandler(s.handleDeleteExample)).Methods(http.MethodDelete)
	t.HandleFunc("/ais/{id}/plan", s.trainingHandler(s.handlePlan)).Methods(http.MethodGet, http.MethodOptions)
	t.HandleFunc("/ais/{id}/train", s.trainingHandler(s.handleStartTraining)).Methods(http.MethodPost)
	t.HandleFunc("/ais/{id}/test-prompts", s.trainingHandler(s.handleSetTestPrompts)).Methods(http.MethodPut)
	t.HandleFunc("/ais/{id}/revisions/{rev}/evaluate", s.trainingHandler(s.handleEvaluate)).Methods(http.MethodPost)
	t.HandleFunc("/ais/{id}/revisions/{rev}/deploy", s.trainingHandler(s.handleDeploy)).Methods(http.MethodPost)
	t.HandleFunc("/ais/{id}/revisions/{rev}/export", s.trainingHandler(s.handleExportStatus)).Methods(http.MethodGet, http.MethodOptions)
	t.HandleFunc("/ais/{id}/revisions/{rev}/export", s.trainingHandler(s.handleExport)).Methods(http.MethodPost)
	t.HandleFunc("/ais/{id}/revisions/{rev}/export", s.trainingHandler(s.handleDeleteExport)).Methods(http.MethodDelete)
	t.HandleFunc("/ais/{id}/revisions/{rev}/export/file", s.trainingHandler(s.handleExportFile)).Methods(http.MethodGet, http.MethodOptions)
	t.HandleFunc("/ais/{id}/undeploy", s.trainingHandler(s.handleUndeploy)).Methods(http.MethodPost)
	t.HandleFunc("/jobs", s.trainingHandler(s.handleListJobs)).Methods(http.MethodGet, http.MethodOptions)
	t.HandleFunc("/jobs/{id}", s.trainingHandler(s.handleGetJob)).Methods(http.MethodGet, http.MethodOptions)
	t.HandleFunc("/jobs/{id}/cancel", s.trainingHandler(s.handleCancelJob)).Methods(http.MethodPost)
}

func (s *Server) trainingHandler(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.training == nil {
			writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Training is not available.", nil)
			return
		}
		h(w, r)
	}
}

func writeTrainingErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, training.ErrNotFound), errors.Is(err, mimir.ErrNotFound):
		writeErrFrom(w, http.StatusNotFound, "NOT_FOUND", err)
	case errors.Is(err, training.ErrConflict):
		writeErrFrom(w, http.StatusConflict, "CONFLICT", err)
	default:
		writeErrFrom(w, http.StatusBadRequest, "BAD_REQUEST", err)
	}
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, mimir.MaxTextBytes+1<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "BAD_REQUEST", "invalid request body", nil)
		return false
	}
	return true
}

func (s *Server) handleTrainingBackends(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.training.Backends(r.Context()))
}

func (s *Server) handleBaseModels(w http.ResponseWriter, r *http.Request) {
	var catalog []models.CatalogEntry
	if s.trainingCatalog != nil {
		catalog = s.trainingCatalog()
	}
	out, err := s.training.RecommendBases(r.Context(), r.URL.Query().Get("goal"), catalog)
	if err != nil {
		writeTrainingErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleClassifyMaterial(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Filename string `json:"filename"`
		Text     string `json:"text"`
		// Use, when set, previews the warning for that choice.
		Use           training.Use `json:"use,omitempty"`
		ContentBase64 string       `json:"content_base64,omitempty"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	name, text, err := training.MaterialText(in.Filename, in.Text, in.ContentBase64)
	if err != nil {
		writeTrainingErr(w, err)
		return
	}
	rec := s.training.Classify(name, text)
	out := map[string]any{"recommendation": rec}
	use := in.Use
	if use == "" {
		use = rec.Use
	}
	warning, err := training.ChoiceWarning(rec, use)
	out["use"], out["warning"] = use, warning
	if err != nil {
		out["error"] = err.Error()
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleSamples(w http.ResponseWriter, r *http.Request) {
	files, err := training.Samples()
	if err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "INTERNAL_ERROR", err)
		return
	}
	writeJSON(w, http.StatusOK, files)
}

func (s *Server) handleCreateExample(w http.ResponseWriter, r *http.Request) {
	var catalog []models.CatalogEntry
	if s.trainingCatalog != nil {
		catalog = s.trainingCatalog()
	}
	ai, err := s.training.CreateExample(r.Context(), catalog)
	if err != nil {
		writeTrainingErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ai)
}

func (s *Server) handleDeployedAIs(w http.ResponseWriter, r *http.Request) {
	out, err := s.training.DeployedModels(r.Context())
	if err != nil {
		writeTrainingErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleListAIs(w http.ResponseWriter, r *http.Request) {
	out, err := s.training.ListAIs(r.Context())
	if err != nil {
		writeTrainingErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateAI(w http.ResponseWriter, r *http.Request) {
	var in training.CreateInput
	if !decodeBody(w, r, &in) {
		return
	}
	ai, err := s.training.CreateAI(r.Context(), in)
	if err != nil {
		writeTrainingErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, ai)
}

func (s *Server) handleGetAI(w http.ResponseWriter, r *http.Request) {
	v, err := s.training.View(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeTrainingErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handlePatchAI(w http.ResponseWriter, r *http.Request) {
	var p training.Patch
	if !decodeBody(w, r, &p) {
		return
	}
	ai, err := s.training.UpdateAI(r.Context(), mux.Vars(r)["id"], p)
	if err != nil {
		writeTrainingErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ai)
}

func (s *Server) handleDeleteAI(w http.ResponseWriter, r *http.Request) {
	if err := s.training.DeleteAI(r.Context(), mux.Vars(r)["id"]); err != nil {
		writeTrainingErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAddMaterial(w http.ResponseWriter, r *http.Request) {
	var in training.MaterialInput
	if !decodeBody(w, r, &in) {
		return
	}
	m, err := s.training.AddMaterial(r.Context(), mux.Vars(r)["id"], in)
	if err != nil {
		writeTrainingErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

func (s *Server) handleDeleteMaterial(w http.ResponseWriter, r *http.Request) {
	v := mux.Vars(r)
	if err := s.training.RemoveMaterial(r.Context(), v["id"], v["mid"]); err != nil {
		writeTrainingErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAddConversations(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ConversationIDs []string `json:"conversation_ids"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	m, err := s.training.AddConversations(r.Context(), mux.Vars(r)["id"], in.ConversationIDs)
	if err != nil {
		writeTrainingErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

func (s *Server) handleListExamples(w http.ResponseWriter, r *http.Request) {
	examples, stats, err := s.training.Examples(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeTrainingErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"examples": examples, "stats": stats})
}

func (s *Server) handleAddExample(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Messages []training.Message `json:"messages"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	if err := s.training.AddExample(r.Context(), mux.Vars(r)["id"], in.Messages); err != nil {
		writeTrainingErr(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) handlePatchExample(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Messages []training.Message `json:"messages,omitempty"`
		Excluded *bool              `json:"excluded,omitempty"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	v := mux.Vars(r)
	if err := s.training.UpdateExample(r.Context(), v["id"], v["eid"], in.Messages, in.Excluded); err != nil {
		writeTrainingErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteExample(w http.ResponseWriter, r *http.Request) {
	v := mux.Vars(r)
	if err := s.training.DeleteExample(r.Context(), v["id"], v["eid"]); err != nil {
		writeTrainingErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePlan(w http.ResponseWriter, r *http.Request) {
	p, err := s.training.Plan(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeTrainingErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleStartTraining(w http.ResponseWriter, r *http.Request) {
	// An optional body picks the computer: {"node_id": "..."}.
	var in struct {
		NodeID string `json:"node_id"`
	}
	if r.ContentLength > 0 && !decodeBody(w, r, &in) {
		return
	}
	job, err := s.training.StartTraining(r.Context(), mux.Vars(r)["id"], in.NodeID)
	if err != nil {
		writeTrainingErr(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func (s *Server) handleSetTestPrompts(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Prompts []string `json:"prompts"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	out, err := s.training.SetTestPrompts(r.Context(), mux.Vars(r)["id"], in.Prompts)
	if err != nil {
		writeTrainingErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func revisionVar(w http.ResponseWriter, r *http.Request) (int, bool) {
	rev, err := strconv.Atoi(mux.Vars(r)["rev"])
	if err != nil || rev < 1 {
		writeErr(w, http.StatusBadRequest, "BAD_REQUEST", "revision must be a positive number", nil)
		return 0, false
	}
	return rev, true
}

func (s *Server) handleEvaluate(w http.ResponseWriter, r *http.Request) {
	rev, ok := revisionVar(w, r)
	if !ok {
		return
	}
	id := mux.Vars(r)["id"]
	// Check the revision exists before answering 202.
	if _, err := s.training.View(r.Context(), id); err != nil {
		writeTrainingErr(w, err)
		return
	}
	s.training.EvaluateAsync(id, rev)
	writeJSON(w, http.StatusAccepted, map[string]any{"ai_id": id, "revision": rev, "status": "running"})
}

func (s *Server) handleDeploy(w http.ResponseWriter, r *http.Request) {
	rev, ok := revisionVar(w, r)
	if !ok {
		return
	}
	ai, err := s.training.Deploy(r.Context(), mux.Vars(r)["id"], rev)
	if err != nil {
		writeTrainingErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ai)
}

func (s *Server) handleExportStatus(w http.ResponseWriter, r *http.Request) {
	rev, ok := revisionVar(w, r)
	if !ok {
		return
	}
	st, err := s.training.ExportStatus(r.Context(), mux.Vars(r)["id"], rev)
	if err != nil {
		writeTrainingErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	rev, ok := revisionVar(w, r)
	if !ok {
		return
	}
	st, err := s.training.Export(r.Context(), mux.Vars(r)["id"], rev)
	if err != nil {
		writeTrainingErr(w, err)
		return
	}
	code := http.StatusAccepted
	if st.State == training.ExportReady {
		code = http.StatusOK
	}
	writeJSON(w, code, st)
}

func (s *Server) handleDeleteExport(w http.ResponseWriter, r *http.Request) {
	rev, ok := revisionVar(w, r)
	if !ok {
		return
	}
	if err := s.training.DeleteExport(r.Context(), mux.Vars(r)["id"], rev); err != nil {
		writeTrainingErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleExportFile downloads an exported GGUF. Files are several GB, so it
// streams from disk and supports range requests for resumed downloads.
func (s *Server) handleExportFile(w http.ResponseWriter, r *http.Request) {
	rev, ok := revisionVar(w, r)
	if !ok {
		return
	}
	path, name, err := s.training.ExportFile(r.Context(), mux.Vars(r)["id"], rev)
	if err != nil {
		writeTrainingErr(w, err)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		writeTrainingErr(w, err)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		writeTrainingErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", asciiName(name)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, "", info.ModTime(), f)
}

func (s *Server) handleUndeploy(w http.ResponseWriter, r *http.Request) {
	ai, err := s.training.Undeploy(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeTrainingErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ai)
}

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.training.Jobs(r.Context(), r.URL.Query().Get("ai_id"))
	if err != nil {
		writeTrainingErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, jobs)
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	job, err := s.training.Job(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeTrainingErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	job, err := s.training.CancelJob(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeTrainingErr(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}
