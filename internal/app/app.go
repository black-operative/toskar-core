package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/yeixio/toskar-core/internal/api"
	"github.com/yeixio/toskar-core/internal/api/openai"
	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/benchmark"
	"github.com/yeixio/toskar-core/internal/browser"
	"github.com/yeixio/toskar-core/internal/cache"
	"github.com/yeixio/toskar-core/internal/codeexec"
	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/internal/connectors"
	"github.com/yeixio/toskar-core/internal/diagnostics"
	"github.com/yeixio/toskar-core/internal/discovery"
	"github.com/yeixio/toskar-core/internal/egress"
	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/gjallarhorn"
	"github.com/yeixio/toskar-core/internal/hardware"
	"github.com/yeixio/toskar-core/internal/imagegen"
	"github.com/yeixio/toskar-core/internal/inventory"
	"github.com/yeixio/toskar-core/internal/join"
	"github.com/yeixio/toskar-core/internal/logs"
	"github.com/yeixio/toskar-core/internal/mcp"
	"github.com/yeixio/toskar-core/internal/mimir"
	"github.com/yeixio/toskar-core/internal/models"
	modelhealth "github.com/yeixio/toskar-core/internal/models/health"
	"github.com/yeixio/toskar-core/internal/models/hfclient"
	"github.com/yeixio/toskar-core/internal/models/lifecycle"
	"github.com/yeixio/toskar-core/internal/muninn"
	"github.com/yeixio/toskar-core/internal/nodes"
	"github.com/yeixio/toskar-core/internal/ocr"
	"github.com/yeixio/toskar-core/internal/orchestrator"
	"github.com/yeixio/toskar-core/internal/orchestrator/builtin/simple"
	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/internal/pyenv"
	"github.com/yeixio/toskar-core/internal/ratings"
	"github.com/yeixio/toskar-core/internal/remotetools"
	"github.com/yeixio/toskar-core/internal/replylang"
	"github.com/yeixio/toskar-core/internal/runlog"
	"github.com/yeixio/toskar-core/internal/runtimes"
	"github.com/yeixio/toskar-core/internal/runtimes/external"
	"github.com/yeixio/toskar-core/internal/runtimes/llamacpp"
	"github.com/yeixio/toskar-core/internal/scheduler"
	"github.com/yeixio/toskar-core/internal/share"
	"github.com/yeixio/toskar-core/internal/speech"
	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/internal/store/repositories"
	"github.com/yeixio/toskar-core/internal/tasks"
	"github.com/yeixio/toskar-core/internal/telemetry"
	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/internal/training"
	"github.com/yeixio/toskar-core/internal/updates"
	"github.com/yeixio/toskar-core/internal/version"
	"github.com/yeixio/toskar-core/internal/webfixtures"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// App is the Yggdrasil control-plane application.
type App struct {
	Config *config.Manager
	DB     *store.DB
	Bus    *events.Bus
	API    *api.Server
	Logger *slog.Logger

	Conversations *repositories.ConversationRepo
	Settings      *repositories.SettingsRepo
	Metrics       *repositories.MetricsRepo

	Models           *models.Manager
	Runtimes         *runtimes.Manager
	Profiles         *profiles.Manager
	OrchRegistry     *orchestrator.Registry
	Tasks            *tasks.Manager
	Automations      *repositories.AutomationRepo
	AutomationRunner *automations.Runner
	Scheduler        *scheduler.Scheduler
	Tools            *tools.Registry
	Nodes            *nodes.Manager
	APIKeys          *auth.APIKeyManager
	Pairing          *auth.PairingManager
	Identity         *auth.NodeIdentity
	Benchmarks       *benchmark.Runner
	HF               *hfclient.Client
	Lifecycle        *lifecycle.Sweeper
	Health           *modelhealth.Monitor
	Mimir            *mimir.Store
	Muninn           *muninn.Store
	// Notifications is Gjallarhorn's notification center and delivery.
	Notifications *gjallarhorn.Hub
	// Share admits chat, automations, benchmarks, and training by priority (§60).
	Share *share.Gate
	// Connectors are the connected services, such as GitHub (§32).
	Connectors *connectors.Manager
	// Egress records what left this computer (§63).
	Egress *egress.Log
	// Updates says when a newer Toskar is out.
	Updates *updates.Checker
	// RunLog keeps each request's run trace (§35).
	RunLog *runlog.Store
	// Caches lists every cache and its policy (§36).
	Caches *cache.Registry
	// External is the OpenAI-compatible server whose models can be chosen
	// for a chat (#111).
	External *external.Runtime
	secrets  *auth.SecretStore
	extMu    sync.Mutex
	extList  []contracts.Model
	extAt    time.Time
	extErr   error
	// Ratings is community model ratings (#37).
	Ratings *ratings.Service
	// identity is this computer's Bifrost key; joinTokens and networkMu
	// serve the one-line join (#40).
	identity   *auth.NodeIdentity
	joinTokens *join.Tokens
	networkMu  sync.Mutex
	capCache   *cache.Cache[inventory.Snapshot]
	// tokenCounts keeps counts from models' tokenizers (§66).
	tokenCounts *cache.Cache[int]
	// tokenize counts with a running model; tests replace it.
	tokenize func(ctx context.Context, endpoint, text string) (int, error)
	// windows keeps each running llama-server's actual window (endpoint ->
	// tokens), and shapes each model file's GGUF shape (path -> gguf.Info),
	// for the context gauge (#230).
	windows sync.Map
	shapes  sync.Map
	// window reads a running model's window; tests replace it.
	window func(ctx context.Context, endpoint string) (int, error)
	// sampler keeps a day of the daemon's memory and goroutines (#231).
	sampler *diagnostics.Sampler
	// live keeps a day of this computer's CPU, memory, and GPU figures (#317).
	live *telemetry.Sampler
	// StubReply, when set with stub inference, scripts what the stub model
	// says, for the quality test set (§64). It sees every prompt.
	StubReply func(modelID string, messages []pluginapi.ChatMessage) string
	// MCP runs the MCP tool sources the person added.
	MCP *mcp.Manager
	// Artifacts holds chat attachments and files the assistant produced.
	Artifacts  *artifacts.Store
	summarizer *muninn.Summarizer
	// memTotal caches this computer's memory for Auto model choice.
	memTotal atomic.Uint64
	// failedModels maps a model id to when it last could not answer.
	failedModels sync.Map
	// work counts turns streaming on each computer, by node ID.
	workMu sync.Mutex
	work   map[string]int
	// Speech transcribes audio and reads text aloud (Gungnir §18–19).
	Speech *speech.Engine
	// toolNet places heavy tools on the computer that suits them, and
	// portable are the tools it can place (Gungnir §14–16).
	toolNet  *remotetools.Network
	portable []remotetools.Portable
	// Images makes and edits images on this computer (Gungnir §17).
	Images *imagegen.Setup
	// Video makes short clips on this computer (Gungnir §27).
	Video *imagegen.Setup
	// browser runs each chat's isolated browser (Gungnir §26).
	browser *browser.Manager
	// health turns computer and model health changes into notifications.
	health *healthNotices
	// runs maps a conversation id to its running turn, so Stop can cancel it.
	runs     sync.Map
	Training *training.Service
	// python manages the private Python environments for training and OCR.
	python *pyenv.Manager

	hw *hardware.Detector
	// accel holds the facts the acceleration state needs (acceleration.go).
	accel accelFacts
	// discoveryMu guards advertiser, which settings changes, renames, and
	// shutdown replace or stop from different requests.
	discoveryMu sync.Mutex
	advertiser  *discovery.Advertiser
	internal    *nodes.InternalServer

	stubInference bool

	// bifrostBoundLoopback is true when the Bifrost listener started on loopback;
	// enabling discovery later requires a process restart to rebind to the LAN.
	bifrostBoundLoopback  bool
	discoveryNeedsRestart bool

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// Options configures application startup.
type Options struct {
	DataDir string
	WebRoot fs.FS
	Logger  *slog.Logger
}

// New constructs and wires the application.
func New(opts Options) (*App, error) {
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}
	// A quality run answers the web from fixed pages (tests/quality/web.json)
	// so every model reads the same thing; never on for real use. Read
	// before anything is opened, so a bad file leaves nothing behind.
	var web *webfixtures.Fixtures
	if path := config.Env("WEB_FIXTURES"); path != "" {
		var err error
		if web, err = webfixtures.Load(path); err != nil {
			return nil, fmt.Errorf("web fixtures: %w", err)
		}
	}

	cfgMgr, err := config.NewManager(opts.DataDir)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	if err := cfgMgr.Update(func(c *config.Config) {
		config.ApplyEnvOverrides(c)
	}); err != nil {
		return nil, fmt.Errorf("apply env config: %w", err)
	}
	cfg := cfgMgr.Get()

	if cfg.NodeID == "" {
		nodeID := uuid.NewString()
		if err := cfgMgr.Update(func(c *config.Config) { c.NodeID = nodeID }); err != nil {
			return nil, fmt.Errorf("assign node id: %w", err)
		}
		cfg = cfgMgr.Get()
	}

	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}

	bus := events.NewBus(128)
	convRepo := repositories.NewConversationRepo(db.SQL)
	settingsRepo := repositories.NewSettingsRepo(db.SQL)
	metricsRepo := repositories.NewMetricsRepo(db.SQL)

	secrets := auth.NewSecretStore(cfg.DataDir)
	identity, err := auth.LoadOrCreateIdentity(secrets, cfg.NodeID)
	if err != nil {
		return nil, fmt.Errorf("identity: %w", err)
	}

	catalog, err := models.NewCatalog(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	modelStorage := models.NewStorage(db.SQL, cfg.ModelsDir)
	downloader := models.NewDownloader(modelStorage, bus, db.SQL)
	downloader.QuotaLimitBytes = func(ctx context.Context) (uint64, error) {
		gb, err := settingsRepo.GetInt(ctx, "model_storage_limit_gb", 0)
		if err != nil || gb <= 0 {
			return 0, nil
		}
		return uint64(gb) * 1024 * 1024 * 1024, nil
	}
	modelMgr := models.NewManager(catalog, modelStorage, downloader, bus)
	if presets, err := models.LoadPresets(cfg.DataDir); err == nil {
		modelMgr.SetPresets(presets)
	}
	modelMgr.SyncDynamic(context.Background())

	llamaClient := llamacpp.NewClient()
	rtRegistry := runtimes.NewRegistry()
	llamaRT := llamacpp.New(cfg.RuntimesDir, cfg.LogsDir)
	healthMonitor := modelhealth.NewMonitor(modelhealth.DefaultSettings())
	healthMonitor.Log = logger
	healthMonitor.Probe = llamacpp.Probe
	var ratingsRef atomic.Pointer[ratings.Service]
	healthMonitor.Publish = func(evt modelhealth.Event) {
		bus.Publish(events.New(evt.Type, evt.Payload))
		// A model that stopped working on this computer counts against it
		// in the runtime observations a shared rating may include.
		if r := ratingsRef.Load(); r != nil && evt.Type == modelhealth.EventFailed {
			if node, _ := evt.Payload["node_id"].(string); node == "" || node == cfgMgr.Get().NodeID {
				model, _ := evt.Payload["model_id"].(string)
				reason, _ := evt.Payload["reason"].(string)
				r.RecordCrash(context.Background(), model, reason == modelhealth.ReasonOOM)
			}
		}
	}
	rtRegistry.Register(llamaRT)
	extRuntime := external.New(external.Config{BaseURL: mustNormalizeExternal(cfg.ExternalOpenAIURL)})
	rtRegistry.Register(extRuntime)
	rtMgr := runtimes.NewManager(rtRegistry, db.SQL, bus, llamaClient)
	rtMgr.OnStart = func(ctx context.Context, modelID string, err error) {
		if r := ratingsRef.Load(); r != nil {
			r.RecordStart(ctx, modelID, err)
		}
	}

	profileMgr := profiles.NewManager(db.SQL)
	if err := profileMgr.EnsurePresets(context.Background()); err != nil {
		return nil, fmt.Errorf("profiles: %w", err)
	}

	orchReg := orchestrator.NewRegistry()
	orchReg.Register(simple.New())

	sched := scheduler.New(bus)
	wd, _ := os.Getwd()
	toolReg := tools.NewRegistry(wd, bus)
	// Every tool call is audited (Gungnir §13); records expire with run records.
	toolReg.SetAudit(tools.NewAuditLog(db.SQL))

	pairing := auth.NewPairingManager(db.SQL, identity)
	if err := pairing.RecomputeFingerprints(context.Background()); err != nil {
		logger.Warn("recompute paired computer fingerprints", "error", err)
	}
	apiKeyMgr := auth.NewAPIKeyManager(db.SQL, secrets)

	if key, err := secrets.Read(externalKeySecret); err == nil {
		c := extRuntime.Config()
		c.APIKey = strings.TrimSpace(key)
		extRuntime.SetConfig(c)
	}
	a := &App{
		External:      extRuntime,
		secrets:       secrets,
		Share:         share.New(0),
		Config:        cfgMgr,
		DB:            db,
		Bus:           bus,
		Logger:        logger,
		Conversations: convRepo,
		Settings:      settingsRepo,
		Metrics:       metricsRepo,
		Models:        modelMgr,
		Runtimes:      rtMgr,
		Profiles:      profileMgr,
		OrchRegistry:  orchReg,
		Scheduler:     sched,
		Tools:         toolReg,
		APIKeys:       apiKeyMgr,
		Pairing:       pairing,
		Identity:      identity,
		hw:            &hardware.Detector{DataPath: cfg.DataDir},
		stubInference: config.EnvTruthy("STUB_INFERENCE"),
		Health:        healthMonitor,
	}
	healthMonitor.Stopper = llamaStopper{rt: rtMgr}
	healthMonitor.Memory = a.memoryPressure
	llamaRT.OnProcessExit(func(instanceID, modelID string, exitCode int, stderrTail string) {
		a.Health.ReportProcessExit(instanceID, exitCode, stderrTail)
		a.Logger.Info("model runtime exited", "model_id", modelID, "running_model_id", instanceID, "exit_code", exitCode)
	})

	if a.stubInference {
		if err := modelMgr.EnsureStubModel(context.Background()); err != nil {
			return nil, fmt.Errorf("stub model: %w", err)
		}
		logger.Info("stub inference enabled", "model_id", models.StubModelID)
	}
	// Record what leaves this computer: web tools and connected services
	// as they run, and chats sent to servers elsewhere (§63).
	a.Egress = egress.New(db.SQL)
	a.RunLog = runlog.NewStore(db.SQL)
	toolReg.SetObserver(a.recordToolEgress)
	rtMgr.OnRemote = func(ctx context.Context, host string) {
		a.Egress.Add(ctx, egress.ExternalServer, host, "prompt and conversation")
	}
	// Connected services add tools; their credentials stay in the secrets
	// directory and are added only when a tool runs (§32).
	a.Connectors = connectors.NewManager(db.SQL, secrets, toolReg, connectors.GitHub{}, connectors.HomeAssistant{}, connectors.Email{}, connectors.Calendar{})
	if err := a.Connectors.Load(context.Background()); err != nil {
		logger.Warn("load connected services", "error", err)
	}
	// MCP tool sources add tools the same way. Their tool lists are kept,
	// so none is started until a tool is needed.
	a.MCP = mcp.NewManager(db.SQL, secrets, toolReg, version.Version, logger)
	a.MCP.Sample = a.mcpSample
	if err := a.MCP.Load(context.Background()); err != nil {
		logger.Warn("load tool sources", "error", err)
	}
	// After connected tools are in the catalog, so turning one off sticks.
	a.loadDisabledTools(context.Background())

	bench := benchmark.NewRunner()
	bench.ModelPath = modelMgr.Path
	bench.StartModel = func(ctx context.Context, modelID, modelPath string) (pluginapi.RunningModel, error) {
		return rtMgr.StartModel(ctx, "llamacpp", pluginapi.ModelStartConfig{
			ModelID: modelID, ModelPath: modelPath, Adapters: a.localAdapters(ctx, modelID),
		})
	}
	bench.Admit = func(ctx context.Context, waiting func(string)) (func(), error) {
		work, err := a.enterWork(ctx, share.Benchmark, "benchmark", waiting)
		return work.Done, err
	}
	bench.StopModel = func(ctx context.Context, instanceID string) error {
		return rtMgr.StopModel(ctx, "llamacpp", instanceID)
	}
	bench.ListRunning = func(ctx context.Context) ([]pluginapi.RunningModel, error) {
		return rtMgr.ListRunning(ctx, "llamacpp")
	}
	bench.Chat = rtMgr.Chat
	a.Benchmarks = bench

	a.Nodes = nodes.NewManager(db.SQL, bus, pairing, cfg.NodeID, cfg.NodeName, a.detectHardware)
	a.Nodes.SetLocalTraining(a.isTraining)
	a.Nodes.SetAdvertiseAddr(a.bifrostAdvertiseAddr)
	a.Nodes.SetStaticPeers(cfg.StaticPeers)
	_ = a.syncInternalBind()

	a.Tasks = tasks.NewManager(db.SQL, bus, profileMgr, orchReg, rtMgr, sched, toolReg, a.listNodes, modelMgr.Path)
	a.Tasks.SetClusterHooks(a.placeRole, func(ctx context.Context, nodeID, modelID string, messages []pluginapi.ChatMessage) (<-chan pluginapi.ChatChunk, error) {
		return a.generateOnNode(ctx, nodeID, modelID, "", "", messages)
	})

	openaiHandler := &openai.Handler{
		Profiles: profileMgr,
		Runtimes: rtMgr,
		Chat:     a,
		Bus:      bus,
		Auth:     a.openAIAuth,
		// Chat completions also learn what the key may ask of the assistant (§62).
		Permissions: a.openAIPermissions,
	}

	webRoot := opts.WebRoot
	if webRoot == nil {
		if root, err := api.ResolveWebRoot(cfg.WebUIDir); err == nil {
			webRoot = root
		}
	}

	a.API = api.NewServer(api.Dependencies{
		Config:   cfgMgr,
		Bus:      bus,
		Logger:   logger,
		WebRoot:  webRoot,
		OpenAI:   openaiHandler,
		Hardware: a.detectHardware,
		ListConversations: func(ctx context.Context) ([]contracts.Conversation, error) {
			return convRepo.List(ctx)
		},
		CreateConversation: func(ctx context.Context, title, profileID, modelID string) (contracts.Conversation, error) {
			return convRepo.Create(ctx, title, profileID, modelID)
		},
		UpdateConversation: func(ctx context.Context, id string, title, profileID, modelID *string, memoryOff *bool) (contracts.Conversation, error) {
			return convRepo.Update(ctx, id, repositories.ConversationPatch{
				Title:     title,
				ProfileID: profileID,
				ModelID:   modelID,
				MemoryOff: memoryOff,
			})
		},
		DeleteConversation: func(ctx context.Context, id string) error {
			if a.Artifacts != nil {
				if err := a.Artifacts.DeleteConversation(ctx, id); err != nil {
					return err
				}
			}
			return convRepo.Delete(ctx, id)
		},
		ListMessages: func(ctx context.Context, id string) ([]contracts.Message, error) {
			return convRepo.ListMessages(ctx, id)
		},
		GetSettings: func(ctx context.Context) (contracts.SettingsView, error) {
			return a.settingsView(ctx)
		},
		UpdateSettings: func(ctx context.Context, patch map[string]any) (contracts.SettingsView, error) {
			if err := a.applySettingsPatch(ctx, patch); err != nil {
				return contracts.SettingsView{}, err
			}
			return a.settingsView(ctx)
		},
		ResetApp: a.ResetApp,
		ListPerformance: func(ctx context.Context, sort, order string, limit int) ([]contracts.GenerationRun, error) {
			if a.Metrics == nil {
				return []contracts.GenerationRun{}, nil
			}
			return a.Metrics.List(ctx, repositories.ListFilter{Sort: sort, Order: order, Limit: limit})
		},
		ListBenchmarkWorkloads: func() []contracts.BenchmarkWorkload {
			return a.Benchmarks.ListWorkloads()
		},
		StartBenchmark: func(ctx context.Context, req contracts.BenchmarkRequest) (*contracts.BenchmarkJob, error) {
			// Detach from the HTTP request context so the job survives after 202.
			return a.Benchmarks.Start(context.Background(), req)
		},
		GetBenchmark: func(ctx context.Context, id string) (*contracts.BenchmarkJob, error) {
			return a.Benchmarks.Get(id)
		},
		ListBenchmarks: func(ctx context.Context) ([]contracts.BenchmarkJob, error) {
			return a.Benchmarks.List(), nil
		},
		CancelBenchmark: func(ctx context.Context, id string) error {
			return a.Benchmarks.Cancel(id)
		},
		ExportDiagnostics: func(ctx context.Context, includeConversations bool) (string, error) {
			return a.exportDiagnostics(ctx, includeConversations)
		},
		LiveFigures: a.liveAll,
		GPUSetup:    a.gpuSetup,
		RuntimeHistory: func() diagnostics.RuntimeHistory {
			if a.sampler == nil {
				return diagnostics.RuntimeHistory{Now: diagnostics.Sample()}
			}
			return a.sampler.History()
		},
		ListLogs: func(ctx context.Context) ([]logs.Entry, error) {
			return (&logs.Store{Dir: a.Config.Get().LogsDir}).List()
		},
		GetLog: func(ctx context.Context, name string, tailBytes int64) (logs.Content, error) {
			return (&logs.Store{Dir: a.Config.Get().LogsDir}).ReadTail(name, tailBytes)
		},
		Version: func() contracts.VersionResponse {
			offer := version.CurrentOffer()
			return contracts.VersionResponse{
				Version: offer.Version, Commit: offer.Commit,
				BuildDate: version.BuildDate, Product: "Yggdrasil",
				License: offer.License, Source: offer.Source,
			}
		},
		ListModels: a.listModelsCluster,
		RecommendModels: func(ctx context.Context, purpose string) (contracts.Recommendation, error) {
			hw, _ := a.detectHardware(ctx)
			return modelMgr.Recommend(ctx, purpose, hw, a.communitySignals(ctx))
		},
		ModelsFit: a.modelsFitAll,
		BrowseModels: func(ctx context.Context, query string, limit int) ([]contracts.BrowseModel, error) {
			if a.HF == nil {
				a.HF = hfclient.New()
			}
			return a.HF.Search(ctx, query, limit)
		},
		InstallModel: func(ctx context.Context, id string, wait bool, nodeID string) error {
			return a.installModelOn(ctx, id, nodeID, wait)
		},
		InstallModelFromURL: func(ctx context.Context, req contracts.InstallFromURLRequest, wait bool) (string, error) {
			return a.installFromURLOn(ctx, req, wait)
		},
		DeleteModel: func(ctx context.Context, id, nodeID string) error {
			return a.deleteModelOn(ctx, id, nodeID)
		},
		ListRunningModels: a.listRunningAll,
		Acceleration:      a.healthAcceleration,
		StartModel: func(ctx context.Context, id, nodeID string) (contracts.RunningModelView, error) {
			return a.startModel(ctx, id, nodeID)
		},
		StopModel: func(ctx context.Context, instanceID, nodeID string) error {
			return a.stopModel(ctx, instanceID, nodeID)
		},
		ListProfiles: profileMgr.List,
		CreateProfile: func(ctx context.Context, p contracts.AIProfile) (contracts.AIProfile, error) {
			return profileMgr.Create(ctx, p)
		},
		GetProfile: profileMgr.Get,
		UpdateProfile: func(ctx context.Context, p contracts.AIProfile) (contracts.AIProfile, error) {
			if err := profileMgr.Update(ctx, p); err != nil {
				return contracts.AIProfile{}, err
			}
			return profileMgr.Get(ctx, p.ID)
		},
		DeleteProfile: profileMgr.Delete,
		ResetProfile:  profileMgr.ResetPreset,
		ListRuntimes:  rtMgr.List,
		InstallRuntime: func(ctx context.Context, id string) error {
			return rtMgr.Install(ctx, id, runtimes.InstallOptions{})
		},
		ListNodes: func(ctx context.Context) ([]contracts.Node, error) {
			return a.listNodesWithHardware(ctx)
		},
		RefreshDiscovery:      a.Nodes.RefreshDiscovery,
		StartPairing:          a.Nodes.StartPairing,
		ClaimPairing:          a.Nodes.ClaimPairing,
		ApprovePairing:        a.Nodes.ApprovePairing,
		ListPairingOffers:     a.Nodes.ListIncomingOffers,
		ReceivePairingOffer:   a.Nodes.ReceiveOffer,
		LookupOutboundPairing: a.Nodes.LookupOutbound,
		RevokeNode:            a.Nodes.Revoke,
		ListTasks:             a.Tasks.List,
		CreateTask:            a.Tasks.Create,
		GetTask:               a.Tasks.Get,
		RunTask:               a.Tasks.Run,
		DecideTool:            toolReg.Decide,
		DescribeTool:          func(ctx context.Context, id string) (any, error) { return a.describeTool(ctx, id) },
		ListToolRuns: func(ctx context.Context, toolID, conversationID string, limit int) (any, error) {
			return tools.NewAuditLog(db.SQL).List(ctx, tools.AuditFilter{ToolID: toolID, ConversationID: conversationID, Limit: limit})
		},
		ListTools: func(ctx context.Context) (any, error) {
			return a.listToolViews(ctx)
		},
		SetToolEnabled: a.setToolEnabled,
		TestTool:       a.testTool,
		ToolActivity: func() any {
			return toolReg.Recent()
		},
		ToolProviders: func(ctx context.Context) any {
			return a.toolProviders(ctx)
		},
		ListAPIKeys:          apiKeyMgr.List,
		CreateAPIKey:         apiKeyMgr.Create,
		RevokeAPIKey:         apiKeyMgr.Revoke,
		RotateAPIKey:         apiKeyMgr.Rotate,
		SetAPIKeyPermissions: apiKeyMgr.SetPermissions,
		VerifyAPIKey:         apiKeyMgr.Verify,
		Devices:              &auth.DevicePairer{CreateKey: apiKeyMgr.CreateDevice},
		PhoneAddress:         a.phoneAddress,
		EnableLANForPhone:    a.enableLANForPhone,
		StopChat:             a.StopChat,
		Chat: func(w http.ResponseWriter, r *http.Request, conversationID, profileID, modelID, message string, stream bool, execution string) error {
			return a.HandleHTTPChat(w, r, conversationID, profileID, modelID, message, stream, execution)
		},
	})

	a.HF = hfclient.New()
	a.setupCaches()
	a.Lifecycle = &lifecycle.Sweeper{
		Logger: logger,
		OnUnload: func(inst lifecycle.Instance) {
			a.Bus.Publish(events.New(events.ModelUnloaded, map[string]any{
				"model_id":    inst.ModelID,
				"instance_id": inst.InstanceID,
				"reason":      "idle",
			}))
		},
		List: func(ctx context.Context) ([]lifecycle.Instance, error) {
			views, err := a.localRunningViews(ctx)
			if err != nil {
				return nil, err
			}
			out := make([]lifecycle.Instance, 0, len(views))
			for _, v := range views {
				inst := lifecycle.Instance{InstanceID: v.InstanceID, ModelID: v.ModelID}
				if v.LastUsedAt != nil {
					inst.LastUsed = *v.LastUsedAt
				} else if t, ok := runtimeLastUsed.Load(v.ModelID); ok {
					inst.LastUsed = t.(time.Time)
				}
				out = append(out, inst)
			}
			return out, nil
		},
		Stop: func(ctx context.Context, instanceID string) error {
			return a.stopModelLocal(ctx, instanceID)
		},
		Settings: func(ctx context.Context) (string, int, error) {
			mode, _ := a.Settings.GetString(ctx, "model_lifecycle", "automatic")
			mins, _ := a.Settings.GetInt(ctx, "idle_unload_minutes", 15)
			return mode, mins, nil
		},
	}

	// Gjallarhorn: every notice is kept in the notification center; the
	// desktop is one delivery channel.
	a.Notifications = gjallarhorn.NewHub(db.SQL, bus, desktopChannel{
		settings: settingsRepo,
		send:     automations.OSSender{},
		toShell:  shellNotices(bus, config.Env("DESKTOP_NOTIFICATIONS")),
	})
	a.health = newHealthNotices()
	// Email and webhook destinations keep their passwords and signing
	// secrets in the secrets directory, and what they send is recorded in
	// What left this computer.
	a.Notifications.SetSecrets(secrets)
	a.Notifications.SetSettings(settingsRepo)
	a.Notifications.SetEgress(a.Egress)
	a.API.BindNotifications(a.Notifications)
	a.API.BindConnectors(a.Connectors)
	a.API.BindMCP(a.MCP, mcp.NewServer(a.mcpBackend()), ctlPath)
	a.API.BindPersonal(a)
	a.API.BindPrivacy(a)
	a.Updates = &updates.Checker{
		Current:   version.Version,
		Supported: updateCheckSupported,
		On: func(ctx context.Context) bool {
			on, err := settingsRepo.GetBool(ctx, updates.Setting, true)
			return err == nil && on
		},
		Sent: func(ctx context.Context, host, detail string) {
			a.Egress.Add(ctx, egress.UpdateCheck, host, detail)
		},
	}
	a.API.BindUpdates(a.Updates)
	a.Ratings = a.newRatings(cfg)
	ratingsRef.Store(a.Ratings)
	a.API.BindRatings(a.Ratings)
	a.identity = identity
	a.joinTokens = &join.Tokens{DB: db.SQL}
	a.API.BindNetwork(a)
	a.API.BindExternal(a)
	a.API.BindRuns(a.RunLog)
	a.API.BindCapabilities(a)
	a.API.BindCaches(a)

	autoRepo := repositories.NewAutomationRepo(db.SQL)
	a.Automations = autoRepo
	a.AutomationRunner = &automations.Runner{
		Store:  autoRepo,
		Exec:   automationExecutor{app: a},
		Notify: automationNotifier{settings: settingsRepo, send: automations.OSSender{}, hub: a.Notifications},
		Bus:    bus,
		Logger: logger,
		Post:   a.postAutomationResult,
		Pause: func(ctx context.Context, id string) error {
			enabled := false
			_, err := a.Automations.Update(ctx, id, automations.Patch{Enabled: &enabled}, time.Now())
			return err
		},
	}
	a.API.BindAutomations(api.Dependencies{
		ListAutomations: a.Automations.List,
		CreateAutomation: func(ctx context.Context, in automations.CreateInput) (automations.Automation, error) {
			created, err := a.Automations.Create(ctx, in, time.Now())
			if err != nil {
				return automations.Automation{}, err
			}
			if err := a.enableBackgroundWhenScheduled(ctx); err != nil && a.Logger != nil {
				a.Logger.Warn("could not keep the daemon running for schedules", "error", err)
			}
			return created, nil
		},
		GetAutomation:         a.Automations.History,
		ListAutomationRuns:    a.Automations.RunsPage,
		ContinueAutomationRun: a.continueAutomationRun,
		UpdateAutomation: func(ctx context.Context, id string, patch automations.Patch) (automations.Automation, error) {
			return a.Automations.Update(ctx, id, patch, time.Now())
		},
		DeleteAutomation:  a.Automations.Delete,
		RunAutomation:     a.AutomationRunner.RunNow,
		PreviewAutomation: a.AutomationRunner.Preview,
		ParseAutomation:   a.parseAutomation,
		PauseAutomation: func(ctx context.Context, id string) (automations.Automation, error) {
			enabled := false
			return a.Automations.Update(ctx, id, automations.Patch{Enabled: &enabled}, time.Now())
		},
		ResumeAutomation: func(ctx context.Context, id string) (automations.Automation, error) {
			enabled := true
			return a.Automations.Update(ctx, id, automations.Patch{Enabled: &enabled}, time.Now())
		},
	})

	a.Mimir = mimir.NewStore(db.SQL, filepath.Join(cfg.DataDir, "knowledge"))
	a.Mimir.SetSecrets(secrets)
	// The file store comes first: the tools below read and attach chat files.
	a.Artifacts = artifacts.NewStore(db.SQL, filepath.Join(cfg.DataDir, "artifacts"))
	a.python = pyenv.New(filepath.Join(cfg.RuntimesDir, "python"))
	a.Mimir.SetRecognizer(&ocr.Recognizer{Python: a.python, WorkDir: filepath.Join(cfg.DataDir, "knowledge", "ocr-jobs")})
	// Code runs only inside the operating system's sandbox (Gungnir §20).
	a.Tools.Register(&codeexec.Tool{Python: a.python, Sandbox: codeexec.Detect(), Store: a.Artifacts,
		WorkDir: filepath.Join(cfg.DataDir, "code-runs"), PythonRoot: a.python.Root})
	// Speech runs on this computer; models are kept with the runtimes.
	a.Speech = &speech.Engine{Python: a.python, Dir: filepath.Join(cfg.RuntimesDir, "speech")}
	a.toolNet = a.newToolNetwork(cfg)
	a.registerPortable(&speech.TranscribeTool{Engine: a.Speech, Store: a.Artifacts})
	a.registerPortable(&speech.SynthesizeTool{Engine: a.Speech, Store: a.Artifacts})
	a.API.BindSpeech(a.Speech, a.Artifacts)
	knowledgeModels := newKnowledgeModels(a)
	a.Mimir.SetModels(knowledgeModels)
	a.Muninn = muninn.NewStore(db.SQL)
	// Memories are found by meaning too, in any language, with the same
	// embedding model as knowledge (multilingual spec §18).
	a.Muninn.SetEmbedder(func(ctx context.Context) (muninn.Embedder, error) {
		emb, err := knowledgeModels.Embedder(ctx)
		if err != nil || emb == nil {
			return nil, err
		}
		return emb, nil
	})
	a.summarizer = &muninn.Summarizer{Store: a.Muninn}
	a.API.BindMemory(a.Muninn)
	a.Tools.Register(&artifacts.CreateTool{Store: a.Artifacts})
	a.Tools.Register(&artifacts.AnalyzeTool{Store: a.Artifacts})
	a.Tools.Register(&scheduleTool{app: a})
	a.API.BindArtifacts(a.Artifacts)
	a.API.BindKnowledge(a.Mimir)
	a.Images = a.newImageSetup(cfg)
	images := &imagegen.Engine{Setup: a.Images, WorkDir: filepath.Join(cfg.DataDir, "image-jobs")}
	a.registerPortable(&imagegen.GenerateTool{Engine: images, Store: a.Artifacts})
	a.registerPortable(&imagegen.EditTool{Engine: images, Store: a.Artifacts})
	a.registerPlaces(cfg)
	a.registerBrowser(cfg)
	a.API.BindImages(a.Images)
	a.Video = a.newVideoSetup(cfg)
	videos := &imagegen.Engine{Setup: a.Video, WorkDir: filepath.Join(cfg.DataDir, "video-jobs"), What: "video generation"}
	a.registerPortable(&imagegen.VideoTool{Engine: videos, Store: a.Artifacts})
	a.API.BindVideo(a.Video)
	a.Training = a.newTrainingService()
	if err := a.Training.Recover(context.Background()); err != nil {
		return nil, fmt.Errorf("training: %w", err)
	}
	a.API.BindTraining(a.Training, modelMgr.Catalog().List)
	openaiHandler.Specialized = a.Training.DeployedModels

	acceptor := a.newJoinAcceptor()
	a.internal = nodes.NewInternalServer(nodes.InternalDeps{
		Config:          a.Config.Get(),
		Logger:          logger,
		Identity:        identity,
		Pairing:         pairing,
		Hardware:        a.detectHardware,
		Live:            a.localLive,
		ListModels:      modelMgr.List,
		InstallModel:    modelMgr.Install,
		InstallFromURL:  modelMgr.InstallFromURL,
		DeleteModel:     modelMgr.Delete,
		ListRunning:     a.localRunningViews,
		StartModel:      a.startModelLocal,
		StopModel:       a.stopModelLocal,
		Chat:            a.internalChat,
		ReceiveOffer:    a.Nodes.ReceiveOffer,
		CompletePairing: a.Nodes.CompletePairing,
		LookupOutbound:  a.Nodes.LookupOutbound,
		Training:        a.Training.RemoteHandler(),
		Tools:           remotetools.Handler(a.portable, a.enterToolWork),
		TrainingActive:  a.isTraining,
		JoinHello:       acceptor.Hello,
		Join:            acceptor.Join,
		Leave:           a.peerLeft,
	})

	if web != nil {
		webfixtures.Register(a.Tools, web)
		logger.Warn("web tools answer from test pages, not the internet", "file", config.Env("WEB_FIXTURES"))
	}

	return a, nil
}

func (a *App) detectHardware(ctx context.Context) (contracts.HardwareInventory, error) {
	return a.hw.Detect(ctx)
}

func (a *App) openAIAuth(r *http.Request) error {
	return a.authorizeControlRequest(r)
}

// openAIPermissions authorizes a chat completion and returns what its key
// may ask of the assistant. A key's limits apply even on this computer; a
// request here without a key gets the defaults.
func (a *App) openAIPermissions(r *http.Request) (auth.APIKeyPermissions, error) {
	if err := a.authorizeControlRequest(r); err != nil {
		return auth.APIKeyPermissions{}, err
	}
	token, err := auth.BearerToken(r)
	if err != nil || token == "" {
		return auth.DefaultAPIKeyPermissions(), nil
	}
	rec, err := a.APIKeys.Verify(r.Context(), token)
	if err != nil {
		if config.ListensBeyondLoopback(a.Config.Get().APIHost) && !auth.FromThisComputer(r) {
			return auth.APIKeyPermissions{}, err
		}
		// On this computer a key is optional; a wrong one gets the defaults.
		return auth.DefaultAPIKeyPermissions(), nil
	}
	return rec.Permissions, nil
}

func (a *App) authorizeControlRequest(r *http.Request) error {
	if !config.ListensBeyondLoopback(a.Config.Get().APIHost) || auth.FromThisComputer(r) {
		return nil
	}
	token, err := auth.BearerToken(r)
	if err != nil {
		return err
	}
	if _, err := a.APIKeys.Verify(r.Context(), token); err != nil {
		return err
	}
	return nil
}

// requireKeyForRemoteBind refuses a non-loopback control API with no key.
// TOSKAR_API_KEY (or YGGDRASIL_API_KEY), when set, is hashed and stored if it is not already valid.
func (a *App) requireKeyForRemoteBind(ctx context.Context) error {
	cfg := a.Config.Get()
	if !config.ListensBeyondLoopback(cfg.APIHost) {
		return nil
	}
	if supplied := strings.TrimSpace(config.Env("API_KEY")); supplied != "" {
		if _, err := a.APIKeys.Adopt(ctx, "bootstrap", supplied); err != nil {
			return fmt.Errorf("configure API key: %w", err)
		}
		return nil
	}
	keys, err := a.APIKeys.List(ctx)
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		// Turned on in the app, such as by Connect a phone, with no phone
		// ever connecting: nothing could connect without a key, so go back
		// to this computer only rather than refuse to start (#216). A host
		// set in the environment, as in Docker, still needs a key.
		if cfg.LANAPIEnabled && config.Env("API_HOST") == "" {
			a.Logger.Warn("local network access turned off: no API key or phone can use it")
			return a.Config.Update(func(c *config.Config) {
				c.LANAPIEnabled, c.APIHost = false, config.DefaultBindLoopback
			})
		}
		return fmt.Errorf("%w (api host %s)", auth.ErrAPIKeyRequired, cfg.APIHost)
	}
	return nil
}

// Start runs the API server until context cancellation.
func (a *App) Start(ctx context.Context) error {
	// Live profiles for diagnosing a running daemon, only when asked for
	// and only on this computer (#231).
	if addr := config.Env("PPROF"); addr != "" {
		if got, err := diagnostics.ServeProfiles(ctx, addr); err != nil {
			a.Logger.Warn("profiles not served", "error", err)
		} else {
			a.Logger.Info("profiles served", "addr", "http://"+got.String()+"/debug/pprof/")
		}
	}
	ctx, a.cancel = context.WithCancel(ctx)
	// The sampler runs on the context Shutdown cancels: on the caller's, a
	// Start that failed early (such as without an API key) left Shutdown
	// waiting for it forever.
	a.sampler = diagnostics.NewSampler()
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		a.sampler.Run(ctx)
	}()
	if a.Updates != nil {
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			a.Updates.Run(ctx)
		}()
	}
	a.startLive(ctx)
	// Tasks an earlier run left pending or running can't finish now, and
	// chats from before chat tasks were settled never left pending.
	if n, err := a.Tasks.SettleInterrupted(ctx); err == nil && n > 0 {
		a.Logger.Info("settled tasks left unfinished", "count", n)
	}
	a.notifyFromEvents(ctx)
	a.Notifications.Start(ctx)
	a.watchCapabilities(ctx)
	a.keepRunRecordsTidy(ctx)
	a.upgradeRuntimeBuilds(ctx)
	_ = a.syncInternalBind()
	if err := a.requireKeyForRemoteBind(ctx); err != nil {
		return err
	}
	cfg := a.Config.Get()
	if err := a.enableBackgroundWhenScheduled(ctx); err != nil && a.Logger != nil {
		a.Logger.Warn("could not keep the daemon running for schedules", "error", err)
	}
	if cfg.DiscoveryEnabled && (cfg.InternalHost == "" || cfg.InternalHost == "127.0.0.1" || cfg.InternalHost == "localhost") {
		_ = a.Config.Update(func(c *config.Config) { c.InternalHost = "0.0.0.0" })
		cfg = a.Config.Get()
	}

	a.Logger.Info("toskar starting",
		"version", version.Version,
		"node_id", cfg.NodeID,
		"data_dir", cfg.DataDir,
		"api", cfg.APIAddr(),
		"bifrost", cfg.InternalAddr(),
		"advertise", a.bifrostAdvertiseAddr(),
		"discovery", cfg.DiscoveryEnabled,
	)

	host := cfg.InternalHost
	a.bifrostBoundLoopback = host == "" || host == "127.0.0.1" || host == "localhost"

	if cfg.DiscoveryEnabled || len(cfg.StaticPeers) > 0 {
		if cfg.DiscoveryEnabled {
			adv, err := startAdvertise(cfg, true)
			if err != nil {
				a.Logger.Warn("mdns advertise failed", "error", err)
			} else {
				a.discoveryMu.Lock()
				a.advertiser = adv
				a.discoveryMu.Unlock()
			}
		}
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			// Immediate + short retries so Docker static peers come up across container boot order.
			for i := 0; i < 15; i++ {
				_ = a.Nodes.RefreshDiscovery(ctx)
				select {
				case <-ctx.Done():
					return
				case <-time.After(2 * time.Second):
				}
			}
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					_ = a.Nodes.RefreshDiscovery(ctx)
				}
			}
		}()
	}

	// Keep paired-computer liveness fresh so chat placement can reuse it,
	// whether or not discovery is on.
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		ticker := time.NewTicker(nodes.LivenessInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.Nodes.RefreshPairedLiveness(ctx)
			}
		}
	}()

	if a.Lifecycle != nil {
		a.Lifecycle.Start(ctx)
	}
	if a.MCP != nil {
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			a.MCP.Run(ctx)
		}()
	}
	a.indexKnowledge(ctx)
	if a.AutomationRunner != nil {
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			a.AutomationRunner.Start(ctx)
		}()
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			a.digestLoop(ctx)
		}()
	}
	if a.Health != nil {
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			a.Health.Run(ctx)
		}()
	}

	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		addr := cfg.InternalAddr()
		a.Logger.Info("starting bifrost listener", "addr", addr)
		if err := a.internal.ListenAndServe(addr); err != nil && ctx.Err() == nil {
			a.Logger.Error("internal server failed", "error", err, "addr", addr)
		}
	}()

	errCh := make(chan error, 1)
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		if err := a.API.ListenAndServe(cfg.APIAddr()); err != nil && ctx.Err() == nil {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		return a.Shutdown(context.Background())
	case err := <-errCh:
		return err
	}
}

// Shutdown stops services and closes resources.
func (a *App) Shutdown(ctx context.Context) error {
	if a.cancel != nil {
		a.cancel()
	}
	a.discoveryMu.Lock()
	if a.advertiser != nil {
		a.advertiser.Stop()
		a.advertiser = nil
	}
	a.discoveryMu.Unlock()
	if a.Lifecycle != nil {
		a.Lifecycle.Halt()
	}
	if a.browser != nil {
		a.browser.CloseAll()
	}

	// Unload models first so llama-server children exit before HTTP shutdown.
	if a.Runtimes != nil {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 3*time.Second)
		a.Runtimes.StopAll(stopCtx)
		stopCancel()
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	_ = a.API.Shutdown(shutdownCtx)
	_ = a.internal.Shutdown(shutdownCtx)
	a.wg.Wait()
	if a.DB != nil {
		_ = a.DB.Close()
	}
	a.Logger.Info("toskar stopped")
	return nil
}

func (a *App) settingsView(ctx context.Context) (contracts.SettingsView, error) {
	cfg := a.Config.Get()
	advanced, _ := a.Settings.GetBool(ctx, "advanced_mode", false)
	lifecycle, _ := a.Settings.GetString(ctx, "model_lifecycle", "automatic")
	idleMins, _ := a.Settings.GetInt(ctx, "idle_unload_minutes", 15)
	keepBackground, _ := a.Settings.GetBool(ctx, "keep_running_in_background", false)
	defaultProfile, _ := a.Settings.GetString(ctx, "default_profile_id", "")
	defaultExec, _ := a.Settings.GetString(ctx, "default_execution", "automatic")
	downloadBehavior, _ := a.Settings.GetString(ctx, "download_behavior", "ask")
	storageLimit, _ := a.Settings.GetInt(ctx, "model_storage_limit_gb", 0)
	saveChat, _ := a.Settings.GetBool(ctx, "save_chat_history", true)
	memoryEnabled, _ := a.Settings.GetBool(ctx, "memory_enabled", true)
	saveTask, _ := a.Settings.GetBool(ctx, "save_task_history", true)
	notifyTask, _ := a.Settings.GetBool(ctx, "notify_task_finish", true)
	notifyPeer, _ := a.Settings.GetBool(ctx, "notify_peer_offline", true)
	communityRatings, _ := a.Settings.GetBool(ctx, ratings.SettingShow, false)
	ratingsPrompts, _ := a.Settings.GetBool(ctx, ratings.SettingAsk, true)
	updateCheck, _ := a.Settings.GetBool(ctx, updates.Setting, true)
	toolTerminal, _ := a.Settings.GetString(ctx, "tool_terminal", "ask")
	toolFiles, _ := a.Settings.GetString(ctx, "tool_file_writes", "ask")
	toolGit, _ := a.Settings.GetString(ctx, "tool_git", "ask")
	launchAtLogin, _ := a.Settings.GetBool(ctx, "launch_at_login", false)
	uiLocale, _ := a.Settings.GetString(ctx, "ui_locale", "")
	assistantMode, _ := a.Settings.GetString(ctx, "assistant_language_mode", replylang.ModeAuto)
	assistantLanguage, _ := a.Settings.GetString(ctx, "assistant_language", "")
	digest, _ := a.Settings.GetString(ctx, settingDigest, "")
	digestZone, _ := a.Settings.GetString(ctx, settingDigestZone, "")
	if assistantMode == "" {
		assistantMode = replylang.ModeAuto
	}
	if defaultExec == "" {
		defaultExec = "automatic"
	}
	if downloadBehavior == "" {
		downloadBehavior = "ask"
	}
	return contracts.SettingsView{
		DataDir: cfg.DataDir, ModelsDir: cfg.ModelsDir, RuntimesDir: cfg.RuntimesDir,
		LogsDir: cfg.LogsDir, APIHost: cfg.APIHost, APIPort: cfg.APIPort,
		LANAPIEnabled: cfg.LANAPIEnabled, WebUIEnabled: cfg.WebUIEnabled,
		DiscoveryEnabled: cfg.DiscoveryEnabled, NodeName: cfg.NodeName, NodeID: cfg.NodeID,
		AdvancedMode: advanced, ModelLifecycle: lifecycle, IdleUnloadMinutes: idleMins,
		KeepRunningInBackground: keepBackground,
		DefaultProfileID:        defaultProfile,
		DefaultExecution:        defaultExec,
		DownloadBehavior:        downloadBehavior,
		ModelStorageLimitGB:     storageLimit,
		SaveChatHistory:         saveChat,
		MemoryEnabled:           memoryEnabled,
		SaveTaskHistory:         saveTask,
		NotifyTaskFinish:        notifyTask,
		NotifyPeerOffline:       notifyPeer,
		CommunityRatings:        communityRatings,
		RatingsPrompts:          ratingsPrompts,
		UpdateCheck:             updateCheck,
		ToolTerminal:            toolTerminal,
		ToolFileWrites:          toolFiles,
		ToolGit:                 toolGit,
		LaunchAtLogin:           launchAtLogin,
		DiscoveryNeedsRestart:   a.discoveryNeedsRestart,
		UILocale:                uiLocale,
		AssistantLanguageMode:   assistantMode,
		AssistantLanguage:       assistantLanguage,
		AutomationDigest:        digest,
		AutomationDigestZone:    digestZone,
	}, nil
}

// localeTag is a BCP 47 language tag: a 2–3 letter language, then optional
// script, region, or variant subtags, such as "en", "es-MX", "zh-Hant-TW",
// or the pseudo-locale "en-XA".
var localeTag = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$`)

// validLocale accepts a language tag, or "" for the system language.
func validLocale(tag string) bool {
	return tag == "" || (len(tag) <= 35 && localeTag.MatchString(tag))
}

func setSettingString(ctx context.Context, repo *repositories.SettingsRepo, key, value string, allowed map[string]bool) error {
	if allowed != nil && !allowed[value] {
		return contracts.Errorf("INVALID_SETTING", map[string]any{"setting": key, "value": value}, "invalid %s value %q", key, value)
	}
	return repo.Set(ctx, key, value)
}

func (a *App) applySettingsPatch(ctx context.Context, patch map[string]any) error {
	if v, ok := patch["memory_enabled"].(bool); ok {
		if err := a.Settings.SetBool(ctx, "memory_enabled", v); err != nil {
			return err
		}
	}
	if v, ok := patch["advanced_mode"].(bool); ok {
		if err := a.Settings.SetBool(ctx, "advanced_mode", v); err != nil {
			return err
		}
	}
	if v, ok := patch["keep_running_in_background"].(bool); ok {
		if err := a.Settings.SetBool(ctx, "keep_running_in_background", v); err != nil {
			return err
		}
	}
	if v, ok := patch["model_lifecycle"].(string); ok && v != "" {
		if v != "automatic" && v != "manual" {
			return contracts.Errorf("INVALID_SETTING", map[string]any{"setting": "model_lifecycle", "value": v}, "model_lifecycle must be automatic or manual")
		}
		if err := a.Settings.Set(ctx, "model_lifecycle", v); err != nil {
			return err
		}
	}
	if v, ok := patch["idle_unload_minutes"].(float64); ok {
		if err := a.Settings.SetInt(ctx, "idle_unload_minutes", int(v)); err != nil {
			return err
		}
	}
	if v, ok := patch["idle_unload_minutes"].(int); ok {
		if err := a.Settings.SetInt(ctx, "idle_unload_minutes", v); err != nil {
			return err
		}
	}
	if v, ok := patch["default_profile_id"].(string); ok {
		if err := a.Settings.Set(ctx, "default_profile_id", v); err != nil {
			return err
		}
	}
	if v, ok := patch["default_execution"].(string); ok && v != "" {
		if err := setSettingString(ctx, a.Settings, "default_execution", v, map[string]bool{
			"automatic": true, "local": true, "ask": true,
		}); err != nil {
			return err
		}
	}
	if v, ok := patch["download_behavior"].(string); ok && v != "" {
		if err := setSettingString(ctx, a.Settings, "download_behavior", v, map[string]bool{
			"ask": true, "automatic": true,
		}); err != nil {
			return err
		}
	}
	if v, ok := patch["model_storage_limit_gb"].(float64); ok {
		if err := a.Settings.SetInt(ctx, "model_storage_limit_gb", int(v)); err != nil {
			return err
		}
	}
	if v, ok := patch["model_storage_limit_gb"].(int); ok {
		if err := a.Settings.SetInt(ctx, "model_storage_limit_gb", v); err != nil {
			return err
		}
	}
	for _, key := range []string{"save_chat_history", "save_task_history", "notify_task_finish", "notify_peer_offline", "launch_at_login", ratings.SettingShow, ratings.SettingAsk, updates.Setting} {
		if v, ok := patch[key].(bool); ok {
			if err := a.Settings.SetBool(ctx, key, v); err != nil {
				return err
			}
		}
	}
	if v, ok := patch["ui_locale"].(string); ok {
		if !validLocale(v) {
			return contracts.Errorf("INVALID_LOCALE", nil, "ui_locale must be a language tag such as en or es-MX, or empty for the system language")
		}
		if err := a.Settings.Set(ctx, "ui_locale", v); err != nil {
			return err
		}
	}
	if v, ok := patch[settingDigest].(string); ok {
		if !validDigestTime(v) {
			return contracts.Errorf("INVALID_SETTING", map[string]any{"setting": settingDigest, "value": v}, "automation_digest must be a time such as 08:00, or empty for none")
		}
		if err := a.Settings.Set(ctx, settingDigest, v); err != nil {
			return err
		}
	}
	if v, ok := patch[settingDigestZone].(string); ok {
		if _, err := time.LoadLocation(v); err != nil || v == "" {
			return contracts.Errorf("INVALID_SETTING", map[string]any{"setting": settingDigestZone, "value": v}, "automation_digest_zone must be an IANA time zone such as America/Juneau")
		}
		if err := a.Settings.Set(ctx, settingDigestZone, v); err != nil {
			return err
		}
	}
	if v, ok := patch["assistant_language_mode"].(string); ok {
		if !replylang.ValidMode(v) {
			return contracts.Errorf("INVALID_SETTING", map[string]any{"setting": "assistant_language_mode", "value": v},
				"assistant_language_mode must be auto, app, or language")
		}
		if err := a.Settings.Set(ctx, "assistant_language_mode", v); err != nil {
			return err
		}
	}
	if v, ok := patch["assistant_language"].(string); ok {
		if !validLocale(v) {
			return contracts.Errorf("INVALID_LOCALE", nil, "assistant_language must be a language tag such as de or pt-BR")
		}
		if err := a.Settings.Set(ctx, "assistant_language", v); err != nil {
			return err
		}
	}
	toolAllowed := map[string]bool{"deny": true, "ask": true, "allow": true, "allow-for-session": true}
	if v, ok := patch["tool_terminal"].(string); ok && v != "" {
		if err := setSettingString(ctx, a.Settings, "tool_terminal", v, toolAllowed); err != nil {
			return err
		}
	}
	if v, ok := patch["tool_file_writes"].(string); ok && v != "" {
		if err := setSettingString(ctx, a.Settings, "tool_file_writes", v, toolAllowed); err != nil {
			return err
		}
	}
	if v, ok := patch["tool_git"].(string); ok && v != "" {
		if err := setSettingString(ctx, a.Settings, "tool_git", v, toolAllowed); err != nil {
			return err
		}
	}

	var discoveryTouched bool
	var discoveryEnabled bool
	lanBefore := a.Config.Get()
	lanTouched := false
	if v, ok := patch["lan_api_enabled"].(bool); ok && v {
		keys, err := a.APIKeys.List(ctx)
		if err != nil {
			return err
		}
		if len(keys) == 0 {
			return auth.ErrAPIKeyRequired
		}
	}
	err := a.Config.Update(func(c *config.Config) {
		if v, ok := patch["node_name"].(string); ok && v != "" {
			c.NodeName = v
		}
		if v, ok := patch["lan_api_enabled"].(bool); ok {
			lanTouched = true
			c.LANAPIEnabled = v
			if v {
				c.APIHost = "0.0.0.0"
			} else {
				c.APIHost = config.DefaultBindLoopback
			}
		}
		if v, ok := patch["discovery_enabled"].(bool); ok {
			discoveryTouched = true
			discoveryEnabled = v
			c.DiscoveryEnabled = v
			if v {
				c.InternalHost = "0.0.0.0"
			} else {
				c.InternalHost = config.DefaultBindLoopback
			}
		}
		if v, ok := patch["web_ui_enabled"].(bool); ok {
			c.WebUIEnabled = v
		}
	})
	if err != nil {
		return err
	}
	// Network access applies now: the API moves to its new address, without
	// a restart (#216). If it can't, the setting goes back.
	if lanTouched && a.API != nil && a.API.Addr() != "" {
		if err := a.API.Rebind(a.Config.Get().APIAddr()); err != nil {
			_ = a.Config.Update(func(c *config.Config) {
				c.LANAPIEnabled, c.APIHost = lanBefore.LANAPIEnabled, lanBefore.APIHost
			})
			return fmt.Errorf("change local network access: %w", err)
		}
		if !discoveryTouched {
			// The discovery record says whether the API answers on the network.
			a.restartAdvertiser()
		}
	}
	if v, ok := patch["node_name"].(string); ok && v != "" {
		if discoveryTouched && a.Nodes != nil {
			a.Nodes.SetLocalName(v) // discovery reloads below
		} else {
			a.renamed(v)
		}
	}
	if discoveryTouched {
		a.reloadDiscovery()
		if discoveryEnabled && a.bifrostBoundLoopback {
			a.discoveryNeedsRestart = true
		} else if !discoveryEnabled {
			a.discoveryNeedsRestart = false
		}
	}
	return nil
}

// enableLANForPhone turns on local network access for Connect a phone
// (#216), without the key the setting otherwise needs first: the phone's
// key comes from pairing.
func (a *App) enableLANForPhone(ctx context.Context) error {
	before := a.Config.Get()
	if config.ListensBeyondLoopback(before.APIHost) {
		return nil
	}
	if err := a.Config.Update(func(c *config.Config) { c.LANAPIEnabled, c.APIHost = true, "0.0.0.0" }); err != nil {
		return err
	}
	if a.API != nil && a.API.Addr() != "" {
		if err := a.API.Rebind(a.Config.Get().APIAddr()); err != nil {
			_ = a.Config.Update(func(c *config.Config) { c.LANAPIEnabled, c.APIHost = before.LANAPIEnabled, before.APIHost })
			return fmt.Errorf("turn on local network access: %w", err)
		}
	}
	a.restartAdvertiser()
	return nil
}

func (a *App) reloadDiscovery() {
	_ = a.syncInternalBind()
	a.restartAdvertiser()
	if a.Nodes != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			_ = a.Nodes.RefreshDiscovery(ctx)
		}()
	}
}

// startAdvertise announces this computer on mDNS; tests replace it.
var startAdvertise = discovery.StartAdvertise

// restartAdvertiser announces this computer on mDNS again from the current
// config, such as after a rename, or stops when discovery is off.
func (a *App) restartAdvertiser() {
	a.discoveryMu.Lock()
	defer a.discoveryMu.Unlock()
	if a.advertiser != nil {
		a.advertiser.Stop()
		a.advertiser = nil
	}
	cfg := a.Config.Get()
	if !cfg.DiscoveryEnabled {
		return
	}
	adv, err := startAdvertise(cfg, true)
	if err != nil {
		a.Logger.Warn("mdns advertise failed after settings change", "error", err)
		return
	}
	a.advertiser = adv
}

// enableBackgroundWhenScheduled turns on keep_running_in_background when a schedule
// exists. The desktop shell quits the daemon on window close unless that flag is set.
func (a *App) enableBackgroundWhenScheduled(ctx context.Context) error {
	if a.Automations == nil || a.Settings == nil {
		return nil
	}
	items, err := a.Automations.List(ctx)
	if err != nil || len(items) == 0 {
		return err
	}
	on, err := a.Settings.GetBool(ctx, "keep_running_in_background", false)
	if err != nil || on {
		return err
	}
	return a.Settings.SetBool(ctx, "keep_running_in_background", true)
}

// ResetApp restores first-run defaults: settings, profiles, chats, tasks, and API keys.
// Downloaded models and runtimes are kept unless deleteModels is true.
func (a *App) ResetApp(ctx context.Context, deleteModels bool) (contracts.SettingsView, error) {
	defaults := config.DefaultConfig()
	if err := a.Settings.SetBool(ctx, "advanced_mode", false); err != nil {
		return contracts.SettingsView{}, err
	}
	_ = a.Settings.SetBool(ctx, "keep_running_in_background", false)
	_ = a.Settings.Set(ctx, "model_lifecycle", "automatic")
	_ = a.Settings.SetInt(ctx, "idle_unload_minutes", 15)
	_ = a.Settings.Set(ctx, "default_profile_id", "")
	_ = a.Settings.Set(ctx, "default_execution", "automatic")
	_ = a.Settings.Set(ctx, "download_behavior", "ask")
	_ = a.Settings.SetInt(ctx, "model_storage_limit_gb", 0)
	_ = a.Settings.SetBool(ctx, "save_chat_history", true)
	_ = a.Settings.SetBool(ctx, "save_task_history", true)
	_ = a.Settings.SetBool(ctx, "notify_task_finish", true)
	_ = a.Settings.SetBool(ctx, "notify_peer_offline", true)
	_ = a.Settings.SetBool(ctx, "launch_at_login", false)
	_ = a.Settings.Set(ctx, "tool_terminal", "ask")
	_ = a.Settings.Set(ctx, "tool_file_writes", "ask")
	_ = a.Settings.Set(ctx, "tool_git", "ask")
	a.discoveryNeedsRestart = false
	if err := a.Config.Update(func(c *config.Config) {
		c.LANAPIEnabled = defaults.LANAPIEnabled
		c.WebUIEnabled = defaults.WebUIEnabled
		c.DiscoveryEnabled = defaults.DiscoveryEnabled
		c.APIHost = defaults.APIHost
		c.InternalHost = defaults.InternalHost
		if c.DiscoveryEnabled {
			c.InternalHost = "0.0.0.0"
		}
		c.NodeName = defaults.NodeName
	}); err != nil {
		return contracts.SettingsView{}, err
	}
	a.reloadDiscovery()
	if err := a.Conversations.DeleteAll(ctx); err != nil {
		return contracts.SettingsView{}, fmt.Errorf("clear conversations: %w", err)
	}
	if a.Artifacts != nil {
		if err := a.Artifacts.DeleteAll(ctx); err != nil {
			return contracts.SettingsView{}, fmt.Errorf("clear files: %w", err)
		}
	}
	if a.Metrics != nil {
		if err := a.Metrics.DeleteAll(ctx); err != nil {
			return contracts.SettingsView{}, fmt.Errorf("clear metrics: %w", err)
		}
	}
	if a.DB != nil {
		if _, err := a.DB.SQL.ExecContext(ctx, `DELETE FROM task_steps`); err != nil {
			return contracts.SettingsView{}, fmt.Errorf("clear task steps: %w", err)
		}
		if _, err := a.DB.SQL.ExecContext(ctx, `DELETE FROM tasks`); err != nil {
			return contracts.SettingsView{}, fmt.Errorf("clear tasks: %w", err)
		}
	}
	if err := a.Profiles.ResetToDefaults(ctx); err != nil {
		return contracts.SettingsView{}, fmt.Errorf("reset profiles: %w", err)
	}
	if keys, err := a.APIKeys.List(ctx); err == nil {
		for _, key := range keys {
			_ = a.APIKeys.Revoke(ctx, key.ID)
		}
	}
	if deleteModels && a.Models != nil {
		installed, err := a.Models.List(ctx)
		if err == nil {
			for _, m := range installed {
				if m.Installed {
					_ = a.Models.Delete(ctx, m.ID)
				}
			}
		}
	}
	a.Logger.Info("application reset to defaults", "delete_models", deleteModels)
	return a.settingsView(ctx)
}

func (a *App) exportDiagnostics(ctx context.Context, includeConversations bool) (string, error) {
	cfg := a.Config.Get()
	path := diagnostics.DefaultBundlePath(cfg)
	var hwJSON []byte
	if inv, err := a.hw.Detect(ctx); err == nil {
		hwJSON, _ = json.MarshalIndent(inv, "", "  ")
	}
	if err := diagnostics.WriteBundle(path, diagnostics.Options{
		IncludeConversations: includeConversations,
		Config:               cfg,
		HardwareJSON:         hwJSON,
	}); err != nil {
		return "", err
	}
	return path, nil
}

// DataDirHint returns a short path for logs.
func DataDirHint() string {
	return filepath.Clean(config.DefaultDataDir())
}

// updateCheckSupported reports whether this build looks for a newer
// version. Only release builds installed from a download do: the App Store
// edition and the copy the desktop app runs update with their app
// (TOSKAR_UPDATE_CHECK=off), and development builds have nothing to compare.
func updateCheckSupported() bool {
	if !updates.Checks(version.Version) || pyenv.Sandboxed() {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(config.Env("UPDATE_CHECK"))) {
	case "off", "0", "false", "no":
		return false
	}
	return true
}
