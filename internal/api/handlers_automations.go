package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
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

// handleListAutomationRuns pages an automation's runs, newest first: before
// is the last run of the page already shown (#204).
func (s *Server) handleListAutomationRuns(w http.ResponseWriter, r *http.Request) {
	if s.deps.ListAutomationRuns == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Automations are not available.", nil)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	page, err := s.deps.ListAutomationRuns(r.Context(), mux.Vars(r)["id"], r.URL.Query().Get("before"), limit)
	if err != nil {
		writeAutomationErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
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

// handleParseAutomation reads a request into an automation to review and
// save: its name, task, schedule, notification, and notes on what was
// assumed (#204). language is the language the request may be written in
// besides English, and the language of the name and notes; empty uses the
// App language.
func (s *Server) handleParseAutomation(w http.ResponseWriter, r *http.Request) {
	if s.deps.ParseAutomation == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Automations are not available.", nil)
		return
	}
	var body struct {
		Text     string `json:"text"`
		TimeZone string `json:"time_zone"`
		Language string `json:"language"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "invalid body", nil)
		return
	}
	parsed, err := s.deps.ParseAutomation(r.Context(), body.Text, body.TimeZone, body.Language)
	if err != nil {
		writeErrFrom(w, http.StatusBadRequest, "AUTOMATION_INVALID", err)
		return
	}
	writeJSON(w, http.StatusOK, parsed)
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
	// The run has started and goes on without this request; its progress
	// and result arrive as automation events (#204).
	writeJSON(w, http.StatusAccepted, run)
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
	case errors.Is(err, automations.ErrRunning):
		writeErrFrom(w, http.StatusConflict, "AUTOMATION_RUNNING", err)
	case strings.Contains(msg, "not found"):
		writeErr(w, http.StatusNotFound, "NOT_FOUND", msg, nil)
	case strings.Contains(msg, "already has a run"):
		writeErr(w, http.StatusConflict, "CONFLICT", msg, nil)
	default:
		writeErr(w, http.StatusBadRequest, "AUTOMATION_INVALID", msg, nil)
	}
}
