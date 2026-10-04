package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/yeixio/toskar-core/internal/muninn"
)

// BindMemory attaches the persistent memory routes.
func (s *Server) BindMemory(m *muninn.Store) { s.memory = m }

func (s *Server) memoryRoutes(api *mux.Router) {
	api.HandleFunc("/memory", s.memoryHandler(s.handleListMemory)).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/memory", s.memoryHandler(s.handleAddMemory)).Methods(http.MethodPost)
	api.HandleFunc("/memory/{id}", s.memoryHandler(s.handlePatchMemory)).Methods(http.MethodPatch)
	api.HandleFunc("/memory/{id}", s.memoryHandler(s.handleDeleteMemory)).Methods(http.MethodDelete)
}

func (s *Server) memoryHandler(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.memory == nil {
			writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Memory is not available.", nil)
			return
		}
		h(w, r)
	}
}

func writeMemoryErr(w http.ResponseWriter, err error) {
	if errors.Is(err, muninn.ErrNotFound) {
		writeErrFrom(w, http.StatusNotFound, "NOT_FOUND", err)
		return
	}
	writeErrFrom(w, http.StatusBadRequest, "BAD_REQUEST", err)
}

func (s *Server) handleListMemory(w http.ResponseWriter, r *http.Request) {
	items, err := s.memory.List(r.Context())
	if err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "INTERNAL_ERROR", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"memories": items, "categories": muninn.Categories})
}

func (s *Server) handleAddMemory(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Content  string `json:"content"`
		Category string `json:"category"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "BAD_REQUEST", "invalid memory", nil)
		return
	}
	m, created, err := s.memory.Add(r.Context(), in.Content, in.Category, muninn.SourceManual, "")
	if err != nil {
		writeMemoryErr(w, err)
		return
	}
	status := http.StatusCreated
	if !created {
		status = http.StatusOK
	}
	writeJSON(w, status, m)
}

func (s *Server) handlePatchMemory(w http.ResponseWriter, r *http.Request) {
	var p muninn.Patch
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeErr(w, http.StatusBadRequest, "BAD_REQUEST", "invalid memory", nil)
		return
	}
	m, err := s.memory.Update(r.Context(), mux.Vars(r)["id"], p)
	if err != nil {
		writeMemoryErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) handleDeleteMemory(w http.ResponseWriter, r *http.Request) {
	if err := s.memory.Delete(r.Context(), mux.Vars(r)["id"]); err != nil {
		writeMemoryErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
