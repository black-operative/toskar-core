package contracts

import (
	"strings"
	"time"
)

// ErrorBody is the standard public API error envelope.
type ErrorBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// APIError wraps ErrorBody for JSON responses.
type APIError struct {
	Error ErrorBody `json:"error"`
}

// HealthResponse is returned by GET /api/v1/health.
type HealthResponse struct {
	Status  string `json:"status"`
	Product string `json:"product"`
	Version string `json:"version"`
	// Acceleration sums up where the loaded models run: gpu, partial, cpu,
	// cpu_expected, or idle (#317). It never changes Status: a computer
	// without a GPU is healthy.
	Acceleration string `json:"acceleration,omitempty"`
}

// VersionResponse is returned by GET /api/v1/version.
type VersionResponse struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"build_date"`
	Product   string `json:"product"`
	License   string `json:"license"`
	Source    string `json:"source"`
	// Contract is the client contract this build speaks (§68).
	Contract ContractInfo `json:"contract"`
}

// CPUInfo describes the host CPU.
type CPUInfo struct {
	Model   string `json:"model"`
	Cores   int    `json:"cores"`
	Threads int    `json:"threads,omitempty"`
}

// MemoryInfo describes system memory.
type MemoryInfo struct {
	TotalBytes     uint64 `json:"total_bytes"`
	AvailableBytes uint64 `json:"available_bytes,omitempty"`
	// Swap is a degraded fallback. It is not added to normal capacity.
	SwapTotalBytes uint64 `json:"swap_total_bytes,omitempty"`
	SwapUsedBytes  uint64 `json:"swap_used_bytes,omitempty"`
}

// DiskInfo describes available disk for models/runtimes.
type DiskInfo struct {
	Path           string `json:"path"`
	TotalBytes     uint64 `json:"total_bytes"`
	AvailableBytes uint64 `json:"available_bytes"`
}

// Accelerator describes a GPU or other accelerator.
type Accelerator struct {
	ID            string   `json:"id"`
	Vendor        string   `json:"vendor"`
	Model         string   `json:"model"`
	Kind          string   `json:"kind"`
	DedicatedVRAM uint64   `json:"dedicated_vram_bytes,omitempty"`
	UnifiedMemory uint64   `json:"unified_memory_bytes,omitempty"`
	Backends      []string `json:"backends,omitempty"`
}

// HardwareInventory is the full hardware snapshot for a node.
type HardwareInventory struct {
	OS           string        `json:"os"`
	Arch         string        `json:"arch"`
	Hostname     string        `json:"hostname,omitempty"`
	CPU          CPUInfo       `json:"cpu"`
	Memory       MemoryInfo    `json:"memory"`
	Disk         DiskInfo      `json:"disk"`
	Accelerators []Accelerator `json:"accelerators"`
	DetectedAt   time.Time     `json:"detected_at"`
}

// NodeStatus enumerates node liveness.
type NodeStatus string

const (
	NodeStatusOnline  NodeStatus = "online"
	NodeStatusOffline NodeStatus = "offline"
	NodeStatusUnknown NodeStatus = "unknown"
)

// Node is a cluster member.
type Node struct {
	ID         string             `json:"id"`
	Name       string             `json:"name"`
	OS         string             `json:"os"`
	Arch       string             `json:"arch"`
	Status     NodeStatus         `json:"status"`
	IsLocal    bool               `json:"is_local"`
	Paired     bool               `json:"paired"`
	Hardware   *HardwareInventory `json:"hardware,omitempty"`
	LastSeenAt *time.Time         `json:"last_seen_at,omitempty"`
	Address    string             `json:"address,omitempty"`
	// Training is true while the computer is training an AI, so placement
	// sends work elsewhere when it can (#111).
	Training bool `json:"training,omitempty"`
}

// ModelCapabilities describes model features.
type ModelCapabilities struct {
	ToolCalling     bool   `json:"tool_calling"`
	ToolCallSupport string `json:"tool_call_support,omitempty"` // native | compatible | limited | unsupported
	Vision          bool   `json:"vision"`
	Coding          bool   `json:"coding"`
}

// LanguageCapability is how well a model writes a language (multilingual
// spec §13–14, §31). Levels are coarse on purpose: they don't imply
// precision the sources don't have. A language a model has no level for is
// unknown, and is left out.
type LanguageCapability struct {
	// Language is a BCP 47 tag, such as "de" or "zh-Hans".
	Language string `json:"language"`
	// Level is "limited", "fair", "good", or "excellent".
	Level string `json:"level"`
	// Confidence in the level: "low", "medium", or "high".
	Confidence string `json:"confidence"`
	// Sources the level comes from: "model_card", "maintainer",
	// "benchmark", "provider", "community", or "local_evaluation".
	Sources []string `json:"sources"`
}

// Language capability levels, confidences, and sources.
var (
	LanguageLevels      = []string{"limited", "fair", "good", "excellent"}
	LanguageConfidences = []string{"low", "medium", "high"}
	LanguageSources     = []string{"model_card", "maintainer", "benchmark", "provider", "community", "local_evaluation"}
)

// ModelSource describes where a model artifact comes from.
type ModelSource struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256,omitempty"`
	Format string `json:"format,omitempty"`
}

// Model is a catalog or installed model.
type Model struct {
	ID           string            `json:"id"`
	DisplayName  string            `json:"display_name"`
	Summary      string            `json:"summary,omitempty"`
	Family       string            `json:"family,omitempty"`
	Variant      string            `json:"variant,omitempty"`
	Parameters   string            `json:"parameters,omitempty"` // e.g. "14B"
	SizeBytes    uint64            `json:"size_bytes,omitempty"`
	MemoryNeeded uint64            `json:"memory_needed_bytes,omitempty"`
	Context      int               `json:"context,omitempty"`
	Capabilities ModelCapabilities `json:"capabilities"`
	Source       ModelSource       `json:"source,omitempty"`
	Purpose      []string          `json:"purpose,omitempty"`
	Tags         []string          `json:"tags,omitempty"` // coding, general, reasoning, vision, fast, large
	Runtime      []string          `json:"runtime,omitempty"`
	Roles        []string          `json:"recommended_roles,omitempty"`
	Installed    bool              `json:"installed"`
	InstalledOn  []ModelOnNode     `json:"installed_on,omitempty"`
	Status       string            `json:"status,omitempty"` // available|downloading|installed|loading|running|stopping|error
	LastUsedAt   *time.Time        `json:"last_used_at,omitempty"`
	Fit          *ModelFit         `json:"fit,omitempty"`
	Dynamic      bool              `json:"dynamic,omitempty"` // installed from browse-all / URL
	// SupportRole marks a model that serves Yggdrasil instead of chatting:
	// SupportEmbedding, SupportReranker, or SupportClassifier. Empty for chat models.
	SupportRole string `json:"support_role,omitempty"`
	// Languages are how well the model writes each language it has a level
	// for (§13–14, §31), best first.
	Languages []LanguageCapability `json:"languages,omitempty"`
}

// Supporting model roles (spec §61).
const (
	SupportEmbedding  = "embedding"
	SupportReranker   = "reranker"
	SupportClassifier = "classifier"
)

// SupportRoleOf names a supporting model's job, or "" for a chat model. A
// catalog entry says so; a model installed by URL or from Hugging Face is
// recognized by its name, purpose, or tags.
func SupportRoleOf(m Model) string {
	if m.SupportRole != "" {
		return m.SupportRole
	}
	text := strings.ToLower(m.ID + " " + m.DisplayName + " " + strings.Join(m.Purpose, " ") + " " + strings.Join(m.Tags, " "))
	switch {
	case strings.Contains(text, "rerank"):
		return SupportReranker
	case strings.Contains(text, "embed"):
		return SupportEmbedding
	}
	for _, v := range append(append([]string{}, m.Purpose...), m.Tags...) {
		if strings.EqualFold(v, SupportClassifier) {
			return SupportClassifier
		}
	}
	return ""
}

// ModelOnNode records that a model file is present on a computer.
type ModelOnNode struct {
	NodeID   string `json:"node_id"`
	NodeName string `json:"node_name"`
}

// FitLabel is a hardware suitability grade for a model on a node.
type FitLabel string

const (
	FitExcellent   FitLabel = "excellent"
	FitGood        FitLabel = "good"
	FitTight       FitLabel = "tight"
	FitHeavy       FitLabel = "heavy"
	FitUnsupported FitLabel = "unsupported"
	// FitTooLarge is the old binary rejection. New estimates use heavy or unsupported.
	FitTooLarge FitLabel = "too_large"
)

// ModelFit describes how well a model fits a node's hardware.
type ModelFit struct {
	ModelID             string   `json:"model_id"`
	NodeID              string   `json:"node_id,omitempty"`
	NodeName            string   `json:"node_name,omitempty"`
	Label               FitLabel `json:"label"`
	ExpectedMemoryBytes uint64   `json:"expected_memory_bytes"`
	Reason              string   `json:"reason,omitempty"`
	EstTokPerSec        float64  `json:"est_tok_per_sec,omitempty"`
	TokPerSecMeasured   bool     `json:"tok_per_sec_measured,omitempty"`
	// RuntimeMemoryLowBytes and RuntimeMemoryHighBytes are a conservative range
	// for weights, KV cache, and runtime buffers. They are not the file size alone.
	RuntimeMemoryLowBytes  uint64   `json:"runtime_memory_low_bytes,omitempty"`
	RuntimeMemoryHighBytes uint64   `json:"runtime_memory_high_bytes,omitempty"`
	WeightBytes            uint64   `json:"weight_bytes,omitempty"`
	Quantization           string   `json:"quantization,omitempty"`
	Approximate            bool     `json:"approximate,omitempty"`
	ContextTokens          int      `json:"context_tokens,omitempty"`
	TotalMemoryBytes       uint64   `json:"total_memory_bytes,omitempty"`
	AvailableMemoryBytes   uint64   `json:"available_memory_bytes,omitempty"`
	MemoryKind             string   `json:"memory_kind,omitempty"` // unified | system
	Headroom               float64  `json:"headroom,omitempty"`
	InstallAllowed         bool     `json:"install_allowed"`
	RuntimeWarning         string   `json:"runtime_warning,omitempty"`
	Recommendations        []string `json:"recommendations,omitempty"`
	GPUNote                string   `json:"gpu_note,omitempty"`
	KnownToRun             bool     `json:"known_to_run,omitempty"`
}

// CategoryWinner is a recommendation badge for Discover.
type CategoryWinner struct {
	Category string `json:"category"` // best_overall|fastest|best_quality|lowest_memory|coding|general|reasoning|vision
	Label    string `json:"label"`    // human label e.g. "Best coding"
	ModelID  string `json:"model_id"`
	// CommunityChosen is true when community ratings from hardware like
	// this computer's moved this model ahead of the curated first choice.
	CommunityChosen bool `json:"community_chosen,omitempty"`
}

// ModelsFitResponse is returned by GET /models/fit.
type ModelsFitResponse struct {
	NodeID      string           `json:"node_id,omitempty"`
	NodeName    string           `json:"node_name,omitempty"`
	MemoryBytes uint64           `json:"memory_bytes"`
	Fits        []ModelFit       `json:"fits"`
	Winners     []CategoryWinner `json:"winners"`
}

// RunningModelView is a loaded model instance for the Running tab.
type RunningModelView struct {
	ModelID        string     `json:"model_id"`
	DisplayName    string     `json:"display_name"`
	InstanceID     string     `json:"instance_id"`
	NodeID         string     `json:"node_id"`
	NodeName       string     `json:"node_name"`
	Status         string     `json:"status"`
	MemoryBytes    uint64     `json:"memory_bytes,omitempty"`
	Endpoint       string     `json:"endpoint,omitempty"`
	SpeedTokPerSec float64    `json:"speed_tok_per_sec,omitempty"`
	UsedByProfiles []string   `json:"used_by_profiles,omitempty"`
	Accelerator    string     `json:"accelerator,omitempty"`
	LastUsedAt     *time.Time `json:"last_used_at,omitempty"`
	// Mode is "embedding" or "reranking" for a supporting model serving
	// knowledge search, and empty for a chat model.
	Mode string `json:"mode,omitempty"`
	// Acceleration is where the model runs, from the runtime's own report;
	// absent when that is not known.
	Acceleration *Acceleration `json:"acceleration,omitempty"`
}

// Acceleration states.
const (
	// AccelerationGPU: every layer is on a GPU.
	AccelerationGPU = "gpu"
	// AccelerationPartial: some layers are on a GPU, the rest on the CPU.
	AccelerationPartial = "partial"
	// AccelerationCPU: on the CPU although this computer has a GPU.
	AccelerationCPU = "cpu"
	// AccelerationCPUExpected: on the CPU, and there is no GPU to use.
	AccelerationCPUExpected = "cpu_expected"
	// AccelerationIdle: no model is loaded (health only).
	AccelerationIdle = "idle"
)

// Acceleration reasons, for a state short of "gpu".
const (
	// AccelerationReasonCPUBuild: the CPU-only runtime build is installed
	// while a GPU build would run here.
	AccelerationReasonCPUBuild = "cpu_build"
	// AccelerationReasonGPUMemory: the model doesn't fit in GPU memory.
	AccelerationReasonGPUMemory = "gpu_memory"
	// AccelerationReasonGPUUnavailable: a GPU build found no GPU it could
	// use, such as without a graphics driver or Vulkan.
	AccelerationReasonGPUUnavailable = "gpu_unavailable"
	// AccelerationReasonNoGPU: this computer has no GPU.
	AccelerationReasonNoGPU = "no_gpu"
)

// Acceleration is where a running model runs (#317).
type Acceleration struct {
	// State is gpu, partial, cpu, or cpu_expected (Acceleration* constants).
	State string `json:"state"`
	// Reason says why for a state short of gpu (AccelerationReason*).
	Reason string `json:"reason,omitempty"`
	// Backend is metal, vulkan, cuda, rocm, sycl, or cpu.
	Backend string `json:"backend"`
	// Devices are the GPUs the model is on.
	Devices         []string `json:"devices,omitempty"`
	LayersOffloaded int      `json:"layers_offloaded"`
	LayersTotal     int      `json:"layers_total"`
	// GPUMemoryBytes is what the model takes on its GPUs.
	GPUMemoryBytes uint64 `json:"gpu_memory_bytes,omitempty"`
}

// BrowseModel is a Hugging Face browse-all hit (GGUF filtered).
type BrowseModel struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"display_name"`
	Summary     string   `json:"summary,omitempty"`
	RepoID      string   `json:"repo_id"`
	Filename    string   `json:"filename"`
	SourceURL   string   `json:"source_url"`
	SizeBytes   uint64   `json:"size_bytes,omitempty"`
	Parameters  string   `json:"parameters,omitempty"`
	Variant     string   `json:"variant,omitempty"`
	Downloads   int      `json:"downloads,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

// InstallFromURLRequest installs a model from a direct GGUF URL (browse-all).
type InstallFromURLRequest struct {
	SourceURL   string   `json:"source_url"`
	DisplayName string   `json:"display_name,omitempty"`
	ID          string   `json:"id,omitempty"`
	Filename    string   `json:"filename,omitempty"`
	SizeBytes   uint64   `json:"size_bytes,omitempty"`
	Parameters  string   `json:"parameters,omitempty"`
	Variant     string   `json:"variant,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	NodeID      string   `json:"node_id,omitempty"`
}

// ModelStartRequest starts a loaded instance.
type ModelStartRequest struct {
	NodeID string `json:"node_id,omitempty"`
}

// ModelStopRequest stops a loaded instance.
type ModelStopRequest struct {
	NodeID     string `json:"node_id,omitempty"`
	InstanceID string `json:"instance_id,omitempty"`
}

// ModelRole assignment within a profile.
type ModelRole struct {
	Role     string `json:"role"`
	ModelID  string `json:"model_id"`
	NodeID   string `json:"node_id,omitempty"`
	Required bool   `json:"required"`
}

// ToolPolicy for a profile tool.
type ToolPolicy struct {
	ToolID string `json:"tool_id"`
	Policy string `json:"policy"` // deny | ask | allow-for-session | allow
}

// NodePolicy controls placement preferences (spec §20, Execution).
type NodePolicy struct {
	Mode string `json:"mode"` // automatic | prefer_local | manual
	// PreferredNodes are computers placement favors when they can run the model.
	PreferredNodes []string `json:"preferred_nodes,omitempty"`
	// DeniedNodes are computers this profile never runs on.
	DeniedNodes []string `json:"denied_nodes,omitempty"`
	// Remote is off to keep every turn on this computer.
	Remote string `json:"remote,omitempty"`
}

// AIProfile is the user-facing AI configuration.
type AIProfile struct {
	ID             string       `json:"id"`
	Name           string       `json:"name"`
	Purpose        string       `json:"purpose"`
	OrchestratorID string       `json:"orchestrator_id"`
	Roles          []ModelRole  `json:"roles"`
	Tools          []ToolPolicy `json:"tools,omitempty"`
	NodePolicy     NodePolicy   `json:"node_policy"`
	// KnowledgeSources are Mimir source ids searched on every turn.
	KnowledgeSources []string `json:"knowledge_sources,omitempty"`
	// Orchestration is how this profile works through a request (§40).
	// Empty fields keep Yggdrasil's defaults.
	Orchestration OrchestrationPolicy `json:"orchestration,omitzero"`
	// ModelCallsNoTools is set for a turn whose model cannot call tools,
	// such as Gemma 2: it is not shown the tool protocol, but the look-ups
	// Toskar runs itself (web, places, connected services) still run and
	// reach it as reference material. Never stored or sent.
	ModelCallsNoTools bool `json:"-"`
}

// OrchestrationPolicy is a profile's advanced controls (spec §40, §56).
// Every field is optional; empty keeps the default, which follows the
// effort a chat chooses.
type OrchestrationPolicy struct {
	// Strategy is how a request is worked through: single (one model, no
	// plan), planned (a plan when the request has several parts), or team
	// (a planner splits the request, workers do the parts, and a reviewer
	// checks the answer). Empty is Auto.
	Strategy string `json:"strategy,omitempty"`
	// Effort is the profile's effort when a chat leaves it on Auto:
	// fast, balanced, or thorough.
	Effort string `json:"effort,omitempty"`
	// Planning is on, off, or always: on works through a request with
	// several parts in parts, and always also asks the planner model to
	// split a request that has no obvious parts.
	Planning string `json:"planning,omitempty"`
	// MaxWorkers caps how many parts a plan has (2–8).
	MaxWorkers int `json:"max_workers,omitempty"`
	// Parallel is on or off: whether independent parts are looked up side
	// by side.
	Parallel string `json:"parallel,omitempty"`
	// Verification is off (no checks), check (report only), correct (one
	// correction pass), or thorough (two).
	Verification string `json:"verification,omitempty"`
	// MaxToolCalls caps tool calls in one turn (1–50).
	MaxToolCalls int `json:"max_tool_calls,omitempty"`
	// Memory is off to keep persistent memory out of this profile's chats.
	Memory string `json:"memory,omitempty"`
	// ContextShare is the most of the model's window earlier messages may
	// use, from 0.1 to 0.9.
	ContextShare float64 `json:"context_share,omitempty"`
	// Fallback is off to show a failure instead of quietly answering on
	// another model.
	Fallback string `json:"fallback,omitempty"`
	// FallbackModels are tried in order when the answering model fails,
	// before Yggdrasil picks another installed model.
	FallbackModels []string `json:"fallback_models,omitempty"`
	// Retries is how many times a turn that fails before showing anything
	// is tried again (1–3, default 1): first the same model on another
	// computer, then another model.
	Retries int `json:"retries,omitempty"`
	// TimeoutSeconds stops a turn that runs longer (10–3600).
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
}

// Conversation is a chat thread.
type Conversation struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	ProfileID string `json:"profile_id,omitempty"`
	ModelID   string `json:"model_id,omitempty"`
	// MemoryOff keeps persistent memory out of this conversation.
	MemoryOff bool      `json:"memory_off"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Citation is a source an answer drew on. The product renders citations;
// the model is not asked to invent citation syntax.
type Citation struct {
	// Kind is web, knowledge, or file.
	Kind  string `json:"kind"`
	Title string `json:"title"`
	URL   string `json:"url,omitempty"`
	// Source is the knowledge source name or the file path.
	Source  string `json:"source,omitempty"`
	Snippet string `json:"snippet,omitempty"`
}

// ActivityStep is one thing the assistant did for an answer, in plain language.
type ActivityStep struct {
	// Kind is knowledge, memory, search, read, file, write, create, command,
	// git, route, or recover.
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// MessageMeta is what an assistant answer used and did.
type MessageMeta struct {
	Sources []Citation     `json:"sources,omitempty"`
	Steps   []ActivityStep `json:"steps,omitempty"`
	// Notice tells the user something changed that may affect the answer,
	// such as a smaller model answering after the chosen one failed.
	Notice string `json:"notice,omitempty"`
	// Files are attached to a question or produced with an answer.
	Files []FileRef `json:"files,omitempty"`
	// RunID names the run trace behind the answer (§35), at
	// GET /api/v1/runs/{id}.
	RunID string `json:"run_id,omitempty"`
	// Contract is the client contract the metadata was written in (§68).
	// Metadata saved before the contract existed has none, and reads as 1.0.
	Contract string `json:"contract,omitempty"`
	// Setup offers to install what the request needed (Gungnir §29).
	Setup *SetupOffer `json:"setup,omitempty"`
	// Context is how full the model's window was for this answer, so a
	// client can show the context gauge when it opens the chat again
	// (contract 1.6). Answers saved before then have none.
	Context *ContextUsage `json:"context,omitempty"`
}

// ContextUsage is how a turn used the model's context window, in tokens.
type ContextUsage struct {
	PromptTokens int  `json:"prompt_tokens"`
	Limit        int  `json:"limit"`
	Instructions int  `json:"instructions"`
	Tools        int  `json:"tools"`
	Conversation int  `json:"conversation"`
	ToolResults  int  `json:"tool_results"`
	Estimated    bool `json:"estimated,omitempty"`
	// SummarizedMessages are older messages the model saw as a summary.
	SummarizedMessages int `json:"summarized_messages,omitempty"`
	// MemoryBytes is about how much memory the window reserves, when the
	// model ran on this computer.
	MemoryBytes int64 `json:"memory_bytes,omitempty"`
}

// SetupOffer offers to install a missing ability, such as image generation,
// and to finish the request once it is ready.
type SetupOffer struct {
	// Ability is the inventory ability, such as image_generation.
	Ability string `json:"ability"`
	Label   string `json:"label"`
	// Option is what to install: for image_generation, the model id for
	// POST /api/v1/images/setup.
	Option    string `json:"option"`
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
	NodeID    string `json:"node_id,omitempty"`
	NodeName  string `json:"node_name,omitempty"`
	// Request is the message to send again once it is set up; empty for a
	// question about the ability.
	Request string `json:"request,omitempty"`
}

// FileRef points at a stored file (an artifact). Its bytes are at
// GET /api/v1/artifacts/{id}/content.
type FileRef struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	MimeType string `json:"mime_type"`
	// Kind is document, spreadsheet, pdf, image, code, or other.
	Kind string `json:"kind"`
	Size int64  `json:"size_bytes"`
	// Producer is user for an attachment, assistant for a produced file.
	Producer string `json:"producer"`
}

// Message is a chat message.
type Message struct {
	ID             string       `json:"id"`
	ConversationID string       `json:"conversation_id"`
	Role           string       `json:"role"`
	Content        string       `json:"content"`
	CreatedAt      time.Time    `json:"created_at"`
	Meta           *MessageMeta `json:"meta,omitempty"`
}

// GenerationMetrics is per-turn inference performance.
type GenerationMetrics struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	TTFTMs           float64 `json:"ttft_ms"`
	PromptMs         float64 `json:"prompt_ms"`
	EvalMs           float64 `json:"eval_ms"`
	TotalMs          float64 `json:"total_ms"`
	PromptTokPerSec  float64 `json:"prompt_tok_per_sec"`
	EvalTokPerSec    float64 `json:"eval_tok_per_sec"`
}

// GenerationRoleStep is one orchestrator role's metrics on a specific node.
type GenerationRoleStep struct {
	Role             string  `json:"role"`
	NodeID           string  `json:"node_id,omitempty"`
	NodeName         string  `json:"node_name,omitempty"`
	ModelID          string  `json:"model_id,omitempty"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	TTFTMs           float64 `json:"ttft_ms"`
	PromptMs         float64 `json:"prompt_ms"`
	EvalMs           float64 `json:"eval_ms"`
	TotalMs          float64 `json:"total_ms"`
	PromptTokPerSec  float64 `json:"prompt_tok_per_sec"`
	EvalTokPerSec    float64 `json:"eval_tok_per_sec"`
}

// GenerationRun is a stored performance sample for the Performance tab.
type GenerationRun struct {
	ID                string               `json:"id"`
	ConversationID    string               `json:"conversation_id,omitempty"`
	ConversationTitle string               `json:"conversation_title,omitempty"`
	MessageID         string               `json:"message_id,omitempty"`
	ProfileID         string               `json:"profile_id,omitempty"`
	ProfileName       string               `json:"profile_name,omitempty"`
	ModelID           string               `json:"model_id"`
	RuntimeID         string               `json:"runtime_id"`
	PromptTokens      int                  `json:"prompt_tokens"`
	CompletionTokens  int                  `json:"completion_tokens"`
	TotalTokens       int                  `json:"total_tokens"`
	TTFTMs            float64              `json:"ttft_ms"`
	PromptMs          float64              `json:"prompt_ms"`
	EvalMs            float64              `json:"eval_ms"`
	TotalMs           float64              `json:"total_ms"`
	PromptTokPerSec   float64              `json:"prompt_tok_per_sec"`
	EvalTokPerSec     float64              `json:"eval_tok_per_sec"`
	RoleSteps         []GenerationRoleStep `json:"role_steps,omitempty"`
	CrossMachine      bool                 `json:"cross_machine"`
	NodeCount         int                  `json:"node_count,omitempty"`
	CreatedAt         time.Time            `json:"created_at"`
	// Backend and Device produced the reply, such as vulkan and "AMD Radeon
	// RX 7900 XTX", or cpu and "" (#317); empty when not known, such as for
	// a reply from a paired computer.
	Backend string `json:"backend,omitempty"`
	Device  string `json:"device,omitempty"`
}

// BenchmarkPrompt is one prompt inside a workload category.
type BenchmarkPrompt struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Text  string `json:"text"`
}

// BenchmarkWorkload groups prompts by use-case category.
type BenchmarkWorkload struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Prompts     []BenchmarkPrompt `json:"prompts"`
}

// BenchmarkRequest starts a multi-model comparison.
type BenchmarkRequest struct {
	ModelIDs    []string `json:"model_ids"`
	WorkloadIDs []string `json:"workload_ids"`
	Runs        int      `json:"runs,omitempty"` // measured runs per prompt (default 2)
}

// BenchmarkStatus enumerates job lifecycle.
type BenchmarkStatus string

const (
	BenchmarkPending   BenchmarkStatus = "pending"
	BenchmarkRunning   BenchmarkStatus = "running"
	BenchmarkCompleted BenchmarkStatus = "completed"
	BenchmarkFailed    BenchmarkStatus = "failed"
	BenchmarkCancelled BenchmarkStatus = "cancelled"
)

// BenchmarkProgress is live progress for the UI.
type BenchmarkProgress struct {
	Percent         int    `json:"percent"`
	Phase           string `json:"phase"`
	CurrentModel    string `json:"current_model,omitempty"`
	CurrentWorkload string `json:"current_workload,omitempty"`
	CurrentPrompt   string `json:"current_prompt,omitempty"`
	CompletedSteps  int    `json:"completed_steps"`
	TotalSteps      int    `json:"total_steps"`
	Message         string `json:"message,omitempty"`
}

// BenchmarkSample is one measured generation during a job.
type BenchmarkSample struct {
	ModelID          string  `json:"model_id"`
	WorkloadID       string  `json:"workload_id"`
	PromptID         string  `json:"prompt_id"`
	RunIndex         int     `json:"run_index"`
	Warmup           bool    `json:"warmup"`
	LoadMs           float64 `json:"load_ms,omitempty"`
	TTFTMs           float64 `json:"ttft_ms"`
	PromptMs         float64 `json:"prompt_ms"`
	EvalMs           float64 `json:"eval_ms"`
	TotalMs          float64 `json:"total_ms"`
	PromptTokPerSec  float64 `json:"prompt_tok_per_sec"`
	EvalTokPerSec    float64 `json:"eval_tok_per_sec"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	Error            string  `json:"error,omitempty"`
	// Backend and Device ran the sample (#317).
	Backend string `json:"backend,omitempty"`
	Device  string `json:"device,omitempty"`
}

// BenchmarkModelSummary aggregates measured samples for one model × workload.
type BenchmarkModelSummary struct {
	ModelID            string  `json:"model_id"`
	WorkloadID         string  `json:"workload_id"`
	Samples            int     `json:"samples"`
	AvgTTFTMs          float64 `json:"avg_ttft_ms"`
	AvgPromptMs        float64 `json:"avg_prompt_ms"`
	AvgEvalMs          float64 `json:"avg_eval_ms"`
	AvgTotalMs         float64 `json:"avg_total_ms"`
	AvgPromptTokPerSec float64 `json:"avg_prompt_tok_per_sec"`
	AvgEvalTokPerSec   float64 `json:"avg_eval_tok_per_sec"`
	LoadMs             float64 `json:"load_ms,omitempty"`
	WinnerScore        float64 `json:"winner_score"` // higher = better (eval tok/s weighted with TTFT)
}

// BenchmarkJob is a full comparison run.
type BenchmarkJob struct {
	ID          string                  `json:"id"`
	Status      BenchmarkStatus         `json:"status"`
	Request     BenchmarkRequest        `json:"request"`
	Progress    BenchmarkProgress       `json:"progress"`
	Samples     []BenchmarkSample       `json:"samples"`
	Summaries   []BenchmarkModelSummary `json:"summaries"`
	Winners     map[string]string       `json:"winners"` // workload_id -> model_id
	Error       string                  `json:"error,omitempty"`
	CreatedAt   time.Time               `json:"created_at"`
	StartedAt   *time.Time              `json:"started_at,omitempty"`
	CompletedAt *time.Time              `json:"completed_at,omitempty"`
}

// TaskStatus enumerates task lifecycle.
type TaskStatus string

const (
	TaskPending   TaskStatus = "pending"
	TaskRunning   TaskStatus = "running"
	TaskCompleted TaskStatus = "completed"
	TaskFailed    TaskStatus = "failed"
	TaskCancelled TaskStatus = "cancelled"
)

// Task is an orchestration unit of work.
type Task struct {
	ID             string     `json:"id"`
	ProfileID      string     `json:"profile_id"`
	ConversationID string     `json:"conversation_id,omitempty"`
	Prompt         string     `json:"prompt"`
	Status         TaskStatus `json:"status"`
	Result         string     `json:"result,omitempty"`
	Error          string     `json:"error,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

// SettingsView is the user-visible settings payload.
type SettingsView struct {
	DataDir           string `json:"data_dir"`
	ModelsDir         string `json:"models_dir"`
	RuntimesDir       string `json:"runtimes_dir"`
	LogsDir           string `json:"logs_dir"`
	APIHost           string `json:"api_host"`
	APIPort           int    `json:"api_port"`
	LANAPIEnabled     bool   `json:"lan_api_enabled"`
	WebUIEnabled      bool   `json:"web_ui_enabled"`
	DiscoveryEnabled  bool   `json:"discovery_enabled"`
	NodeName          string `json:"node_name"`
	NodeID            string `json:"node_id"`
	AdvancedMode      bool   `json:"advanced_mode"`
	ModelLifecycle    string `json:"model_lifecycle"`     // automatic | manual
	IdleUnloadMinutes int    `json:"idle_unload_minutes"` // 0 = never
	// KeepRunningInBackground keeps the local daemon (and desktop tray) alive
	// when the main window is closed. Default false. A saved schedule turns it on,
	// because the scheduler stops when the daemon stops.
	KeepRunningInBackground bool `json:"keep_running_in_background"`

	// DefaultProfileID is the preferred Chat profile when starting a new conversation.
	DefaultProfileID string `json:"default_profile_id,omitempty"`
	// DefaultExecution: automatic | local | ask
	DefaultExecution string `json:"default_execution"`
	// DownloadBehavior: ask | automatic
	DownloadBehavior string `json:"download_behavior"`
	// ModelStorageLimitGB is 0 for unlimited.
	ModelStorageLimitGB int  `json:"model_storage_limit_gb"`
	SaveChatHistory     bool `json:"save_chat_history"`
	// MemoryEnabled turns persistent memory on for chats by default.
	MemoryEnabled     bool `json:"memory_enabled"`
	SaveTaskHistory   bool `json:"save_task_history"`
	NotifyTaskFinish  bool `json:"notify_task_finish"`
	NotifyPeerOffline bool `json:"notify_peer_offline"`
	// CommunityRatings shows community model ratings, downloading the
	// public summary once a day while models are browsed (#37).
	CommunityRatings bool `json:"community_ratings"`
	// RatingsPrompts asks for a rating after a model has been used a while.
	RatingsPrompts bool `json:"ratings_prompts"`
	// Tool defaults: deny | ask | allow | allow-for-session
	ToolTerminal   string `json:"tool_terminal"`
	ToolFileWrites string `json:"tool_file_writes"`
	ToolGit        string `json:"tool_git"`
	// LaunchAtLogin starts Yggdrasil when the user signs in (desktop shell).
	LaunchAtLogin bool `json:"launch_at_login"`
	// DiscoveryNeedsRestart is true when enabling discovery requires a process restart
	// to rebind Bifrost to the LAN (listener already started on loopback).
	DiscoveryNeedsRestart bool `json:"discovery_needs_restart,omitempty"`
	// UILocale is the app language as a BCP 47 tag, such as "es-MX", or ""
	// to follow each device's system language (multilingual spec §6–7, §30).
	UILocale string `json:"ui_locale"`
	// AssistantLanguageMode is the language answers are written in (§11,
	// §30): "auto", the language the person writes in (the default); "app",
	// the App language; or "language", AssistantLanguage. A language asked
	// for in a message always wins.
	AssistantLanguageMode string `json:"assistant_language_mode"`
	// AssistantLanguage is the BCP 47 tag answers are written in with
	// AssistantLanguageMode "language", such as "de".
	AssistantLanguage string `json:"assistant_language"`
}

// Recommendation explains a recommended model setup.
type Recommendation struct {
	Purpose      string      `json:"purpose"`
	Roles        []ModelRole `json:"roles"`
	Models       []Model     `json:"models"`
	Reason       string      `json:"reason"`
	StorageBytes uint64      `json:"estimated_storage_bytes"`
	VRAMBytes    uint64      `json:"estimated_vram_bytes"`
	// CommunityChosen is true when community ratings moved the main model
	// ahead of the curated first choice.
	CommunityChosen bool `json:"community_chosen,omitempty"`
}
