package api

import (
	"context"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/yeixio/toskar-core/internal/inventory"
)

// CapabilitySource builds the capability inventory (spec §37).
type CapabilitySource interface {
	Capabilities(ctx context.Context) inventory.Snapshot
}

// BindCapabilities attaches the capability routes.
func (s *Server) BindCapabilities(c CapabilitySource) { s.capabilities = c }

func (s *Server) capabilityRoutes(api *mux.Router) {
	api.HandleFunc("/capabilities", s.handleCapabilities).Methods(http.MethodGet)
	api.HandleFunc("/capabilities/models/{id}", s.handleModelPlacement).Methods(http.MethodGet)
}

// handleCapabilities returns the inventory: models, computers, tools,
// connected services, providers, files, and the abilities they add up to.
// ?ask= returns only the abilities a question is about.
func (s *Server) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	if s.capabilities == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "The capability inventory is not available.", nil)
		return
	}
	snap := s.capabilities.Capabilities(r.Context())
	if q := r.URL.Query().Get("ask"); q != "" {
		writeJSON(w, http.StatusOK, map[string]any{"question": q, "abilities": inventory.Ask(snap, q)})
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

// handleModelPlacement says which computers can run a model.
func (s *Server) handleModelPlacement(w http.ResponseWriter, r *http.Request) {
	if s.capabilities == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "The capability inventory is not available.", nil)
		return
	}
	places, ok := inventory.NodesFor(s.capabilities.Capabilities(r.Context()), mux.Vars(r)["id"])
	if !ok {
		writeErr(w, http.StatusNotFound, "MODEL_NOT_INSTALLED", "That model is not installed on any computer.", map[string]any{"model_id": mux.Vars(r)["id"]})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"model_id": mux.Vars(r)["id"], "computers": places})
}
