package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/yeixio/toskar-core/internal/mimir"
)

// KnowledgeService is Mimir as the API sees it.
type KnowledgeService interface {
	List(ctx context.Context) ([]mimir.Source, error)
	Create(ctx context.Context, in mimir.CreateInput) (mimir.Source, error)
	Get(ctx context.Context, id string) (mimir.Source, error)
	Update(ctx context.Context, id string, in mimir.UpdateInput) (mimir.Source, error)
	Delete(ctx context.Context, id string) error
	Refresh(ctx context.Context, id string) error
	Search(ctx context.Context, in mimir.SearchInput) ([]mimir.Hit, error)
	Content(ctx context.Context, id string) (string, error)
}

// BindKnowledge attaches the connected-knowledge routes.
func (s *Server) BindKnowledge(k KnowledgeService) { s.knowledge = k }

func (s *Server) knowledgeRoutes(api *mux.Router) {
	api.HandleFunc("/knowledge/sources", s.handleListKnowledge).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/knowledge/sources", s.handleCreateKnowledge).Methods(http.MethodPost)
	api.HandleFunc("/knowledge/search", s.handleSearchKnowledge).Methods(http.MethodPost, http.MethodOptions)
	api.HandleFunc("/knowledge/sources/{id}/content", s.handleKnowledgeContent).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/knowledge/sources/{id}/refresh", s.handleRefreshKnowledge).Methods(http.MethodPost)
	api.HandleFunc("/knowledge/sources/{id}", s.handleGetKnowledge).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/knowledge/sources/{id}", s.handleUpdateKnowledge).Methods(http.MethodPatch)
	api.HandleFunc("/knowledge/sources/{id}", s.handleDeleteKnowledge).Methods(http.MethodDelete)
}

func (s *Server) knowledgeReady(w http.ResponseWriter) bool {
	if s.knowledge == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Connected knowledge is not available.", nil)
		return false
	}
	return true
}

func writeKnowledgeErr(w http.ResponseWriter, err error) {
	if errors.Is(err, mimir.ErrNotFound) {
		writeErrFrom(w, http.StatusNotFound, "NOT_FOUND", err)
		return
	}
	writeErrFrom(w, http.StatusBadRequest, "BAD_REQUEST", err)
}

func (s *Server) handleListKnowledge(w http.ResponseWriter, r *http.Request) {
	if !s.knowledgeReady(w) {
		return
	}
	items, err := s.knowledge.List(r.Context())
	if err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "INTERNAL_ERROR", err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleCreateKnowledge(w http.ResponseWriter, r *http.Request) {
	if !s.knowledgeReady(w) {
		return
	}
	var in mimir.CreateInput
	r.Body = http.MaxBytesReader(w, r.Body, mimir.MaxTextBytes+1<<20)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "BAD_REQUEST", "invalid knowledge source", nil)
		return
	}
	src, err := s.knowledge.Create(r.Context(), in)
	if err != nil {
		writeKnowledgeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, src)
}

func (s *Server) handleGetKnowledge(w http.ResponseWriter, r *http.Request) {
	if !s.knowledgeReady(w) {
		return
	}
	src, err := s.knowledge.Get(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeKnowledgeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, src)
}

func (s *Server) handleUpdateKnowledge(w http.ResponseWriter, r *http.Request) {
	if !s.knowledgeReady(w) {
		return
	}
	var in mimir.UpdateInput
	r.Body = http.MaxBytesReader(w, r.Body, mimir.MaxTextBytes+1<<20)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "BAD_REQUEST", "invalid knowledge source", nil)
		return
	}
	src, err := s.knowledge.Update(r.Context(), mux.Vars(r)["id"], in)
	if err != nil {
		writeKnowledgeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, src)
}

func (s *Server) handleDeleteKnowledge(w http.ResponseWriter, r *http.Request) {
	if !s.knowledgeReady(w) {
		return
	}
	if err := s.knowledge.Delete(r.Context(), mux.Vars(r)["id"]); err != nil {
		writeKnowledgeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRefreshKnowledge(w http.ResponseWriter, r *http.Request) {
	if !s.knowledgeReady(w) {
		return
	}
	id := mux.Vars(r)["id"]
	if err := s.knowledge.Refresh(r.Context(), id); err != nil && errors.Is(err, mimir.ErrNotFound) {
		writeKnowledgeErr(w, err)
		return
	}
	// A failed rebuild is recorded on the source, so return it either way.
	src, err := s.knowledge.Get(r.Context(), id)
	if err != nil {
		writeKnowledgeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, src)
}

func (s *Server) handleKnowledgeContent(w http.ResponseWriter, r *http.Request) {
	if !s.knowledgeReady(w) {
		return
	}
	text, err := s.knowledge.Content(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeKnowledgeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"text": text})
}

func (s *Server) handleSearchKnowledge(w http.ResponseWriter, r *http.Request) {
	if !s.knowledgeReady(w) {
		return
	}
	var in mimir.SearchInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "BAD_REQUEST", "invalid search", nil)
		return
	}
	hits, err := s.knowledge.Search(r.Context(), in)
	if err != nil {
		writeKnowledgeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, hits)
}
