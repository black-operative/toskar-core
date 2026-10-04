package api

import (
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/yeixio/toskar-core/internal/logs"
)

func (s *Server) handleListLogs(w http.ResponseWriter, r *http.Request) {
	if s.deps.ListLogs == nil {
		writeJSON(w, http.StatusOK, []logs.Entry{})
		return
	}
	items, err := s.deps.ListLogs(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "LOGS_LIST_FAILED",
			"Could not list log files.", map[string]any{"cause": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleGetLog(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	if s.deps.GetLog == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Log viewing is not available.", nil)
		return
	}
	tail := int64(256 * 1024)
	if v := r.URL.Query().Get("tail_bytes"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			tail = n
		}
	}
	content, err := s.deps.GetLog(r.Context(), name, tail)
	if err != nil {
		writeErr(w, http.StatusNotFound, "LOG_NOT_FOUND",
			"That log file could not be opened.", map[string]any{"cause": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, content)
}
