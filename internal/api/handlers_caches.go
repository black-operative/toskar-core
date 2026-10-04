package api

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/yeixio/toskar-core/internal/cache"
)

// CacheSource lists and clears caches (spec §36).
type CacheSource interface {
	CacheList() []cache.Info
	ClearCache(name string) bool
}

// BindCaches attaches the cache routes.
func (s *Server) BindCaches(c CacheSource) { s.caches = c }

func (s *Server) cacheRoutes(api *mux.Router) {
	api.HandleFunc("/caches", s.handleListCaches).Methods(http.MethodGet)
	api.HandleFunc("/caches/{name}/clear", s.handleClearCache).Methods(http.MethodPost)
}

// handleListCaches lists every cache with its policy (key, TTL,
// invalidation, scope, privacy) and counts.
func (s *Server) handleListCaches(w http.ResponseWriter, r *http.Request) {
	if s.caches == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Caches are not available.", nil)
		return
	}
	writeJSON(w, http.StatusOK, s.caches.CacheList())
}

func (s *Server) handleClearCache(w http.ResponseWriter, r *http.Request) {
	if s.caches == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Caches are not available.", nil)
		return
	}
	if !s.caches.ClearCache(mux.Vars(r)["name"]) {
		writeErr(w, http.StatusNotFound, "CACHE_NOT_FOUND", "No cache has that name.", nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
