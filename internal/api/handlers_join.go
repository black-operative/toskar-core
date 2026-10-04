package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/yeixio/toskar-core/internal/join"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Network is the one-line join (#40): join tokens on a computer in the
// network, and joining or leaving on another.
type Network interface {
	CreateJoinToken(ctx context.Context, ttl time.Duration) (JoinTokenCreated, error)
	ListJoinTokens(ctx context.Context) ([]join.Token, error)
	RevokeJoinToken(ctx context.Context, id string) (join.Token, error)
	JoinNetwork(ctx context.Context, req JoinRequest) (JoinResult, error)
	NetworkStatus(ctx context.Context) (NetworkStatus, error)
	LeaveNetwork(ctx context.Context) (LeaveResult, error)
}

// tokenRecord embeds a token's record in JoinTokenCreated beside the
// token itself.
type tokenRecord = join.Token

// JoinTokenCreated is a new token, shown once, and the command to use it.
type JoinTokenCreated struct {
	tokenRecord
	Token       string `json:"token"`
	Server      string `json:"server"`
	Fingerprint string `json:"fingerprint"`
	Command     string `json:"command"`
	// InstallCommand installs Yggdrasil on a Linux or macOS computer that
	// doesn't have it yet, then joins; WindowsCommand does so in PowerShell.
	InstallCommand string `json:"install_command"`
	WindowsCommand string `json:"windows_command"`
}

// JoinRequest joins this computer to a network.
type JoinRequest struct {
	Server      string `json:"server"`
	Token       string `json:"token"`
	Fingerprint string `json:"fingerprint"`
	// Name renames this computer as it joins; empty keeps its name.
	Name string `json:"name,omitempty"`
}

// NetworkNode is a computer in a network.
type NetworkNode struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Address     string `json:"address,omitempty"`
	Status      string `json:"status,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

// JoinResult is a finished join.
type JoinResult struct {
	// Status is joined, or already_joined when this computer already
	// trusted the server.
	Status    string      `json:"status"`
	NetworkID string      `json:"network_id"`
	Server    NetworkNode `json:"server"`
	// Node is this computer, named as the server knows it.
	Node NetworkNode `json:"node"`
}

// NetworkStatus is this computer's network membership.
type NetworkStatus struct {
	NetworkID string `json:"network_id"`
	// Reachable is whether other computers can reach this one's Bifrost.
	Reachable bool          `json:"reachable"`
	Node      NetworkNode   `json:"node"`
	Peers     []NetworkNode `json:"peers"`
}

// LeaveResult says which computers were told this one left.
type LeaveResult struct {
	Left        []string `json:"left"`
	Unreachable []string `json:"unreachable"`
}

// BindNetwork attaches the join routes.
func (s *Server) BindNetwork(n Network) { s.network = n }

func (s *Server) networkRoutes(api *mux.Router) {
	api.HandleFunc("/join-tokens", s.networkHandler(s.handleCreateJoinToken)).Methods(http.MethodPost)
	api.HandleFunc("/join-tokens", s.networkHandler(s.handleListJoinTokens)).Methods(http.MethodGet)
	api.HandleFunc("/join-tokens/{id}", s.networkHandler(s.handleRevokeJoinToken)).Methods(http.MethodDelete)
	api.HandleFunc("/network", s.networkHandler(s.handleNetworkStatus)).Methods(http.MethodGet)
	api.HandleFunc("/network/join", s.networkHandler(s.handleJoinNetwork)).Methods(http.MethodPost)
	api.HandleFunc("/network/leave", s.networkHandler(s.handleLeaveNetwork)).Methods(http.MethodPost)
}

func (s *Server) networkHandler(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.network == nil {
			writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Joining computers is not available.", nil)
			return
		}
		h(w, r)
	}
}

// joinStatus is the HTTP status for a join error code.
func joinStatus(code string) int {
	switch code {
	case "JOIN_BAD_REQUEST":
		return http.StatusBadRequest
	case "JOIN_TOKEN_INVALID", "JOIN_WRONG_SERVER":
		return http.StatusUnauthorized
	case "JOIN_TOKEN_EXPIRED", "JOIN_TOKEN_USED", "JOIN_TOKEN_REVOKED":
		return http.StatusGone
	case "JOIN_OTHER_NETWORK", "JOIN_NOT_REACHABLE":
		return http.StatusConflict
	case "JOIN_RATE_LIMITED":
		return http.StatusTooManyRequests
	case "JOIN_UNREACHABLE":
		return http.StatusBadGateway
	}
	return http.StatusInternalServerError
}

func writeJoinErr(w http.ResponseWriter, fallback string, err error) {
	code := fallback
	if c, _ := contracts.ErrorCode(err); c != "" {
		code = c
	}
	writeErrFrom(w, joinStatus(code), fallback, err)
}

func (s *Server) handleCreateJoinToken(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TTLMinutes int `json:"ttl_minutes"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, "JOIN_BAD_REQUEST", "The body must be {\"ttl_minutes\": n}.", nil)
			return
		}
	}
	created, err := s.network.CreateJoinToken(r.Context(), time.Duration(body.TTLMinutes)*time.Minute)
	if err != nil {
		writeJoinErr(w, "JOIN_TOKEN_FAILED", err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleListJoinTokens(w http.ResponseWriter, r *http.Request) {
	list, err := s.network.ListJoinTokens(r.Context())
	if err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "JOIN_TOKEN_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleRevokeJoinToken(w http.ResponseWriter, r *http.Request) {
	t, err := s.network.RevokeJoinToken(r.Context(), mux.Vars(r)["id"])
	switch {
	case errors.Is(err, join.ErrNotFound):
		writeErr(w, http.StatusNotFound, "JOIN_TOKEN_NOT_FOUND", err.Error(), nil)
	case errors.Is(err, join.ErrUsed), errors.Is(err, join.ErrRevoked), errors.Is(err, join.ErrExpired):
		writeErr(w, http.StatusConflict, "JOIN_TOKEN_NOT_ACTIVE", err.Error(), nil)
	case err != nil:
		writeErrFrom(w, http.StatusInternalServerError, "JOIN_TOKEN_FAILED", err)
	default:
		writeJSON(w, http.StatusOK, t)
	}
}

func (s *Server) handleNetworkStatus(w http.ResponseWriter, r *http.Request) {
	st, err := s.network.NetworkStatus(r.Context())
	if err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "JOIN_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleJoinNetwork(w http.ResponseWriter, r *http.Request) {
	var req JoinRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "JOIN_BAD_REQUEST", "The body must be {\"server\", \"token\", \"fingerprint\"}.", nil)
		return
	}
	res, err := s.network.JoinNetwork(r.Context(), req)
	if err != nil {
		writeJoinErr(w, "JOIN_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleLeaveNetwork(w http.ResponseWriter, r *http.Request) {
	res, err := s.network.LeaveNetwork(r.Context())
	if err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "JOIN_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
