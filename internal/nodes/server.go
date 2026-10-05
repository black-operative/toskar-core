package nodes

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/internal/version"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// InternalServer serves /internal/v1/* node RPC.
type InternalServer struct {
	cfg    config.Config
	logger *slog.Logger
	deps   InternalDeps
	http   *http.Server
	replay *auth.ReplayGuard
}

// InternalDeps wires local services into the cluster RPC.
type InternalDeps struct {
	Config   config.Config
	Logger   *slog.Logger
	Identity *auth.NodeIdentity
	Pairing  *auth.PairingManager
	Hardware func(ctx context.Context) (contracts.HardwareInventory, error)
	// Live is this computer's live CPU, memory, and GPU figures (#317).
	Live func(ctx context.Context) (contracts.LiveFigures, error)

	ListModels     func(ctx context.Context) ([]contracts.Model, error)
	InstallModel   func(ctx context.Context, id string, wait bool) error
	InstallFromURL func(ctx context.Context, req contracts.InstallFromURLRequest, wait bool) (string, error)
	DeleteModel    func(ctx context.Context, id string) error
	ListRunning    func(ctx context.Context) ([]contracts.RunningModelView, error)
	StartModel     func(ctx context.Context, modelID string) (contracts.RunningModelView, error)
	StopModel      func(ctx context.Context, instanceID string) error
	Chat           func(ctx context.Context, req RemoteChatRequest) (<-chan pluginapi.ChatChunk, error)

	// The pairing routes answer before trust exists. Each checks the
	// other computer's signature and limits attempts by source address.
	ReceiveOffer    func(offer auth.PairingOffer) (*auth.PairingSession, error)
	CompletePairing func(ctx context.Context, source string, complete auth.PairingComplete) (*auth.PairingSession, error)
	LookupOutbound  func(source, code string) (auth.PairingOffer, error)

	// Training serves /internal/v1/training/ for paired computers that send
	// training runs here.
	Training http.Handler
	// Tools serves /internal/v1/tools/ for paired computers that run tools
	// here (Gungnir §38).
	Tools http.Handler

	// JoinHello and Join answer the one-line join handshake (#40); they
	// check the join token themselves.
	JoinHello http.HandlerFunc
	Join      http.HandlerFunc
	// Leave hears a paired computer leaving the network.
	Leave func(ctx context.Context, nodeID string) error
	// TrainingActive reports whether this computer is training an AI,
	// which its health answer tells paired computers.
	TrainingActive func() bool
}

// Health is a computer's Bifrost health answer.
type Health struct {
	contracts.HealthResponse
	Training bool `json:"training,omitempty"`
}

func NewInternalServer(deps InternalDeps) *InternalServer {
	s := &InternalServer{
		cfg:    deps.Config,
		logger: deps.Logger,
		deps:   deps,
		replay: auth.NewReplayGuard(),
	}
	r := mux.NewRouter()
	api := r.PathPrefix("/internal/v1").Subrouter()
	api.Use(s.authMiddleware)

	api.HandleFunc("/health", s.handleHealth).Methods(http.MethodGet)
	api.HandleFunc("/node", s.handleNode).Methods(http.MethodGet)
	api.HandleFunc("/hardware", s.handleHardware).Methods(http.MethodGet)
	api.HandleFunc("/live", s.handleLive).Methods(http.MethodGet)
	api.HandleFunc("/models", s.handleListModels).Methods(http.MethodGet)
	api.HandleFunc("/models/install", s.handleInstallModel).Methods(http.MethodPost)
	api.HandleFunc("/models/install-from-url", s.handleInstallFromURL).Methods(http.MethodPost)
	api.HandleFunc("/models/{id}", s.handleDeleteModel).Methods(http.MethodDelete)
	api.HandleFunc("/models/running", s.handleListRunning).Methods(http.MethodGet)
	api.HandleFunc("/models/{id}/start", s.handleStartModel).Methods(http.MethodPost)
	api.HandleFunc("/models/instances/{id}/stop", s.handleStopModel).Methods(http.MethodPost)
	api.HandleFunc("/chat", s.handleChat).Methods(http.MethodPost)
	api.HandleFunc("/pairing/offer", s.handlePairingOffer).Methods(http.MethodPost)
	api.HandleFunc("/pairing/complete", s.handlePairingComplete).Methods(http.MethodPost)
	api.HandleFunc("/pairing/outbound/{code}", s.handlePairingOutbound).Methods(http.MethodGet)
	if deps.JoinHello != nil && deps.Join != nil {
		api.HandleFunc("/join/hello", deps.JoinHello).Methods(http.MethodPost)
		api.HandleFunc("/join", deps.Join).Methods(http.MethodPost)
	}
	if deps.Leave != nil {
		api.HandleFunc("/join/leave", s.handleLeave).Methods(http.MethodPost)
	}
	if deps.Training != nil {
		api.PathPrefix("/training/").Handler(http.StripPrefix("/internal/v1", deps.Training))
	}
	if deps.Tools != nil {
		api.PathPrefix("/tools/").Handler(http.StripPrefix("/internal/v1", deps.Tools))
	}

	s.http = &http.Server{
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s
}

func (s *InternalServer) ListenAndServe(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.logger.Info("internal node api listening", "addr", ln.Addr().String())
	return s.http.Serve(ln)
}

func (s *InternalServer) Shutdown(ctx context.Context) error {
	if s.http == nil {
		return nil
	}
	return s.http.Shutdown(ctx)
}

func (s *InternalServer) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if isPublicInternalPath(path) {
			next.ServeHTTP(w, r)
			return
		}
		if s.deps.Pairing == nil {
			http.Error(w, "auth unavailable", http.StatusServiceUnavailable)
			return
		}
		authz := r.Header.Get("Authorization")
		if !strings.HasPrefix(authz, "Bearer ") {
			http.Error(w, "authorization required", http.StatusUnauthorized)
			return
		}
		token := strings.TrimPrefix(authz, "Bearer ")
		// Peek node id from token without verify to load cert.
		nodeID, cert, err := s.lookupTokenPeer(r.Context(), token)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		claims, err := auth.ParseAndVerifyAuthToken(token, cert, s.localNodeID())
		if err != nil {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		if !s.replay.Use(claims) {
			http.Error(w, "token already used", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), peerKey{}, nodeID)))
	})
}

// isPublicInternalPath reports the routes that answer before trust exists.
// Paths match exactly, so a model named "node" is not one of them.
func isPublicInternalPath(path string) bool {
	switch path {
	case "/internal/v1/health",
		"/internal/v1/node",
		"/internal/v1/pairing/offer",
		"/internal/v1/pairing/complete",
		"/internal/v1/join/hello",
		"/internal/v1/join":
		return true
	}
	rest, ok := strings.CutPrefix(path, "/internal/v1/pairing/outbound/")
	return ok && rest != "" && !strings.Contains(rest, "/")
}

// localNodeID is this computer's node ID, the audience of tokens sent here.
func (s *InternalServer) localNodeID() string {
	if s.deps.Identity != nil {
		return s.deps.Identity.NodeID
	}
	return s.cfg.NodeID
}

// sourceAddr is the caller's IP address, for limiting pairing attempts.
func sourceAddr(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// peerKey carries the authenticated paired computer's node ID.
type peerKey struct{}

// PeerFrom is the paired computer a request came from, or "".
func PeerFrom(ctx context.Context) string {
	id, _ := ctx.Value(peerKey{}).(string)
	return id
}

// handleLeave forgets a paired computer that says it is leaving.
func (s *InternalServer) handleLeave(w http.ResponseWriter, r *http.Request) {
	peer := PeerFrom(r.Context())
	if peer == "" {
		http.Error(w, "authorization required", http.StatusUnauthorized)
		return
	}
	if err := s.deps.Leave(r.Context(), peer); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *InternalServer) lookupTokenPeer(ctx context.Context, token string) (string, []byte, error) {
	claimed, parseErr := auth.PeekAuthTokenNodeID(token)
	if parseErr != nil {
		return "", nil, parseErr
	}
	cert, err := s.deps.Pairing.TrustedCertPEM(ctx, claimed)
	if err != nil {
		return "", nil, err
	}
	return claimed, cert, nil
}

func (s *InternalServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	h := Health{HealthResponse: contracts.HealthResponse{Status: "ok", Product: "Yggdrasil", Version: version.Version}}
	if s.deps.TrainingActive != nil {
		h.Training = s.deps.TrainingActive()
	}
	writeJSON(w, h)
}

func (s *InternalServer) handleNode(w http.ResponseWriter, r *http.Request) {
	cert := ""
	if s.deps.Identity != nil {
		cert = string(s.deps.Identity.CertPEM)
	}
	writeJSON(w, map[string]any{
		"id":       s.cfg.NodeID,
		"name":     s.cfg.NodeName,
		"cert_pem": cert,
	})
}

func (s *InternalServer) handleLive(w http.ResponseWriter, r *http.Request) {
	if s.deps.Live == nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	live, err := s.deps.Live(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, live)
}

func (s *InternalServer) handleHardware(w http.ResponseWriter, r *http.Request) {
	if s.deps.Hardware == nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	inv, err := s.deps.Hardware(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, inv)
}

func (s *InternalServer) handleListModels(w http.ResponseWriter, r *http.Request) {
	if s.deps.ListModels == nil {
		writeJSON(w, []contracts.Model{})
		return
	}
	items, err := s.deps.ListModels(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, items)
}

func (s *InternalServer) handleInstallModel(w http.ResponseWriter, r *http.Request) {
	if s.deps.InstallModel == nil {
		http.Error(w, "unavailable", http.StatusNotImplemented)
		return
	}
	var body struct {
		ModelID string `json:"model_id"`
		Wait    bool   `json:"wait"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ModelID == "" {
		http.Error(w, "model_id required", http.StatusBadRequest)
		return
	}
	if err := s.deps.InstallModel(r.Context(), body.ModelID, body.Wait); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "model_id": body.ModelID})
}

func (s *InternalServer) handleInstallFromURL(w http.ResponseWriter, r *http.Request) {
	if s.deps.InstallFromURL == nil {
		http.Error(w, "unavailable", http.StatusNotImplemented)
		return
	}
	var req contracts.InstallFromURLRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	id, err := s.deps.InstallFromURL(r.Context(), req, false)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "model_id": id})
}

func (s *InternalServer) handleDeleteModel(w http.ResponseWriter, r *http.Request) {
	if s.deps.DeleteModel == nil {
		http.Error(w, "unavailable", http.StatusNotImplemented)
		return
	}
	id := mux.Vars(r)["id"]
	if id == "" {
		http.Error(w, "model id required", http.StatusBadRequest)
		return
	}
	if err := s.deps.DeleteModel(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *InternalServer) handleListRunning(w http.ResponseWriter, r *http.Request) {
	if s.deps.ListRunning == nil {
		writeJSON(w, []contracts.RunningModelView{})
		return
	}
	items, err := s.deps.ListRunning(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, items)
}

func (s *InternalServer) handleStartModel(w http.ResponseWriter, r *http.Request) {
	if s.deps.StartModel == nil {
		http.Error(w, "unavailable", http.StatusNotImplemented)
		return
	}
	id := mux.Vars(r)["id"]
	view, err := s.deps.StartModel(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, view)
}

func (s *InternalServer) handleStopModel(w http.ResponseWriter, r *http.Request) {
	if s.deps.StopModel == nil {
		http.Error(w, "unavailable", http.StatusNotImplemented)
		return
	}
	id := mux.Vars(r)["id"]
	if err := s.deps.StopModel(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *InternalServer) handleChat(w http.ResponseWriter, r *http.Request) {
	if s.deps.Chat == nil {
		http.Error(w, "unavailable", http.StatusNotImplemented)
		return
	}
	var req RemoteChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if req.ModelID == "" {
		http.Error(w, "model_id required", http.StatusBadRequest)
		return
	}
	ch, err := s.deps.Chat(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	enc := json.NewEncoder(w)
	for chunk := range ch {
		if err := enc.Encode(chunk); err != nil {
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
		if chunk.Done || chunk.Error != "" {
			return
		}
	}
}

func (s *InternalServer) handlePairingOffer(w http.ResponseWriter, r *http.Request) {
	if s.deps.ReceiveOffer == nil {
		http.Error(w, "unavailable", http.StatusNotImplemented)
		return
	}
	var offer auth.PairingOffer
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&offer); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	session, err := s.deps.ReceiveOffer(offer)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, session)
}

func (s *InternalServer) handlePairingComplete(w http.ResponseWriter, r *http.Request) {
	if s.deps.CompletePairing == nil {
		http.Error(w, "unavailable", http.StatusNotImplemented)
		return
	}
	var complete auth.PairingComplete
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&complete); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if _, err := s.deps.CompletePairing(r.Context(), sourceAddr(r), complete); err != nil {
		http.Error(w, err.Error(), pairingStatus(err))
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *InternalServer) handlePairingOutbound(w http.ResponseWriter, r *http.Request) {
	if s.deps.LookupOutbound == nil {
		http.Error(w, "unavailable", http.StatusNotImplemented)
		return
	}
	offer, err := s.deps.LookupOutbound(sourceAddr(r), mux.Vars(r)["code"])
	if err != nil {
		status := pairingStatus(err)
		if status == http.StatusBadRequest {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, offer)
}

func pairingStatus(err error) int {
	if errors.Is(err, auth.ErrPairingThrottled) {
		return http.StatusTooManyRequests
	}
	return http.StatusBadRequest
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
