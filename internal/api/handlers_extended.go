package api

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/diagnostics"
	"github.com/yeixio/toskar-core/internal/huginn"
	"github.com/yeixio/toskar-core/internal/locale"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

func (s *Server) handleRecommendModels(w http.ResponseWriter, r *http.Request) {
	if s.deps.RecommendModels == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Recommendations not available.", nil)
		return
	}
	purpose := r.URL.Query().Get("purpose")
	rec, err := s.deps.RecommendModels(r.Context(), purpose)
	if err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "RECOMMEND_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (s *Server) handleModelsFit(w http.ResponseWriter, r *http.Request) {
	if s.deps.ModelsFit == nil {
		writeJSON(w, http.StatusOK, []contracts.ModelsFitResponse{})
		return
	}
	items, err := s.deps.ModelsFit(r.Context())
	if err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "FIT_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleBrowseModels(w http.ResponseWriter, r *http.Request) {
	if s.deps.BrowseModels == nil {
		writeJSON(w, http.StatusOK, []contracts.BrowseModel{})
		return
	}
	q := r.URL.Query().Get("q")
	limit := 24
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	items, err := s.deps.BrowseModels(r.Context(), q, limit)
	if err != nil {
		writeErrFrom(w, http.StatusBadGateway, "BROWSE_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleListRunningModels(w http.ResponseWriter, r *http.Request) {
	if s.deps.ListRunningModels == nil {
		writeJSON(w, http.StatusOK, []contracts.RunningModelView{})
		return
	}
	items, err := s.deps.ListRunningModels(r.Context())
	if err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "RUNNING_LIST_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleInstallFromURL(w http.ResponseWriter, r *http.Request) {
	if s.deps.InstallModelFromURL == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Install from URL not available.", nil)
		return
	}
	var req contracts.InstallFromURLRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "invalid body", nil)
		return
	}
	wait := r.URL.Query().Get("wait") == "true"
	id, err := s.deps.InstallModelFromURL(r.Context(), req, wait)
	if err != nil {
		writeErrFrom(w, http.StatusBadRequest, "INSTALL_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "started", "model_id": id})
}

func (s *Server) handleInstallModel(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if s.deps.InstallModel == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Model install not available.", nil)
		return
	}
	wait := r.URL.Query().Get("wait") == "true"
	nodeID := r.URL.Query().Get("node_id")
	if r.Body != nil && r.ContentLength != 0 {
		var body struct {
			NodeID string `json:"node_id"`
			Wait   *bool  `json:"wait"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			if body.NodeID != "" {
				nodeID = body.NodeID
			}
			if body.Wait != nil {
				wait = *body.Wait
			}
		}
	}
	if err := s.deps.InstallModel(r.Context(), id, wait, nodeID); err != nil {
		writeErrFrom(w, http.StatusBadRequest, "INSTALL_FAILED", err)
		return
	}
	status := "started"
	if wait {
		status = "installed"
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": status, "model_id": id})
}

func (s *Server) handleStartModel(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if s.deps.StartModel == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Model start not available.", nil)
		return
	}
	var body contracts.ModelStartRequest
	_ = json.NewDecoder(r.Body).Decode(&body)
	view, err := s.deps.StartModel(r.Context(), id, body.NodeID)
	if err != nil {
		writeErrFrom(w, http.StatusBadRequest, "START_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleStopModel(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if s.deps.StopModel == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Model stop not available.", nil)
		return
	}
	var body contracts.ModelStopRequest
	_ = json.NewDecoder(r.Body).Decode(&body)
	instanceID := body.InstanceID
	if instanceID == "" {
		instanceID = id
	}
	if err := s.deps.StopModel(r.Context(), instanceID, body.NodeID); err != nil {
		writeErrFrom(w, http.StatusBadRequest, "STOP_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleDeleteModel(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if s.deps.DeleteModel == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Model delete not available.", nil)
		return
	}
	nodeID := r.URL.Query().Get("node_id")
	if r.Body != nil && r.ContentLength != 0 {
		var body struct {
			NodeID string `json:"node_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil && body.NodeID != "" {
			nodeID = body.NodeID
		}
	}
	if err := s.deps.DeleteModel(r.Context(), id, nodeID); err != nil {
		writeErrFrom(w, http.StatusBadRequest, "DELETE_FAILED", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListRuntimes(w http.ResponseWriter, r *http.Request) {
	if s.deps.ListRuntimes == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	items, err := s.deps.ListRuntimes(r.Context())
	if err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "RUNTIMES_LIST_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleInstallRuntime(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if s.deps.InstallRuntime == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Runtime install not available.", nil)
		return
	}
	if err := s.deps.InstallRuntime(r.Context(), id); err != nil {
		writeErrFrom(w, http.StatusBadRequest, "INSTALL_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "installed", "runtime_id": id})
}

func (s *Server) handleCreateProfile(w http.ResponseWriter, r *http.Request) {
	if s.deps.CreateProfile == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Profiles not available.", nil)
		return
	}
	var p contracts.AIProfile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "invalid body", nil)
		return
	}
	out, err := s.deps.CreateProfile(r.Context(), p)
	if err != nil {
		writeErrFrom(w, http.StatusBadRequest, "PROFILE_CREATE_FAILED", err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) handleGetProfile(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if s.deps.GetProfile == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Profiles not available.", nil)
		return
	}
	p, err := s.deps.GetProfile(r.Context(), id)
	if err != nil {
		writeErrFrom(w, http.StatusNotFound, "NOT_FOUND", err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handlePatchProfile(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if s.deps.UpdateProfile == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Profiles not available.", nil)
		return
	}
	var p contracts.AIProfile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "invalid body", nil)
		return
	}
	p.ID = id
	out, err := s.deps.UpdateProfile(r.Context(), p)
	if err != nil {
		writeErrFrom(w, http.StatusBadRequest, "PROFILE_UPDATE_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDeleteProfile(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if s.deps.DeleteProfile == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Profiles not available.", nil)
		return
	}
	if err := s.deps.DeleteProfile(r.Context(), id); err != nil {
		writeErrFrom(w, http.StatusBadRequest, "DELETE_FAILED", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleResetProfile puts a built-in profile back to how Yggdrasil ships it
// (spec §24).
func (s *Server) handleResetProfile(w http.ResponseWriter, r *http.Request) {
	if s.deps.ResetProfile == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Profiles not available.", nil)
		return
	}
	p, err := s.deps.ResetProfile(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeErrFrom(w, http.StatusBadRequest, "RESET_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if s.deps.Chat == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Chat not available.", nil)
		return
	}
	var body struct {
		ConversationID string `json:"conversation_id"`
		ProfileID      string `json:"profile_id"`
		ModelID        string `json:"model_id"`
		Message        string `json:"message"`
		Stream         bool   `json:"stream"`
		Execution      string `json:"execution"`
		// Attachments are artifact ids uploaded with POST /artifacts.
		Attachments []string `json:"attachments"`
		// Effort is auto, fast, balanced, or thorough (spec §15).
		Effort string `json:"effort"`
		// TimeZone is the person's IANA time zone, from their browser.
		TimeZone string `json:"time_zone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "invalid body", nil)
		return
	}
	ctx := huginn.WithEffort(artifacts.WithAttachments(r.Context(), body.Attachments), huginn.ParseEffort(body.Effort))
	r = r.WithContext(locale.WithTimeZone(ctx, body.TimeZone))
	if err := s.deps.Chat(w, r, body.ConversationID, body.ProfileID, body.ModelID, body.Message, body.Stream, body.Execution); err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "CHAT_FAILED", err)
	}
}

// handleStopChat stops a conversation's running turn: every model call, tool
// call, and paired computer working on it (spec §67). What was already
// written is kept.
func (s *Server) handleStopChat(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ConversationID string `json:"conversation_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ConversationID == "" {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "conversation_id required", nil)
		return
	}
	stopped := false
	if s.deps.StopChat != nil {
		stopped = s.deps.StopChat(body.ConversationID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"stopped": stopped})
}

func (s *Server) handleConversationMessages(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if s.deps.ListMessages == nil {
		writeJSON(w, http.StatusOK, []contracts.Message{})
		return
	}
	msgs, err := s.deps.ListMessages(r.Context(), id)
	if err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "MESSAGES_LIST_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	if s.deps.ListTasks == nil {
		writeJSON(w, http.StatusOK, []contracts.Task{})
		return
	}
	items, err := s.deps.ListTasks(r.Context())
	if err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "TASKS_LIST_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	if s.deps.CreateTask == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Tasks not available.", nil)
		return
	}
	var body struct {
		ProfileID      string `json:"profile_id"`
		ConversationID string `json:"conversation_id"`
		Prompt         string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "invalid body", nil)
		return
	}
	task, err := s.deps.CreateTask(r.Context(), body.ProfileID, body.ConversationID, body.Prompt)
	if err != nil {
		writeErrFrom(w, http.StatusBadRequest, "TASK_CREATE_FAILED", err)
		return
	}
	if s.deps.RunTask != nil {
		_ = s.deps.RunTask(r.Context(), task.ID)
	}
	writeJSON(w, http.StatusCreated, task)
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if s.deps.GetTask == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Tasks not available.", nil)
		return
	}
	task, err := s.deps.GetTask(r.Context(), id)
	if err != nil {
		writeErrFrom(w, http.StatusNotFound, "NOT_FOUND", err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) handleToolDecide(w http.ResponseWriter, r *http.Request) {
	if s.deps.DecideTool == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Tool decisions not available.", nil)
		return
	}
	var body struct {
		RequestID    string `json:"request_id"`
		Allow        bool   `json:"allow"`
		AllowSession bool   `json:"allow_session"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "invalid body", nil)
		return
	}
	if err := s.deps.DecideTool(body.RequestID, body.Allow, body.AllowSession); err != nil {
		writeErrFrom(w, http.StatusBadRequest, "DECIDE_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handlePairNode(w http.ResponseWriter, r *http.Request) {
	if s.deps.StartPairing == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Pairing not available.", nil)
		return
	}
	var body struct {
		NodeID string `json:"node_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "invalid body", nil)
		return
	}
	session, err := s.deps.StartPairing(body.NodeID)
	if err != nil {
		writeErrFrom(w, http.StatusBadRequest, "PAIR_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, session)
}

func (s *Server) handleClaimPairing(w http.ResponseWriter, r *http.Request) {
	if s.deps.ClaimPairing == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Pairing claim not available.", nil)
		return
	}
	var body struct {
		NodeID string `json:"node_id"`
		Code   string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "invalid body", nil)
		return
	}
	session, err := s.deps.ClaimPairing(r.Context(), body.NodeID, body.Code)
	if err != nil {
		writeErrFrom(w, http.StatusBadRequest, "CLAIM_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, session)
}

func (s *Server) handleReceivePairingOffer(w http.ResponseWriter, r *http.Request) {
	if s.deps.ReceivePairingOffer == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Pairing offer not available.", nil)
		return
	}
	var offer auth.PairingOffer
	if err := json.NewDecoder(r.Body).Decode(&offer); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "invalid body", nil)
		return
	}
	session, err := s.deps.ReceivePairingOffer(offer)
	if err != nil {
		writeErrFrom(w, http.StatusBadRequest, "OFFER_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, session)
}

func (s *Server) handleOutboundPairing(w http.ResponseWriter, r *http.Request) {
	if s.deps.LookupOutboundPairing == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Outbound pairing lookup not available.", nil)
		return
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	offer, err := s.deps.LookupOutboundPairing(host, mux.Vars(r)["code"])
	if errors.Is(err, auth.ErrPairingThrottled) {
		writeErrFrom(w, http.StatusTooManyRequests, "PAIRING_RATE_LIMITED", err)
		return
	}
	if err != nil {
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "unknown or expired code", nil)
		return
	}
	writeJSON(w, http.StatusOK, offer)
}

func (s *Server) handleApprovePairing(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if s.deps.ApprovePairing == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Pairing not available.", nil)
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	session, err := s.deps.ApprovePairing(r.Context(), id, body.Code)
	if err != nil {
		writeErrFrom(w, http.StatusBadRequest, "APPROVE_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, session)
}

func (s *Server) handlePendingPairing(w http.ResponseWriter, r *http.Request) {
	if s.deps.ListPairingOffers == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, s.deps.ListPairingOffers())
}

func (s *Server) handleRevokeNode(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if s.deps.RevokeNode == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Revoke not available.", nil)
		return
	}
	if err := s.deps.RevokeNode(r.Context(), id); err != nil {
		writeErrFrom(w, http.StatusBadRequest, "REVOKE_FAILED", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	if s.deps.ListAPIKeys == nil {
		writeJSON(w, http.StatusOK, []auth.APIKeyRecord{})
		return
	}
	items, err := s.deps.ListAPIKeys(r.Context())
	if err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "API_KEYS_LIST_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	if s.deps.CreateAPIKey == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "API keys not available.", nil)
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	rec, secret, err := s.deps.CreateAPIKey(r.Context(), body.Name)
	if err != nil {
		writeErrFrom(w, http.StatusBadRequest, "CREATE_FAILED", err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"key": rec, "secret": secret})
}

func (s *Server) handleDeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if s.deps.RevokeAPIKey == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "API keys not available.", nil)
		return
	}
	if err := s.deps.RevokeAPIKey(r.Context(), id); err != nil {
		writeErrFrom(w, http.StatusBadRequest, "REVOKE_FAILED", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRotateAPIKey(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if s.deps.RotateAPIKey == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "API keys not available.", nil)
		return
	}
	rec, secret, err := s.deps.RotateAPIKey(r.Context(), id)
	if err != nil {
		writeErrFrom(w, http.StatusBadRequest, "ROTATE_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": rec, "secret": secret})
}

// handleSetAPIKeyPermissions changes what a key may ask of the assistant (§62).
func (s *Server) handleSetAPIKeyPermissions(w http.ResponseWriter, r *http.Request) {
	if s.deps.SetAPIKeyPermissions == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "API keys not available.", nil)
		return
	}
	var p auth.APIKeyPermissions
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_BODY", "Send the permissions as JSON.", nil)
		return
	}
	rec, err := s.deps.SetAPIKeyPermissions(r.Context(), mux.Vars(r)["id"], p)
	if err != nil {
		writeErrFrom(w, http.StatusBadRequest, "PERMISSIONS_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (s *Server) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	if s.deps.ExportDiagnostics == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Diagnostics export is not available.", nil)
		return
	}
	include := r.URL.Query().Get("include_conversations") == "true"
	if r.Method == http.MethodPost {
		var body struct {
			IncludeConversations bool `json:"include_conversations"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		include = include || body.IncludeConversations
	}
	path, err := s.deps.ExportDiagnostics(r.Context(), include)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "DIAGNOSTICS_FAILED",
			"Could not create a diagnostics bundle.", map[string]any{"cause": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path":    path,
		"message": "Diagnostics bundle created. Secrets and private keys were excluded.",
	})
}

// handleLiveFigures reports each computer's CPU, memory, and GPU figures
// now and over the last hour and day, for the Performance page (#317).
func (s *Server) handleLiveFigures(w http.ResponseWriter, r *http.Request) {
	if s.deps.LiveFigures == nil {
		writeJSON(w, http.StatusOK, []contracts.LiveFigures{})
		return
	}
	figures, err := s.deps.LiveFigures(r.Context())
	if err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "LIVE_FIGURES_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, figures)
}

// handleGPUSetup reports what this computer still needs for Toskar to use
// its GPU, and how to fix each piece, for Diagnostics (#317).
func (s *Server) handleGPUSetup(w http.ResponseWriter, r *http.Request) {
	if s.deps.GPUSetup == nil {
		writeJSON(w, http.StatusOK, contracts.GPUSetup{Problems: []contracts.GPUProblem{}})
		return
	}
	setup, err := s.deps.GPUSetup(r.Context())
	if err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "GPU_SETUP_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, setup)
}

// handleRuntimeHistory reports the daemon's memory and goroutines now and
// over the last day, for the Diagnostics page (#231).
func (s *Server) handleRuntimeHistory(w http.ResponseWriter, r *http.Request) {
	if s.deps.RuntimeHistory == nil {
		writeJSON(w, http.StatusOK, diagnostics.RuntimeHistory{Now: diagnostics.Sample()})
		return
	}
	writeJSON(w, http.StatusOK, s.deps.RuntimeHistory())
}
