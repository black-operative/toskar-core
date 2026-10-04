package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/yeixio/toskar-core/internal/connectors"
)

// BindConnectors attaches the connected services routes (spec §32).
func (s *Server) BindConnectors(m *connectors.Manager) { s.connectors = m }

func (s *Server) connectorRoutes(api *mux.Router) {
	api.HandleFunc("/connectors", s.connectorHandler(s.handleListConnectors)).Methods(http.MethodGet)
	api.HandleFunc("/connectors/{id}", s.connectorHandler(s.handleConnect)).Methods(http.MethodPut)
	api.HandleFunc("/connectors/{id}", s.connectorHandler(s.handleDisconnect)).Methods(http.MethodDelete)
	api.HandleFunc("/connectors/{id}/check", s.connectorHandler(s.handleCheckConnector)).Methods(http.MethodPost)
}

func (s *Server) connectorHandler(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.connectors == nil {
			writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Connected services are not available.", nil)
			return
		}
		h(w, r)
	}
}

func writeConnectorErr(w http.ResponseWriter, err error) {
	if errors.Is(err, connectors.ErrUnknown) {
		writeErrFrom(w, http.StatusNotFound, "CONNECTOR_NOT_FOUND", err)
		return
	}
	writeErrFrom(w, http.StatusBadRequest, "CONNECTOR_FAILED", err)
}

// handleListConnectors lists every service and whether it is connected.
// Secret values are never returned; a stored token shows its last four
// characters only.
func (s *Server) handleListConnectors(w http.ResponseWriter, r *http.Request) {
	list, err := s.connectors.List(r.Context())
	if err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "CONNECTORS_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleConnect checks and stores a service's values: {"values": {...}}.
func (s *Server) handleConnect(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Values map[string]string `json:"values"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_BODY", "Send {\"values\": {...}}.", nil)
		return
	}
	st, err := s.connectors.Connect(r.Context(), mux.Vars(r)["id"], body.Values)
	if err != nil {
		writeConnectorErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleDisconnect(w http.ResponseWriter, r *http.Request) {
	if err := s.connectors.Disconnect(r.Context(), mux.Vars(r)["id"]); err != nil {
		writeConnectorErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCheckConnector(w http.ResponseWriter, r *http.Request) {
	st, err := s.connectors.Check(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeConnectorErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}
