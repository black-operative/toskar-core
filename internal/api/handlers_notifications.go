package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/yeixio/toskar-core/internal/gjallarhorn"
)

// BindNotifications attaches the notification center routes (Gjallarhorn).
func (s *Server) BindNotifications(hub *gjallarhorn.Hub) { s.notifications = hub }

func (s *Server) notificationRoutes(api *mux.Router) {
	// Destinations and quiet hours come before /notifications/{id}.
	api.HandleFunc("/notifications/destinations", s.notificationHandler(s.handleListDestinations)).Methods(http.MethodGet)
	api.HandleFunc("/notifications/destinations", s.notificationHandler(s.handleCreateDestination)).Methods(http.MethodPost)
	api.HandleFunc("/notifications/destinations/{id}", s.notificationHandler(s.handleGetDestination)).Methods(http.MethodGet)
	api.HandleFunc("/notifications/destinations/{id}", s.notificationHandler(s.handleUpdateDestination)).Methods(http.MethodPatch)
	api.HandleFunc("/notifications/destinations/{id}", s.notificationHandler(s.handleDeleteDestination)).Methods(http.MethodDelete)
	api.HandleFunc("/notifications/destinations/{id}/test", s.notificationHandler(s.handleTestDestination)).Methods(http.MethodPost)
	api.HandleFunc("/notifications/destinations/{id}/rotate-secret", s.notificationHandler(s.handleRotateSecret)).Methods(http.MethodPost)
	api.HandleFunc("/notifications/quiet-hours", s.notificationHandler(s.handleGetQuietHours)).Methods(http.MethodGet)
	api.HandleFunc("/notifications/quiet-hours", s.notificationHandler(s.handlePutQuietHours)).Methods(http.MethodPut)
	api.HandleFunc("/notifications", s.notificationHandler(s.handleListNotifications)).Methods(http.MethodGet)
	api.HandleFunc("/notifications/{id}", s.notificationHandler(s.handleGetNotification)).Methods(http.MethodGet)
	api.HandleFunc("/notifications/read", s.notificationHandler(s.handleReadNotifications)).Methods(http.MethodPost)
	api.HandleFunc("/notifications/{id}/dismiss", s.notificationHandler(s.handleDismissNotification)).Methods(http.MethodPost)
}

func (s *Server) notificationHandler(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.notifications == nil {
			writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Notifications are not available.", nil)
			return
		}
		h(w, r)
	}
}

// handleListNotifications returns recent notifications, newest first, and
// the unread count. ?unread=1 lists unread ones only.
func (s *Server) handleListNotifications(w http.ResponseWriter, r *http.Request) {
	list, unread, err := s.notifications.List(r.Context(), r.URL.Query().Get("unread") == "1", 50, r.URL.Query().Get("category"))
	if err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "NOTIFICATIONS_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"notifications": list, "unread": unread})
}

// handleReadNotifications marks the given ids read, or all with an empty list.
func (s *Server) handleReadNotifications(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, "INVALID_JSON", "invalid body", nil)
			return
		}
	}
	if err := s.notifications.MarkRead(r.Context(), body.IDs); err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "NOTIFICATIONS_FAILED", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDismissNotification(w http.ResponseWriter, r *http.Request) {
	err := s.notifications.Dismiss(r.Context(), mux.Vars(r)["id"])
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "NOTIFICATION_NOT_FOUND", "notification not found", nil)
		return
	}
	if err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "NOTIFICATIONS_FAILED", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleGetNotification returns one notification with each channel's delivery.
func (s *Server) handleGetNotification(w http.ResponseWriter, r *http.Request) {
	n, err := s.notifications.Get(r.Context(), mux.Vars(r)["id"])
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "NOTIFICATION_NOT_FOUND", "notification not found", nil)
		return
	}
	if err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "NOTIFICATIONS_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, n)
}

func writeDestinationErr(w http.ResponseWriter, err error) {
	if errors.Is(err, gjallarhorn.ErrNotFound) {
		writeErrFrom(w, http.StatusNotFound, "NOT_FOUND", err)
		return
	}
	writeErrFrom(w, http.StatusBadRequest, "BAD_REQUEST", err)
}

// handleListDestinations lists email and webhook destinations. Passwords and
// signing secrets are never returned.
func (s *Server) handleListDestinations(w http.ResponseWriter, r *http.Request) {
	list, err := s.notifications.Destinations(r.Context())
	if err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "NOTIFICATIONS_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleCreateDestination adds a destination. A webhook's signing secret is
// in the response this once.
func (s *Server) handleCreateDestination(w http.ResponseWriter, r *http.Request) {
	var in gjallarhorn.DestinationInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "invalid body", nil)
		return
	}
	d, secret, err := s.notifications.CreateDestination(r.Context(), in)
	if err != nil {
		writeDestinationErr(w, err)
		return
	}
	out := map[string]any{"destination": d}
	if secret != "" {
		out["secret"] = secret
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) handleGetDestination(w http.ResponseWriter, r *http.Request) {
	d, err := s.notifications.Destination(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeDestinationErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleUpdateDestination(w http.ResponseWriter, r *http.Request) {
	var in gjallarhorn.DestinationInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "invalid body", nil)
		return
	}
	d, err := s.notifications.UpdateDestination(r.Context(), mux.Vars(r)["id"], in)
	if err != nil {
		writeDestinationErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleDeleteDestination(w http.ResponseWriter, r *http.Request) {
	if err := s.notifications.DeleteDestination(r.Context(), mux.Vars(r)["id"]); err != nil {
		writeDestinationErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleTestDestination sends a test notification now. A failed send is a
// 200 with ok false and the reason, so the app can show it.
func (s *Server) handleTestDestination(w http.ResponseWriter, r *http.Request) {
	err := s.notifications.TestDestination(r.Context(), mux.Vars(r)["id"])
	if errors.Is(err, gjallarhorn.ErrNotFound) {
		writeDestinationErr(w, err)
		return
	}
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error(), "permanent": gjallarhorn.IsPermanent(err)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleRotateSecret(w http.ResponseWriter, r *http.Request) {
	secret, err := s.notifications.RotateSecret(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeDestinationErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"secret": secret})
}

func (s *Server) handleGetQuietHours(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.notifications.QuietHours(r.Context()))
}

func (s *Server) handlePutQuietHours(w http.ResponseWriter, r *http.Request) {
	var q gjallarhorn.QuietHours
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "invalid body", nil)
		return
	}
	saved, err := s.notifications.SetQuietHours(r.Context(), q)
	if err != nil {
		writeErrFrom(w, http.StatusBadRequest, "BAD_REQUEST", err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}
