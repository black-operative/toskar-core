package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/yeixio/toskar-core/internal/runlog"
)

// RunStore reads run traces (spec §35).
type RunStore interface {
	Get(ctx context.Context, id string) (runlog.Run, error)
	List(ctx context.Context, conversationID string, limit int) ([]runlog.Run, error)
}

// BindRuns attaches the run trace routes.
func (s *Server) BindRuns(r RunStore) { s.runs = r }

func (s *Server) runRoutes(api *mux.Router) {
	api.HandleFunc("/runs", s.handleListRuns).Methods(http.MethodGet)
	api.HandleFunc("/runs/{id}", s.handleGetRun).Methods(http.MethodGet)
}

// handleListRuns lists recent runs, newest first. ?conversation_id= narrows
// to one chat.
func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	if s.runs == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Run traces are not available.", nil)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	list, err := s.runs.List(r.Context(), r.URL.Query().Get("conversation_id"), limit)
	if err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "RUNS_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	if s.runs == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Run traces are not available.", nil)
		return
	}
	run, err := s.runs.Get(r.Context(), mux.Vars(r)["id"])
	if errors.Is(err, runlog.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "RUN_NOT_FOUND", "That run is not recorded, or its record was removed.", nil)
		return
	}
	if err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "RUNS_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}
