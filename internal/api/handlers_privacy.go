package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/yeixio/toskar-core/internal/egress"
	"github.com/yeixio/toskar-core/internal/retention"
)

// Privacy is what left this computer and how long run records are kept
// (spec §63).
type Privacy interface {
	EgressRecords(ctx context.Context, f egress.Filter) ([]egress.Record, error)
	PrivacyOverview(ctx context.Context) (PrivacyOverview, error)
	SetRunRetention(ctx context.Context, days int) error
	DeleteRunRecords(ctx context.Context) (retention.Counts, error)
}

// PrivacyOverview is the privacy summary for Settings.
type PrivacyOverview struct {
	// RetentionDays is how long run records are kept; 0 keeps them.
	RetentionDays int `json:"retention_days"`
	// Last30Days counts what left this computer in the last 30 days, by kind.
	Last30Days map[string]int `json:"last_30_days"`
}

// BindPrivacy attaches the privacy routes.
func (s *Server) BindPrivacy(p Privacy) { s.privacy = p }

func (s *Server) privacyRoutes(api *mux.Router) {
	api.HandleFunc("/egress", s.privacyHandler(s.handleListEgress)).Methods(http.MethodGet)
	api.HandleFunc("/privacy", s.privacyHandler(s.handleGetPrivacy)).Methods(http.MethodGet)
	api.HandleFunc("/privacy", s.privacyHandler(s.handlePutPrivacy)).Methods(http.MethodPut)
	api.HandleFunc("/privacy/delete-runs", s.privacyHandler(s.handleDeleteRuns)).Methods(http.MethodPost)
}

func (s *Server) privacyHandler(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.privacy == nil {
			writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Privacy records are not available.", nil)
			return
		}
		h(w, r)
	}
}

// handleListEgress lists what left this computer, newest first.
// ?conversation_id= narrows to one chat; ?limit= caps the list.
func (s *Server) handleListEgress(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	list, err := s.privacy.EgressRecords(r.Context(), egress.Filter{ConversationID: r.URL.Query().Get("conversation_id"), Limit: limit})
	if err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "EGRESS_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleGetPrivacy(w http.ResponseWriter, r *http.Request) {
	o, err := s.privacy.PrivacyOverview(r.Context())
	if err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "PRIVACY_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, o)
}

// handlePutPrivacy sets how long run records are kept: {"retention_days": n}.
func (s *Server) handlePutPrivacy(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RetentionDays *int `json:"retention_days"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.RetentionDays == nil {
		writeErr(w, http.StatusBadRequest, "INVALID_BODY", `Send {"retention_days": n}.`, nil)
		return
	}
	if err := s.privacy.SetRunRetention(r.Context(), *in.RetentionDays); err != nil {
		writeErrFrom(w, http.StatusBadRequest, "INVALID_RETENTION", err)
		return
	}
	s.handleGetPrivacy(w, r)
}

// handleDeleteRuns deletes run records now and says how many.
func (s *Server) handleDeleteRuns(w http.ResponseWriter, r *http.Request) {
	c, err := s.privacy.DeleteRunRecords(r.Context())
	if err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "DELETE_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}
