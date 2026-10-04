package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/yeixio/toskar-core/internal/personal"
)

// PersonalStore reads and saves how the person likes answers (spec §38).
type PersonalStore interface {
	PersonalStyle(ctx context.Context) (personal.Style, error)
	SetPersonalStyle(ctx context.Context, s personal.Style) (personal.Style, error)
}

// BindPersonal attaches the personalization routes.
func (s *Server) BindPersonal(p PersonalStore) { s.personal = p }

func (s *Server) personalRoutes(api *mux.Router) {
	api.HandleFunc("/personalization", s.handleGetPersonal).Methods(http.MethodGet)
	api.HandleFunc("/personalization", s.handlePutPersonal).Methods(http.MethodPut)
}

func (s *Server) handleGetPersonal(w http.ResponseWriter, r *http.Request) {
	if s.personal == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Personalization is not available.", nil)
		return
	}
	style, err := s.personal.PersonalStyle(r.Context())
	if err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "PERSONALIZATION_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, style)
}

// handlePutPersonal replaces the style. One that tries to grant a
// permission is refused (400).
func (s *Server) handlePutPersonal(w http.ResponseWriter, r *http.Request) {
	if s.personal == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Personalization is not available.", nil)
		return
	}
	var in personal.Style
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_BODY", "Send the style as JSON.", nil)
		return
	}
	saved, err := s.personal.SetPersonalStyle(r.Context(), in)
	if err != nil {
		writeErrFrom(w, http.StatusBadRequest, "INVALID_PERSONALIZATION", err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}
