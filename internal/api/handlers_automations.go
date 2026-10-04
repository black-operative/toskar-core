package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/yeixio/toskar-core/internal/automations"
)

func (s *Server) handleListAutomations(w http.ResponseWriter, r *http.Request) {
	if s.deps.ListAutomations == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Automations are not available.", nil)
		return
	}
	items, err := s.deps.ListAutomations(r.Context())
	if err != nil {
		writeAutomationErr(w, err)
		return
	}
	if items == nil {
		items = []automations.Automation{}
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleCreateAutomation(w http.ResponseWriter, r *http.Request) {
	if s.deps.CreateAutomation == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Automations are not available.", nil)
		return
	}
	var in automations.CreateInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "BAD_REQUEST", "invalid automation", nil)
		return
	}
	created, err := s.deps.CreateAutomation(r.Context(), in)
	if err != nil {
		writeAutomationErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleGetAutomation(w http.ResponseWriter, r *http.Request) {
	if s.deps.GetAutomation == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Automations are not available.", nil)
		return
	}
	detail, err := s.deps.GetAutomation(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeAutomationErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleUpdateAutomation(w http.ResponseWriter, r *http.Request) {
	if s.deps.UpdateAutomation == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Automations are not available.", nil)
		return
	}
	var patch automations.Patch
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeErr(w, http.StatusBadRequest, "BAD_REQUEST", "invalid automation", nil)
		return
	}
	updated, err := s.deps.UpdateAutomation(r.Context(), mux.Vars(r)["id"], patch)
	if err != nil {
		writeAutomationErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleDeleteAutomation(w http.ResponseWriter, r *http.Request) {
	if s.deps.DeleteAutomation == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Automations are not available.", nil)
		return
	}
	if err := s.deps.DeleteAutomation(r.Context(), mux.Vars(r)["id"]); err != nil {
		writeAutomationErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePreviewAutomation(w http.ResponseWriter, r *http.Request) {
	if s.deps.PreviewAutomation == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Automations are not available.", nil)
		return
	}
	var in automations.CreateInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "BAD_REQUEST", "invalid automation", nil)
		return
	}
	preview, err := s.deps.PreviewAutomation(r.Context(), in)
	if err != nil {
		writeAutomationErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (s *Server) handleRunAutomation(w http.ResponseWriter, r *http.Request) {
	if s.deps.RunAutomation == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Automations are not available.", nil)
		return
	}
	run, err := s.deps.RunAutomation(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeAutomationErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) handlePauseAutomation(w http.ResponseWriter, r *http.Request) {
	s.handlePauseOrResume(w, r, s.deps.PauseAutomation)
}

func (s *Server) handleResumeAutomation(w http.ResponseWriter, r *http.Request) {
	s.handlePauseOrResume(w, r, s.deps.ResumeAutomation)
}

func (s *Server) handlePauseOrResume(w http.ResponseWriter, r *http.Request, fn func(ctx context.Context, id string) (automations.Automation, error)) {
	if fn == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Automations are not available.", nil)
		return
	}
	updated, err := fn(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeAutomationErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func writeAutomationErr(w http.ResponseWriter, err error) {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "not found"):
		writeErr(w, http.StatusNotFound, "NOT_FOUND", msg, nil)
	case strings.Contains(msg, "already has a run"):
		writeErr(w, http.StatusConflict, "CONFLICT", msg, nil)
	default:
		writeErr(w, http.StatusBadRequest, "AUTOMATION_INVALID", msg, nil)
	}
}
