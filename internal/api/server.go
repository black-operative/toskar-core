package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/yeixio/toskar-core/internal/api/openai"
	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/internal/connectors"
	"github.com/yeixio/toskar-core/internal/diagnostics"
	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/gjallarhorn"
	"github.com/yeixio/toskar-core/internal/imagegen"
	"github.com/yeixio/toskar-core/internal/logs"
	"github.com/yeixio/toskar-core/internal/mcp"
	"github.com/yeixio/toskar-core/internal/models"
	"github.com/yeixio/toskar-core/internal/muninn"
	"github.com/yeixio/toskar-core/internal/runtimes"
	"github.com/yeixio/toskar-core/internal/speech"
	"github.com/yeixio/toskar-core/internal/training"
	"github.com/yeixio/toskar-core/internal/version"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Dependencies wires handlers to application services.
type Dependencies struct {
	Config   *config.Manager
	Bus      *events.Bus
	Logger   *slog.Logger
	OpenAI   *openai.Handler
	Hardware func(ctx context.Context) (contracts.HardwareInventory, error)

	ListModels          func(ctx context.Context) ([]contracts.Model, error)
	RecommendModels     func(ctx context.Context, purpose string) (contracts.Recommendation, error)
	ModelsFit           func(ctx context.Context) ([]contracts.ModelsFitResponse, error)
	BrowseModels        func(ctx context.Context, query string, limit int) ([]contracts.BrowseModel, error)
	InstallModel        func(ctx context.Context, id string, wait bool, nodeID string) error
	InstallModelFromURL func(ctx context.Context, req contracts.InstallFromURLRequest, wait bool) (string, error)
	DeleteModel         func(ctx context.Context, id string, nodeID string) error
	ListRunningModels   func(ctx context.Context) ([]contracts.RunningModelView, error)
	// Acceleration sums up where this computer's loaded models run, for
	// health (#317).
	Acceleration          func(ctx context.Context) string
	StartModel            func(ctx context.Context, id string, nodeID string) (contracts.RunningModelView, error)
	StopModel             func(ctx context.Context, instanceID string, nodeID string) error
	ListRuntimes          func(ctx context.Context) ([]runtimes.RuntimeInfo, error)
	InstallRuntime        func(ctx context.Context, id string) error
	ListProfiles          func(ctx context.Context) ([]contracts.AIProfile, error)
	CreateProfile         func(ctx context.Context, p contracts.AIProfile) (contracts.AIProfile, error)
	GetProfile            func(ctx context.Context, id string) (contracts.AIProfile, error)
	UpdateProfile         func(ctx context.Context, p contracts.AIProfile) (contracts.AIProfile, error)
	DeleteProfile         func(ctx context.Context, id string) error
	ResetProfile          func(ctx context.Context, id string) (contracts.AIProfile, error)
	ListNodes             func(ctx context.Context) ([]contracts.Node, error)
	RefreshDiscovery      func(ctx context.Context) error
	StartPairing          func(nodeID string) (*auth.PairingSession, error)
	ClaimPairing          func(ctx context.Context, nodeID, code string) (*auth.PairingSession, error)
	ApprovePairing        func(ctx context.Context, sessionID, code string) (*auth.PairingSession, error)
	ListPairingOffers     func() []auth.PairingSession
	ReceivePairingOffer   func(offer auth.PairingOffer) (*auth.PairingSession, error)
	LookupOutboundPairing func(source, code string) (auth.PairingOffer, error)
	RevokeNode            func(ctx context.Context, nodeID string) error
	ListConversations     func(ctx context.Context) ([]contracts.Conversation, error)
	CreateConversation    func(ctx context.Context, title, profileID, modelID string) (contracts.Conversation, error)
	UpdateConversation    func(ctx context.Context, id string, title, profileID, modelID *string, memoryOff *bool) (contracts.Conversation, error)
	DeleteConversation    func(ctx context.Context, id string) error
	ListMessages          func(ctx context.Context, conversationID string) ([]contracts.Message, error)
	Chat                  func(w http.ResponseWriter, r *http.Request, conversationID, profileID, modelID, message string, stream bool, execution string) error
	// StopChat stops a conversation's running turn and reports whether one was running.
	StopChat           func(conversationID string) bool
	ListTasks          func(ctx context.Context) ([]contracts.Task, error)
	CreateTask         func(ctx context.Context, profileID, conversationID, prompt string) (contracts.Task, error)
	GetTask            func(ctx context.Context, id string) (contracts.Task, error)
	RunTask            func(ctx context.Context, id string) error
	ListAutomations    func(ctx context.Context) ([]automations.Automation, error)
	CreateAutomation   func(ctx context.Context, in automations.CreateInput) (automations.Automation, error)
	GetAutomation      func(ctx context.Context, id string) (automations.Detail, error)
	ListAutomationRuns func(ctx context.Context, id, before string, limit int) (automations.RunsPage, error)
	UpdateAutomation   func(ctx context.Context, id string, patch automations.Patch) (automations.Automation, error)
	DeleteAutomation   func(ctx context.Context, id string) error
	RunAutomation      func(ctx context.Context, id string) (automations.Run, error)
	// ContinueAutomationRun opens a run's result in a chat and returns the
	// chat (#204).
	ContinueAutomationRun func(ctx context.Context, id, runID string) (string, error)
	// MakeHookLink makes a webhook trigger's new token, and RunHook starts
	// the automation a token belongs to (#204).
	MakeHookLink      func(ctx context.Context, id string) (string, error)
	RunHook           func(ctx context.Context, token string, body []byte) (automations.Run, error)
	PreviewAutomation func(ctx context.Context, in automations.CreateInput) (automations.Preview, error)
	// ParseAutomation reads a request such as "every morning at 8, tell me
	// if the price is below $500" into an automation (#204).
	ParseAutomation      func(ctx context.Context, text, timeZone, language string) (automations.ParsedRequest, error)
	PauseAutomation      func(ctx context.Context, id string) (automations.Automation, error)
	ResumeAutomation     func(ctx context.Context, id string) (automations.Automation, error)
	DecideTool           func(requestID string, allow, allowSession bool) error
	ListTools            func(ctx context.Context) (any, error)
	DescribeTool         func(ctx context.Context, id string) (any, error)
	ListToolRuns         func(ctx context.Context, toolID, conversationID string, limit int) (any, error)
	SetToolEnabled       func(ctx context.Context, id string, enabled bool) error
	TestTool             func(ctx context.Context, id string, args map[string]any) (map[string]any, error)
	ToolActivity         func() any
	ToolProviders        func(ctx context.Context) any
	ListAPIKeys          func(ctx context.Context) ([]auth.APIKeyRecord, error)
	CreateAPIKey         func(ctx context.Context, name string) (auth.APIKeyRecord, string, error)
	RevokeAPIKey         func(ctx context.Context, id string) error
	RotateAPIKey         func(ctx context.Context, id string) (auth.APIKeyRecord, string, error)
	SetAPIKeyPermissions func(ctx context.Context, id string, p auth.APIKeyPermissions) (auth.APIKeyRecord, error)
	VerifyAPIKey         func(ctx context.Context, secret string) (auth.APIKeyRecord, error)
	// Devices makes the codes a phone connects with (#216).
	Devices *auth.DevicePairer
	// PhoneAddress is where a phone reaches this computer, and whether it
	// can yet: false while the API answers only on this computer.
	PhoneAddress func() (address string, reachable bool)
	// EnableLANForPhone turns on local network access so a phone can connect.
	EnableLANForPhone      func(ctx context.Context) error
	GetSettings            func(ctx context.Context) (contracts.SettingsView, error)
	UpdateSettings         func(ctx context.Context, patch map[string]any) (contracts.SettingsView, error)
	ResetApp               func(ctx context.Context, deleteModels bool) (contracts.SettingsView, error)
	ListPerformance        func(ctx context.Context, sort, order string, limit int) ([]contracts.GenerationRun, error)
	ListBenchmarkWorkloads func() []contracts.BenchmarkWorkload
	StartBenchmark         func(ctx context.Context, req contracts.BenchmarkRequest) (*contracts.BenchmarkJob, error)
	GetBenchmark           func(ctx context.Context, id string) (*contracts.BenchmarkJob, error)
	ListBenchmarks         func(ctx context.Context) ([]contracts.BenchmarkJob, error)
	CancelBenchmark        func(ctx context.Context, id string) error
	// ExportDiagnostics writes a zip bundle and returns its absolute path.
	ExportDiagnostics func(ctx context.Context, includeConversations bool) (string, error)
	// RuntimeHistory is the daemon's memory and goroutines over the last day.
	RuntimeHistory func() diagnostics.RuntimeHistory
	// LiveFigures are this computer's and each paired computer's live CPU,
	// memory, and GPU figures (#317).
	LiveFigures func(ctx context.Context) ([]contracts.LiveFigures, error)
	// GPUSetup is what stands between this computer's GPU and Toskar using
	// it, with fixes (#317).
	GPUSetup func(ctx context.Context) (contracts.GPUSetup, error)
	ListLogs func(ctx context.Context) ([]logs.Entry, error)
	GetLog   func(ctx context.Context, name string, tailBytes int64) (logs.Content, error)
	Version  func() contracts.VersionResponse
	WebRoot  fs.FS
}

// Server is the control-plane HTTP server.
type Server struct {
	deps   Dependencies
	router *mux.Router
	http   *http.Server

	// listen is the API's socket, replaced by Rebind; conns are the open
	// connections, so turning network access off can close other devices'.
	listenMu sync.Mutex
	listen   net.Listener
	replaced map[net.Listener]bool
	conns    map[net.Conn]bool
	closed   chan struct{}

	knowledge       KnowledgeService
	memory          *muninn.Store
	artifacts       *artifacts.Store
	speech          *speech.Engine
	speechStore     *artifacts.Store
	images          *imagegen.Setup
	video           *imagegen.Setup
	notifications   *gjallarhorn.Hub
	connectors      *connectors.Manager
	mcp             *mcp.Manager
	mcpServer       *mcp.Server
	ctl             func() string
	personal        PersonalStore
	privacy         Privacy
	updates         Updates
	ratings         Ratings
	network         Network
	external        ExternalServers
	runs            RunStore
	capabilities    CapabilitySource
	caches          CacheSource
	training        *training.Service
	trainingCatalog func() []models.CatalogEntry
}

// NewServer builds the API server.
func NewServer(deps Dependencies) *Server {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	s := &Server{deps: deps, router: mux.NewRouter()}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.router.Use(s.recoverMiddleware)
	s.router.Use(s.corsMiddleware)
	s.router.Use(s.contractMiddleware)
	s.router.Use(s.logMiddleware)

	s.router.HandleFunc("/about", s.handleSourceOffer).Methods(http.MethodGet, http.MethodOptions)
	s.router.HandleFunc("/source", s.handleSourceOffer).Methods(http.MethodGet, http.MethodOptions)
	// A webhook's token is its proof, so it's outside the API's keys (#204).
	s.router.HandleFunc("/hooks/{token}", s.handleHook).Methods(http.MethodPost)

	api := s.router.PathPrefix("/api/v1").Subrouter()
	api.Use(s.controlAuthMiddleware)
	api.HandleFunc("/health", s.handleHealth).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/version", s.handleVersion).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/hardware", s.handleHardware).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/models", s.handleModels).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/models/recommend", s.handleRecommendModels).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/models/fit", s.handleModelsFit).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/models/browse", s.handleBrowseModels).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/models/running", s.handleListRunningModels).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/models/install-from-url", s.handleInstallFromURL).Methods(http.MethodPost)
	api.HandleFunc("/models/{id}/install", s.handleInstallModel).Methods(http.MethodPost)
	api.HandleFunc("/models/{id}/start", s.handleStartModel).Methods(http.MethodPost)
	api.HandleFunc("/models/{id}/stop", s.handleStopModel).Methods(http.MethodPost)
	api.HandleFunc("/models/{id}", s.handleDeleteModel).Methods(http.MethodDelete)
	api.HandleFunc("/runtimes", s.handleListRuntimes).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/runtimes/{id}/install", s.handleInstallRuntime).Methods(http.MethodPost)
	api.HandleFunc("/profiles", s.handleProfiles).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/profiles", s.handleCreateProfile).Methods(http.MethodPost)
	api.HandleFunc("/profiles/{id}", s.handleGetProfile).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/profiles/{id}", s.handlePatchProfile).Methods(http.MethodPatch)
	api.HandleFunc("/profiles/{id}", s.handleDeleteProfile).Methods(http.MethodDelete)
	api.HandleFunc("/profiles/{id}/reset", s.handleResetProfile).Methods(http.MethodPost)
	api.HandleFunc("/chat", s.handleChat).Methods(http.MethodPost)
	api.HandleFunc("/chat/stop", s.handleStopChat).Methods(http.MethodPost)
	api.HandleFunc("/conversations/{id}/messages", s.handleConversationMessages).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/tasks", s.handleListTasks).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/tasks", s.handleCreateTask).Methods(http.MethodPost)
	api.HandleFunc("/tasks/{id}", s.handleGetTask).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/automations", s.handleListAutomations).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/automations", s.handleCreateAutomation).Methods(http.MethodPost)
	api.HandleFunc("/automations/preview", s.handlePreviewAutomation).Methods(http.MethodPost)
	api.HandleFunc("/automations/parse", s.handleParseAutomation).Methods(http.MethodPost)
	api.HandleFunc("/automations/{id}/run", s.handleRunAutomation).Methods(http.MethodPost)
	api.HandleFunc("/automations/{id}/pause", s.handlePauseAutomation).Methods(http.MethodPost)
	api.HandleFunc("/automations/{id}/resume", s.handleResumeAutomation).Methods(http.MethodPost)
	api.HandleFunc("/automations/{id}", s.handleGetAutomation).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/automations/{id}/runs", s.handleListAutomationRuns).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/automations/{id}/runs/{run_id}/chat", s.handleContinueAutomationRun).Methods(http.MethodPost)
	api.HandleFunc("/automations/{id}/hook", s.handleMakeHookLink).Methods(http.MethodPost)
	api.HandleFunc("/automations/{id}", s.handleUpdateAutomation).Methods(http.MethodPatch)
	api.HandleFunc("/automations/{id}", s.handleDeleteAutomation).Methods(http.MethodDelete)
	api.HandleFunc("/tools", s.handleListTools).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/tools/runs", s.handleToolRuns).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/tools/activity", s.handleToolActivity).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/tools/providers", s.handleToolProviders).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/tools/decide", s.handleToolDecide).Methods(http.MethodPost)
	api.HandleFunc("/tools/{id}/enabled", s.handleSetToolEnabled).Methods(http.MethodPost, http.MethodOptions)
	api.HandleFunc("/tools/{id}/test", s.handleTestTool).Methods(http.MethodPost, http.MethodOptions)
	api.HandleFunc("/tools/{id}", s.handleDescribeTool).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/nodes", s.handleNodes).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/nodes/refresh", s.handleRefreshNodes).Methods(http.MethodPost)
	api.HandleFunc("/nodes/pair", s.handlePairNode).Methods(http.MethodPost)
	api.HandleFunc("/nodes/pair/claim", s.handleClaimPairing).Methods(http.MethodPost)
	api.HandleFunc("/nodes/pairing/pending", s.handlePendingPairing).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/nodes/pairing/offer", s.handleReceivePairingOffer).Methods(http.MethodPost)
	api.HandleFunc("/nodes/pairing/outbound/{code}", s.handleOutboundPairing).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/nodes/{id}/revoke", s.handleRevokeNode).Methods(http.MethodPost)
	api.HandleFunc("/nodes/{id}/pair/approve", s.handleApprovePairing).Methods(http.MethodPost)
	api.HandleFunc("/api-keys", s.handleListAPIKeys).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/api-keys", s.handleCreateAPIKey).Methods(http.MethodPost)
	api.HandleFunc("/api-keys/{id}", s.handleDeleteAPIKey).Methods(http.MethodDelete)
	api.HandleFunc("/api-keys/{id}/rotate", s.handleRotateAPIKey).Methods(http.MethodPost)
	api.HandleFunc("/api-keys/{id}/permissions", s.handleSetAPIKeyPermissions).Methods(http.MethodPut)
	api.HandleFunc("/settings", s.handleGetSettings).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/settings", s.handlePatchSettings).Methods(http.MethodPatch, http.MethodPut)
	api.HandleFunc("/settings/reset", s.handleResetApp).Methods(http.MethodPost, http.MethodOptions)
	api.HandleFunc("/performance", s.handleListPerformance).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/performance/live", s.handleLiveFigures).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/benchmarks/workloads", s.handleListBenchmarkWorkloads).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/benchmarks", s.handleListBenchmarks).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/benchmarks", s.handleStartBenchmark).Methods(http.MethodPost)
	api.HandleFunc("/benchmarks/{id}", s.handleGetBenchmark).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/benchmarks/{id}/cancel", s.handleCancelBenchmark).Methods(http.MethodPost)
	api.HandleFunc("/diagnostics", s.handleDiagnostics).Methods(http.MethodGet, http.MethodPost, http.MethodOptions)
	api.HandleFunc("/diagnostics/runtime", s.handleRuntimeHistory).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/diagnostics/gpu", s.handleGPUSetup).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/logs", s.handleListLogs).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/logs/{name}", s.handleGetLog).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/conversations", s.handleListConversations).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/conversations", s.handleCreateConversation).Methods(http.MethodPost)
	api.HandleFunc("/conversations/{id}", s.handleUpdateConversation).Methods(http.MethodPatch)
	api.HandleFunc("/conversations/{id}", s.handleDeleteConversation).Methods(http.MethodDelete)
	api.HandleFunc("/events", s.handleSSE).Methods(http.MethodGet, http.MethodOptions)
	s.knowledgeRoutes(api)
	s.memoryRoutes(api)
	s.artifactRoutes(api)
	s.deviceRoutes(api)
	s.notificationRoutes(api)
	s.connectorRoutes(api)
	s.mcpRoutes(api)
	s.personalRoutes(api)
	s.privacyRoutes(api)
	s.updatesRoutes(api)
	s.ratingsRoutes(api)
	s.networkRoutes(api)
	s.externalRoutes(api)
	s.runRoutes(api)
	s.capabilityRoutes(api)
	s.cacheRoutes(api)
	s.trainingRoutes(api)
	s.speechRoutes(api)
	s.imageRoutes(api)

	s.mcpRootRoutes(s.router)

	if s.deps.OpenAI != nil {
		s.router.HandleFunc("/v1/models", s.deps.OpenAI.HandleModels).Methods(http.MethodGet, http.MethodOptions)
		s.router.HandleFunc("/v1/chat/completions", s.deps.OpenAI.HandleChatCompletions).Methods(http.MethodPost, http.MethodOptions)
	} else {
		s.router.HandleFunc("/v1/models", s.handleOpenAIModels).Methods(http.MethodGet, http.MethodOptions)
		s.router.HandleFunc("/v1/chat/completions", s.handleOpenAIChat).Methods(http.MethodPost, http.MethodOptions)
	}

	if s.deps.WebRoot != nil {
		fileServer := http.FileServer(http.FS(s.deps.WebRoot))
		s.router.PathPrefix("/").Handler(spaFallback(s.deps.WebRoot, fileServer))
	}
}

// Handler returns the root HTTP handler.
func (s *Server) Handler() http.Handler {
	return s.router
}

// ListenAndServe starts the server on addr, and serves until Shutdown. A
// Rebind swaps the socket without returning.
func (s *Server) ListenAndServe(addr string) error {
	s.listenMu.Lock()
	s.http = &http.Server{
		Addr:              addr,
		Handler:           s.router,
		ReadHeaderTimeout: 10 * time.Second,
		ConnState:         s.trackConn,
	}
	s.replaced, s.conns, s.closed = map[net.Listener]bool{}, map[net.Conn]bool{}, make(chan struct{})
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		s.listenMu.Unlock()
		return err
	}
	s.listen = ln
	s.listenMu.Unlock()
	s.deps.Logger.Info("api listening", "addr", ln.Addr().String())
	return s.serve(ln)
}

// serve serves ln. When Rebind replaced it, it waits for Shutdown instead
// of returning, since the server goes on, on the new socket.
func (s *Server) serve(ln net.Listener) error {
	err := s.http.Serve(ln)
	s.listenMu.Lock()
	replaced := s.replaced[ln]
	delete(s.replaced, ln)
	closed := s.closed
	s.listenMu.Unlock()
	if replaced && !errors.Is(err, http.ErrServerClosed) {
		<-closed
		return http.ErrServerClosed
	}
	return err
}

// Rebind moves the API to addr without a restart (#216), such as when
// local network access is turned on or off. The old socket is closed first,
// since a wildcard and a loopback socket on one port can't both be open on
// every system; if addr can't be bound, the old address is bound again.
// Moving to a loopback address closes connections from other devices.
func (s *Server) Rebind(addr string) error {
	s.listenMu.Lock()
	defer s.listenMu.Unlock()
	if s.http == nil || s.listen == nil {
		return fmt.Errorf("the API isn't listening yet")
	}
	old := s.listen
	if old.Addr().String() == addr {
		return nil
	}
	oldAddr := old.Addr().String()
	s.replaced[old] = true
	_ = old.Close()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		back, backErr := net.Listen("tcp", oldAddr)
		if backErr != nil {
			return fmt.Errorf("listen on %s: %w; and %s again: %v", addr, err, oldAddr, backErr)
		}
		s.listen = back
		go func() { _ = s.serve(back) }()
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	s.listen = ln
	go func() { _ = s.serve(ln) }()
	if loopbackAddr(addr) {
		for c := range s.conns {
			if !loopbackAddr(c.RemoteAddr().String()) {
				_ = c.Close()
			}
		}
	}
	s.deps.Logger.Info("api listening", "addr", ln.Addr().String())
	return nil
}

// Addr is where the API listens now, or "" before it starts.
func (s *Server) Addr() string {
	s.listenMu.Lock()
	defer s.listenMu.Unlock()
	if s.listen == nil {
		return ""
	}
	return s.listen.Addr().String()
}

func (s *Server) trackConn(c net.Conn, state http.ConnState) {
	s.listenMu.Lock()
	defer s.listenMu.Unlock()
	switch state {
	case http.StateNew:
		s.conns[c] = true
	case http.StateClosed, http.StateHijacked:
		delete(s.conns, c)
	}
}

// loopbackAddr reports a host:port on loopback.
func loopbackAddr(hostport string) bool {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && addr.Unmap().IsLoopback()
}

// BindAutomations attaches the scheduler routes after the runner is constructed.
func (s *Server) BindAutomations(d Dependencies) {
	s.deps.ListAutomations = d.ListAutomations
	s.deps.CreateAutomation = d.CreateAutomation
	s.deps.GetAutomation = d.GetAutomation
	s.deps.ListAutomationRuns = d.ListAutomationRuns
	s.deps.UpdateAutomation = d.UpdateAutomation
	s.deps.DeleteAutomation = d.DeleteAutomation
	s.deps.RunAutomation = d.RunAutomation
	s.deps.ContinueAutomationRun = d.ContinueAutomationRun
	s.deps.MakeHookLink = d.MakeHookLink
	s.deps.RunHook = d.RunHook
	s.deps.PreviewAutomation = d.PreviewAutomation
	s.deps.ParseAutomation = d.ParseAutomation
	s.deps.PauseAutomation = d.PauseAutomation
	s.deps.ResumeAutomation = d.ResumeAutomation
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.listenMu.Lock()
	srv, closed := s.http, s.closed
	if closed != nil {
		select {
		case <-closed:
		default:
			close(closed)
		}
	}
	s.listenMu.Unlock()
	if srv == nil {
		return nil
	}
	return srv.Shutdown(ctx)
}

func (s *Server) controlAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.deps.Config == nil || auth.FromThisComputer(r) {
			next.ServeHTTP(w, r)
			return
		}
		if !config.ListensBeyondLoopback(s.deps.Config.Get().APIHost) {
			// Bound to loopback, so nothing needs a key, but a device's
			// connection from before network access was turned off is
			// refused; Rebind closes those too.
			if !loopbackAddr(r.RemoteAddr) {
				writeErr(w, http.StatusForbidden, "LAN_ACCESS_OFF", "local network access is off on this computer", nil)
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		route := routeTemplate(r)
		// A phone connecting has no key yet; the code is its proof, and the
		// handler answers only on the local network (#216).
		if r.Method == http.MethodPost && route == "/api/v1"+pairDeviceRoute {
			next.ServeHTTP(w, r)
			return
		}
		token, err := auth.BearerToken(r)
		if err != nil {
			if errors.Is(err, auth.ErrAPIKeyInURL) {
				writeErrFrom(w, http.StatusBadRequest, "API_KEY_IN_URL", err)
				return
			}
			writeErr(w, http.StatusUnauthorized, "UNAUTHORIZED", "authorization required", nil)
			return
		}
		if s.deps.VerifyAPIKey == nil {
			writeErr(w, http.StatusUnauthorized, "UNAUTHORIZED", "authorization required", nil)
			return
		}
		rec, err := s.deps.VerifyAPIKey(r.Context(), token)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid api key", nil)
			return
		}
		// A phone's key reaches only what the phone uses.
		if rec.Kind == auth.KindDevice && !auth.DeviceMayReach(r.Method, route) {
			writeErr(w, http.StatusForbidden, "DEVICE_NOT_ALLOWED", "a phone's key can't do this; use a key from API Access", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// routeTemplate is the matched route's path template, such as
// /api/v1/conversations/{id}, or the request path when none matched.
func routeTemplate(r *http.Request) string {
	if route := mux.CurrentRoute(r); route != nil {
		if tpl, err := route.GetPathTemplate(); err == nil {
			return tpl
		}
	}
	return r.URL.Path
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ver := "0.1.0-dev"
	if s.deps.Version != nil {
		ver = s.deps.Version().Version
	}
	resp := contracts.HealthResponse{
		Status:  "ok",
		Product: "Yggdrasil",
		Version: ver,
	}
	if s.deps.Acceleration != nil {
		resp.Acceleration = s.deps.Acceleration(r.Context())
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	offer := version.CurrentOffer()
	resp := contracts.VersionResponse{
		Version:  offer.Version,
		Commit:   offer.Commit,
		Product:  "Yggdrasil",
		License:  offer.License,
		Source:   offer.Source,
		Contract: contracts.CurrentContract(),
	}
	if s.deps.Version != nil {
		got := s.deps.Version()
		if got.Version != "" {
			resp.Version = got.Version
		}
		if got.Commit != "" {
			resp.Commit = got.Commit
		}
		if got.BuildDate != "" {
			resp.BuildDate = got.BuildDate
		}
		if got.Product != "" {
			resp.Product = got.Product
		}
		if got.License != "" {
			resp.License = got.License
		}
		if got.Source != "" {
			resp.Source = got.Source
		}
	} else {
		resp.BuildDate = version.BuildDate
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleSourceOffer(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, version.CurrentOffer())
}

func (s *Server) handleHardware(w http.ResponseWriter, r *http.Request) {
	if s.deps.Hardware == nil {
		writeErr(w, http.StatusServiceUnavailable, "HARDWARE_UNAVAILABLE", "Hardware detection is not available.", nil)
		return
	}
	inv, err := s.deps.Hardware(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "HARDWARE_DETECT_FAILED",
			"Could not fully detect hardware. Some details may be missing.",
			map[string]any{"cause": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, inv)
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	if s.deps.ListModels == nil {
		writeJSON(w, http.StatusOK, []contracts.Model{})
		return
	}
	models, err := s.deps.ListModels(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "MODELS_LIST_FAILED", "Could not list models.", map[string]any{"cause": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, models)
}

func (s *Server) handleProfiles(w http.ResponseWriter, r *http.Request) {
	if s.deps.ListProfiles == nil {
		writeJSON(w, http.StatusOK, []contracts.AIProfile{})
		return
	}
	items, err := s.deps.ListProfiles(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "PROFILES_LIST_FAILED", "Could not list profiles.", map[string]any{"cause": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleNodes(w http.ResponseWriter, r *http.Request) {
	if s.deps.ListNodes == nil {
		writeJSON(w, http.StatusOK, []contracts.Node{})
		return
	}
	items, err := s.deps.ListNodes(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "NODES_LIST_FAILED", "Could not list computers.", map[string]any{"cause": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleRefreshNodes(w http.ResponseWriter, r *http.Request) {
	if s.deps.RefreshDiscovery == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Discovery refresh not available.", nil)
		return
	}
	if err := s.deps.RefreshDiscovery(r.Context()); err != nil {
		writeErrFrom(w, http.StatusBadRequest, "REFRESH_FAILED", err)
		return
	}
	if s.deps.ListNodes == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	items, err := s.deps.ListNodes(r.Context())
	if err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "NODES_LIST_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	if s.deps.GetSettings != nil {
		view, err := s.deps.GetSettings(r.Context())
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "SETTINGS_READ_FAILED", "Could not read settings.", map[string]any{"cause": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, view)
		return
	}
	cfg := s.deps.Config.Get()
	writeJSON(w, http.StatusOK, contracts.SettingsView{
		DataDir:          cfg.DataDir,
		ModelsDir:        cfg.ModelsDir,
		RuntimesDir:      cfg.RuntimesDir,
		LogsDir:          cfg.LogsDir,
		APIHost:          cfg.APIHost,
		APIPort:          cfg.APIPort,
		LANAPIEnabled:    cfg.LANAPIEnabled,
		WebUIEnabled:     cfg.WebUIEnabled,
		DiscoveryEnabled: cfg.DiscoveryEnabled,
		NodeName:         cfg.NodeName,
		NodeID:           cfg.NodeID,
	})
}

func (s *Server) handlePatchSettings(w http.ResponseWriter, r *http.Request) {
	var patch map[string]any
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be JSON.", nil)
		return
	}
	if s.deps.UpdateSettings != nil {
		view, err := s.deps.UpdateSettings(r.Context(), patch)
		if err != nil {
			if errors.Is(err, auth.ErrAPIKeyRequired) {
				writeErr(w, http.StatusBadRequest, "API_KEY_REQUIRED", "Create an API key before allowing access from other computers.", nil)
				return
			}
			// A setting with an invalid value says which, with its own code.
			if code, _ := contracts.ErrorCode(err); code != "" {
				writeErrFrom(w, http.StatusBadRequest, code, err)
				return
			}
			writeErr(w, http.StatusInternalServerError, "SETTINGS_UPDATE_FAILED", "Could not update settings.", map[string]any{"cause": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, view)
		return
	}
	err := s.deps.Config.Update(func(c *config.Config) {
		if v, ok := patch["node_name"].(string); ok && v != "" {
			c.NodeName = v
		}
		if v, ok := patch["lan_api_enabled"].(bool); ok {
			c.LANAPIEnabled = v
			if v {
				c.APIHost = "0.0.0.0"
			} else {
				c.APIHost = config.DefaultBindLoopback
			}
		}
		if v, ok := patch["discovery_enabled"].(bool); ok {
			c.DiscoveryEnabled = v
		}
		if v, ok := patch["web_ui_enabled"].(bool); ok {
			c.WebUIEnabled = v
		}
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "SETTINGS_UPDATE_FAILED", "Could not update settings.", map[string]any{"cause": err.Error()})
		return
	}
	s.handleGetSettings(w, r)
}

func (s *Server) handleResetApp(w http.ResponseWriter, r *http.Request) {
	if s.deps.ResetApp == nil {
		writeErr(w, http.StatusNotImplemented, "RESET_UNSUPPORTED", "Application reset is not available.", nil)
		return
	}
	deleteModels := r.URL.Query().Get("delete_models") == "true"
	if r.Body != nil && r.ContentLength != 0 {
		var body struct {
			DeleteModels bool `json:"delete_models"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			deleteModels = body.DeleteModels
		}
	}
	view, err := s.deps.ResetApp(r.Context(), deleteModels)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "RESET_FAILED", "Could not reset the application.", map[string]any{"cause": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleListPerformance(w http.ResponseWriter, r *http.Request) {
	if s.deps.ListPerformance == nil {
		writeJSON(w, http.StatusOK, []contracts.GenerationRun{})
		return
	}
	q := r.URL.Query()
	limit := 200
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	items, err := s.deps.ListPerformance(r.Context(), q.Get("sort"), q.Get("order"), limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "PERFORMANCE_LIST_FAILED", "Could not list performance metrics.", map[string]any{"cause": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleListBenchmarkWorkloads(w http.ResponseWriter, r *http.Request) {
	if s.deps.ListBenchmarkWorkloads == nil {
		writeJSON(w, http.StatusOK, []contracts.BenchmarkWorkload{})
		return
	}
	writeJSON(w, http.StatusOK, s.deps.ListBenchmarkWorkloads())
}

func (s *Server) handleListBenchmarks(w http.ResponseWriter, r *http.Request) {
	if s.deps.ListBenchmarks == nil {
		writeJSON(w, http.StatusOK, []contracts.BenchmarkJob{})
		return
	}
	items, err := s.deps.ListBenchmarks(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "BENCHMARK_LIST_FAILED", "Could not list benchmarks.", map[string]any{"cause": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleStartBenchmark(w http.ResponseWriter, r *http.Request) {
	if s.deps.StartBenchmark == nil {
		writeErr(w, http.StatusNotImplemented, "BENCHMARK_UNSUPPORTED", "Benchmarks are not available.", nil)
		return
	}
	var req contracts.BenchmarkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be JSON.", nil)
		return
	}
	job, err := s.deps.StartBenchmark(r.Context(), req)
	if err != nil {
		writeErrFrom(w, http.StatusBadRequest, "BENCHMARK_START_FAILED", err)
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func (s *Server) handleGetBenchmark(w http.ResponseWriter, r *http.Request) {
	if s.deps.GetBenchmark == nil {
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "Benchmark not found.", nil)
		return
	}
	id := mux.Vars(r)["id"]
	job, err := s.deps.GetBenchmark(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "Benchmark not found.", map[string]any{"cause": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleCancelBenchmark(w http.ResponseWriter, r *http.Request) {
	if s.deps.CancelBenchmark == nil {
		writeErr(w, http.StatusNotImplemented, "BENCHMARK_UNSUPPORTED", "Benchmarks are not available.", nil)
		return
	}
	id := mux.Vars(r)["id"]
	if err := s.deps.CancelBenchmark(r.Context(), id); err != nil {
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "Benchmark not found.", map[string]any{"cause": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleListConversations(w http.ResponseWriter, r *http.Request) {
	if s.deps.ListConversations == nil {
		writeJSON(w, http.StatusOK, []contracts.Conversation{})
		return
	}
	items, err := s.deps.ListConversations(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "CONVERSATIONS_LIST_FAILED", "Could not list conversations.", map[string]any{"cause": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleCreateConversation(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title     string `json:"title"`
		ProfileID string `json:"profile_id"`
		ModelID   string `json:"model_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be JSON.", nil)
		return
	}
	if s.deps.CreateConversation == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Conversations are not available yet.", nil)
		return
	}
	conv, err := s.deps.CreateConversation(r.Context(), body.Title, body.ProfileID, body.ModelID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "CONVERSATION_CREATE_FAILED", "Could not create conversation.", map[string]any{"cause": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, conv)
}

func (s *Server) handleUpdateConversation(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	var body struct {
		Title     *string `json:"title"`
		ProfileID *string `json:"profile_id"`
		ModelID   *string `json:"model_id"`
		MemoryOff *bool   `json:"memory_off"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be JSON.", nil)
		return
	}
	if s.deps.UpdateConversation == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Conversations are not available yet.", nil)
		return
	}
	conv, err := s.deps.UpdateConversation(r.Context(), id, body.Title, body.ProfileID, body.ModelID, body.MemoryOff)
	if err != nil {
		writeErrFrom(w, http.StatusBadRequest, "CONVERSATION_UPDATE_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, conv)
}

func (s *Server) handleDeleteConversation(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if s.deps.DeleteConversation == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Conversations are not available yet.", nil)
		return
	}
	if err := s.deps.DeleteConversation(r.Context(), id); err != nil {
		writeErrFrom(w, http.StatusBadRequest, "CONVERSATION_DELETE_FAILED", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "SSE_UNSUPPORTED", "Streaming is not supported.", nil)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	id, ch := s.deps.Bus.Subscribe()
	defer s.deps.Bus.Unsubscribe(id)

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(evt)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.Type, data)
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

func (s *Server) handleOpenAIModels(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   []any{},
	})
}

func (s *Server) handleOpenAIChat(w http.ResponseWriter, r *http.Request) {
	writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Chat completions will be available after local models are installed.", nil)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code, message string, details map[string]any) {
	writeJSON(w, status, contracts.APIError{
		Error: contracts.ErrorBody{Code: code, Message: message, Details: details},
	})
}

// writeErrFrom writes err with its own stable code and params when it has
// them (contracts.CodedError), and with code otherwise. The message is the
// error's English text, for logs and for clients that do not know the code.
func writeErrFrom(w http.ResponseWriter, status int, code string, err error) {
	if own, params := contracts.ErrorCode(err); own != "" {
		writeErr(w, status, own, err.Error(), params)
		return
	}
	writeErr(w, status, code, err.Error(), nil)
}

// corsMiddleware lets the web UI, the desktop webview and keyed clients call
// the API from a browser, and turns websites away (see checkBrowser).
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Origin")
		check := s.checkBrowser(r)
		if check.writeRefusal(w) {
			return
		}
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Access-Control-Request-Private-Network, "+contracts.ClientContractHeader+", "+contracts.LegacyClientContractHeader)
		w.Header().Set("Access-Control-Expose-Headers", contracts.ContractHeader+", "+contracts.LegacyContractHeader)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		if check.originAllowed {
			// Echo the one allowed origin, never "*".
			w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
			// Chrome / WebKit Private Network Access: Wails webview → 127.0.0.1 API.
			w.Header().Set("Access-Control-Allow-Private-Network", "true")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// contractMiddleware says which client contract every response is in, and
// turns away a client built for another major version with a clear
// message, instead of letting it misread data (§68).
func (s *Server) contractMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(contracts.ContractHeader, contracts.ContractVersion)
		w.Header().Set(contracts.LegacyContractHeader, contracts.ContractVersion)
		version, header := contracts.ClientContract(r.Header)
		if err := contracts.CheckClientContractHeader(version, header); err != nil && r.Method != http.MethodOptions {
			writeErr(w, http.StatusUpgradeRequired, "CONTRACT_MISMATCH", err.Error(), map[string]any{"contract": contracts.ContractVersion})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.deps.Logger.Error("api panic", "path", r.URL.Path, "recover", fmt.Sprint(rec))
				writeErr(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error.", nil)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		urlPath := r.URL.Path
		// A webhook's token is its password, so it stays out of the log.
		if strings.HasPrefix(urlPath, "/hooks/") {
			urlPath = "/hooks/…"
		}
		s.deps.Logger.Debug("http", "method", r.Method, "path", urlPath, "dur", time.Since(start))
	})
}

func spaFallback(root fs.FS, files http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		urlPath := r.URL.Path
		if urlPath == "/" {
			files.ServeHTTP(w, r)
			return
		}
		cleaned := path.Clean(stringsTrimPrefix(urlPath))
		if cleaned == "." {
			cleaned = "index.html"
		}
		if _, err := fs.Stat(root, cleaned); err == nil {
			files.ServeHTTP(w, r)
			return
		}
		// SPA fallback
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		files.ServeHTTP(w, r2)
	})
}

func stringsTrimPrefix(p string) string {
	if len(p) > 0 && p[0] == '/' {
		return p[1:]
	}
	return p
}

// ResolveWebRoot returns an fs.FS for the built web UI if present.
func ResolveWebRoot(explicit string) (fs.FS, error) {
	candidates := []string{explicit}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(dir, "web"),
			filepath.Join(dir, "web", "dist"),
			filepath.Join(dir, "..", "Resources", "web"),
			filepath.Join(dir, "..", "share", "yggdrasil", "web"),
		)
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(wd, "web"),
			filepath.Join(wd, "web", "dist"),
			filepath.Join(wd, "..", "web", "dist"),
		)
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			// Prefer a directory that looks like a Vite build (has index.html).
			if _, err := os.Stat(filepath.Join(c, "index.html")); err == nil {
				return os.DirFS(c), nil
			}
		}
	}
	// Fall back to any existing candidate directory (tests / partial layouts).
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			return os.DirFS(c), nil
		}
	}
	return nil, nil
}
