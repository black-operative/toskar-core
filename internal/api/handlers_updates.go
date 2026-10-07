package api

import (
	"context"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/yeixio/toskar-core/internal/updates"
)

// Updates says whether a newer Toskar is out (yeixio/toskar-apps#90).
type Updates interface {
	Status(ctx context.Context) updates.Status
}

// BindUpdates attaches the update route.
func (s *Server) BindUpdates(u Updates) { s.updates = u }

func (s *Server) updatesRoutes(api *mux.Router) {
	api.HandleFunc("/updates", s.handleGetUpdates).Methods(http.MethodGet)
}

// handleGetUpdates is the result of the last update check. A build that
// doesn't check (App Store, desktop app, development) says enabled: false.
func (s *Server) handleGetUpdates(w http.ResponseWriter, r *http.Request) {
	if s.updates == nil {
		writeJSON(w, http.StatusOK, updates.Status{})
		return
	}
	writeJSON(w, http.StatusOK, s.updates.Status(r.Context()))
}
