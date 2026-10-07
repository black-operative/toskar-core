export interface ErrorBody {
  code: string
  message: string
  details?: Record<string, unknown>
}

export interface APIError {
  error: ErrorBody
}

export interface HealthResponse {
  status: string
  product: string
  version: string
  /** Where the loaded models run; never changes status (#317). */
  acceleration?: 'gpu' | 'partial' | 'cpu' | 'cpu_expected' | 'idle'
}

export interface VersionResponse {
  version: string
  commit: string
  build_date: string
  product: string
  license?: string
  source?: string
}

export interface CPUInfo {
  model: string
  cores: number
  threads?: number
}

export interface MemoryInfo {
  total_bytes: number
  available_bytes?: number
  swap_total_bytes?: number
  swap_used_bytes?: number
}

export interface DiskInfo {
  path: string
  total_bytes: number
  available_bytes: number
}

export interface Accelerator {
  id: string
  vendor: string
  model: string
  kind: string
  dedicated_vram_bytes?: number
  unified_memory_bytes?: number
  backends?: string[]
}

export interface HardwareInventory {
  os: string
  arch: string
  hostname?: string
  cpu: CPUInfo
  memory: MemoryInfo
  disk: DiskInfo
  accelerators: Accelerator[]
  detected_at: string
}

/** How well a model writes a language. A language without one is unknown. */
export interface LanguageCapability {
  language: string
  level: 'limited' | 'fair' | 'good' | 'excellent'
  confidence: 'low' | 'medium' | 'high'
  sources: string[]
}

export interface ModelCapabilities {
  tool_calling: boolean
  vision: boolean
  coding: boolean
}

export interface ModelSource {
  url: string
  sha256?: string
  format?: string
}

export interface Model {
  id: string
  display_name: string
  summary?: string
  family?: string
  variant?: string
  parameters?: string
  size_bytes?: number
  memory_needed_bytes?: number
  context?: number
  capabilities: ModelCapabilities
  source?: ModelSource
  /** How well the model writes each language it has a level for, best first (multilingual spec §13–14). */
  languages?: LanguageCapability[]
  purpose?: string[]
  tags?: string[]
  runtime?: string[]
  recommended_roles?: string[]
  installed: boolean
  installed_on?: { node_id: string; node_name: string }[]
  status?: string
  last_used_at?: string
  fit?: ModelFit
  dynamic?: boolean
  /** Set for a model that helps Yggdrasil instead of chatting. */
  support_role?: 'embedding' | 'reranker' | 'classifier'
}

export type FitLabel = 'excellent' | 'good' | 'tight' | 'heavy' | 'unsupported' | 'too_large'

export interface ModelFit {
  model_id: string
  node_id?: string
  node_name?: string
  label: FitLabel
  expected_memory_bytes: number
  reason?: string
  est_tok_per_sec?: number
  tok_per_sec_measured?: boolean
  runtime_memory_low_bytes?: number
  runtime_memory_high_bytes?: number
  weight_bytes?: number
  quantization?: string
  approximate?: boolean
  context_tokens?: number
  total_memory_bytes?: number
  available_memory_bytes?: number
  memory_kind?: 'unified' | 'system' | ''
  headroom?: number
  install_allowed?: boolean
  runtime_warning?: string
  recommendations?: string[]
  gpu_note?: string
  known_to_run?: boolean
}

export interface CategoryWinner {
  category: string
  label: string
  model_id: string
  /** Community ratings from hardware like this computer's moved this model ahead of the curated first choice. */
  community_chosen?: boolean
}

export interface ModelsFitResponse {
  node_id?: string
  node_name?: string
  memory_bytes: number
  fits: ModelFit[]
  winners: CategoryWinner[]
}

export interface RunningModelView {
  model_id: string
  display_name: string
  instance_id: string
  node_id: string
  node_name: string
  status: string
  memory_bytes?: number
  endpoint?: string
  speed_tok_per_sec?: number
  used_by_profiles?: string[]
  accelerator?: string
  last_used_at?: string
  mode?: 'embedding' | 'reranking'
  acceleration?: Acceleration
}

/** What this computer still needs for Toskar to use its GPU (#317). */
export interface GPUSetup {
  gpu?: string
  problems: GPUProblem[]
}

export interface GPUProblem {
  code:
    | 'vulkan_loader_missing'
    | 'vulkan_driver_missing'
    | 'render_access_denied'
    | 'nvidia_driver_missing'
    | 'windows_driver_missing'
    | 'cpu_build'
  /** A terminal command that fixes it on this computer. */
  command?: string
  url?: string
}

/** A computer's live CPU, memory, and GPU figures (#317). */
export interface LiveFigures {
  node_id: string
  node_name: string
  current: LiveSample
  /** Every few seconds over the last hour (every minute when no model is loaded). */
  recent: LiveSample[]
  /** Per-minute averages over the last day. */
  day: LiveSample[]
}

/** One reading; a figure the computer can't give is absent, never 0. */
export interface LiveSample {
  at: string
  cpu_percent?: number
  memory_used_bytes?: number
  memory_total_bytes?: number
  gpus?: GPUSample[]
}

export interface GPUSample {
  name: string
  busy_percent?: number
  memory_used_bytes?: number
  memory_total_bytes?: number
  temperature_c?: number
  power_watts?: number
}

/** Where a running model runs, from the runtime's own report (#317). */
export interface Acceleration {
  state: 'gpu' | 'partial' | 'cpu' | 'cpu_expected'
  reason?: 'cpu_build' | 'gpu_memory' | 'gpu_unavailable' | 'no_gpu'
  backend: 'metal' | 'vulkan' | 'cuda' | 'rocm' | 'sycl' | 'cpu'
  devices?: string[]
  layers_offloaded: number
  layers_total: number
  gpu_memory_bytes?: number
}

export interface BrowseModel {
  id: string
  display_name: string
  summary?: string
  repo_id: string
  filename: string
  source_url: string
  size_bytes?: number
  parameters?: string
  variant?: string
  downloads?: number
  tags?: string[]
}

export interface InstallFromURLRequest {
  source_url: string
  display_name?: string
  id?: string
  filename?: string
  size_bytes?: number
  parameters?: string
  variant?: string
  tags?: string[]
  node_id?: string
}

export interface ModelRole {
  role: string
  model_id: string
  node_id?: string
  required: boolean
}

export interface ToolPolicy {
  tool_id: string
  policy: 'deny' | 'ask' | 'allow-for-session' | 'allow'
}

export interface ToolRecord {
  id: string
  name: string
  description: string
  capability: string
  source: string
  schema: string
  default_policy: string
  risk: string
  enabled: boolean
  profiles: string[]
  /** The common descriptor (Gungnir §7). */
  version?: number
  input_schema?: Record<string, unknown>
  outputs?: ('text' | 'file' | 'image' | 'audio' | 'video')[]
  /** 1 low risk on this computer, 2 reads outside data, 3 changes things, 4 runs commands or code. */
  level?: 1 | 2 | 3 | 4
  level_name?: string
  execution?: 'local' | 'remote' | 'either'
  requirements?: { network: boolean; filesystem: boolean; credentials: boolean; runtime?: string; gpu?: boolean }
  supports?: { progress: boolean; cancel: boolean }
  timeout_seconds?: number
  provider?: string
  health?: 'ok' | 'off' | 'unavailable'
}

/** An audited tool call (Gungnir §13). */
export interface ToolRun {
  id: string
  at: string
  tool_id: string
  status: 'completed' | 'cached' | 'failed' | 'denied' | 'refused' | 'disabled'
  approval?: 'profile' | 'you' | 'session'
  duration_ms: number
  summary?: string
  error?: string
  source?: string
  conversation_id?: string
  task_id?: string
}

export interface ToolActivityRecord {
  tool_id: string
  status: string
  summary?: string
  duration_ms?: number
  error?: string
  at: string
}

export interface NodePolicy {
  mode: 'automatic' | 'prefer_local' | 'manual'
  preferred_nodes?: string[]
  denied_nodes?: string[]
  remote?: '' | 'off'
}

export interface AIProfile {
  id: string
  name: string
  purpose: string
  orchestrator_id: string
  roles: ModelRole[]
  tools?: ToolPolicy[]
  node_policy: NodePolicy
  knowledge_sources?: string[]
  /** How this profile works through a request (spec §40). Empty keeps defaults. */
  orchestration?: OrchestrationPolicy
}

export interface OrchestrationPolicy {
  strategy?: '' | 'single' | 'planned' | 'team'
  effort?: '' | 'fast' | 'balanced' | 'thorough'
  planning?: '' | 'on' | 'off' | 'always'
  max_workers?: number
  parallel?: '' | 'on' | 'off'
  verification?: '' | 'off' | 'check' | 'correct' | 'thorough'
  max_tool_calls?: number
  memory?: '' | 'off'
  context_share?: number
  fallback?: '' | 'off'
  fallback_models?: string[]
  retries?: number
  timeout_seconds?: number
}

export type NodeStatus = 'online' | 'offline' | 'unknown'

export interface Node {
  /** Training an AI now, so work goes to another computer when it can. */
  training?: boolean
  id: string
  name: string
  os: string
  arch: string
  status: NodeStatus
  is_local: boolean
  paired: boolean
  hardware?: HardwareInventory
  last_seen_at?: string
  address?: string
}

export interface Conversation {
  id: string
  title: string
  profile_id?: string
  model_id?: string
  /** Persistent memory is kept out of this conversation. */
  memory_off?: boolean
  created_at: string
  updated_at: string
}

export interface Citation {
  kind: 'web' | 'knowledge' | 'file' | string
  title: string
  url?: string
  source?: string
  snippet?: string
  /** The chat file a file citation names, for download (contract 1.9). */
  artifact_id?: string
}

export interface ActivityStep {
  kind: string
  text: string
}

export interface MessageMeta {
  sources?: Citation[]
  steps?: ActivityStep[]
  /** Something changed that may affect the answer, such as a smaller model answering. */
  notice?: string
  /** Files attached to a question or produced with an answer. */
  files?: FileRef[]
  /** The run trace behind the answer (spec §35), at /api/v1/runs/{id}. */
  run_id?: string
  /** The client contract the metadata was written in (spec §68); missing means 1.0. */
  contract?: string
  /** An offer to install what the request needed (contract 1.2). */
  setup?: SetupOffer
  /** How full the model's window was for this answer, for the context gauge (contract 1.6). */
  context?: Record<string, unknown>
  /** The backend and device that ran the answer (#317, contract 1.8). */
  backend?: string
  device?: string
  /** An automation the answer drafted, for the person to confirm (#204, contract 1.10). */
  automation?: AutomationDraft
  /** A result an automation posted to the chat it was made from (#204, contract 1.11). */
  automation_run?: AutomationRunRef
}

export interface AutomationRunRef {
  automation_id: string
  run_id: string
  /** The automation's name when it ran. */
  name: string
}

/** An automation a chat drafted; create it with draft_id and conversation_id (#204). */
export interface AutomationDraft {
  id: string
  name: string
  prompt: string
  schedule: AutomationSchedule
  notification: AutomationNotification
  /** The chat's profile, which runs it. */
  profile_id?: string
  /** What was assumed, such as a time the request didn't give. */
  notes?: string[]
}

/** An offer to install a missing ability, then finish the request (Gungnir §29). */
export interface SetupOffer {
  /** The inventory ability, such as image_generation. */
  ability: string
  label: string
  /** What to install: for image_generation, an image model id. */
  option: string
  name: string
  size_bytes: number
  node_id?: string
  node_name?: string
  /** The message to send again once it is set up; empty for a question. */
  request?: string
}

/** A stored file: an attachment or a file the assistant produced. */
export interface FileRef {
  id: string
  name: string
  mime_type: string
  /** document, spreadsheet, pdf, image, code, audio, or other */
  kind: string
  size_bytes: number
  producer: 'user' | 'assistant'
}

export interface Artifact extends FileRef {
  conversation_id?: string
  created_at: string
}

/** Text read aloud: the audio file and its length. */
export interface SpeechResult {
  artifact: Artifact
  seconds: number
}

/** One computer's ability to run one tool (Gungnir §16). */
export interface ToolProvider {
  tool: string
  /** What runs it, such as FLUX.2 [klein] 4B. */
  name?: string
  state: 'healthy' | 'installing' | 'failed' | 'unavailable'
  reason?: string
  /** A GPU does the work. */
  accelerated: boolean
  /** Languages it works in, such as de, when they matter (speech); none means any. */
  languages?: string[]
  /** It tells the language by itself, as Whisper does. */
  auto_detect?: boolean
}

/** A computer's providers for tools that can run on any paired computer. */
export interface NodeToolProviders {
  node_id: string
  name: string
  local: boolean
  online: boolean
  /** Why its providers are unknown, such as offline. */
  note?: string
  providers: ToolProvider[]
}

/** An image model setup can install. */
export interface ImageModel {
  id: string
  name: string
  description: string
  license: string
  memory_bytes: number
  size_bytes: number
  /** It can change an image from an instruction, or, for a video model, animate one. */
  edits: boolean
  kind: 'image' | 'video'
  installed: boolean
  recommended: boolean
}

/** What image generation has installed, and any setup in progress. */
export interface ImageSetup {
  /** False where stable-diffusion.cpp has no build or cannot run; unsupported says why. */
  supported: boolean
  unsupported?: string
  /** Images can be made now. */
  ready: boolean
  /** stable-diffusion.cpp is installed. */
  program: boolean
  release: string
  /** The model images are made with. */
  active?: string
  models: ImageModel[]
  job?: {
    model_id: string
    stage: 'program' | 'model'
    done_bytes: number
    total_bytes: number
    running: boolean
    error?: string
  }
}

export interface Message {
  id: string
  conversation_id: string
  role: string
  content: string
  created_at: string
  /** What an assistant answer drew on and did. */
  meta?: MessageMeta
}

export interface SettingsView {
  data_dir: string
  models_dir: string
  runtimes_dir: string
  logs_dir: string
  api_host: string
  api_port: number
  lan_api_enabled: boolean
  web_ui_enabled: boolean
  discovery_enabled: boolean
  node_name: string
  node_id: string
  advanced_mode: boolean
  model_lifecycle: 'automatic' | 'manual'
  idle_unload_minutes: number
  keep_running_in_background: boolean
  default_profile_id?: string
  default_execution?: 'automatic' | 'local' | 'ask'
  download_behavior?: 'ask' | 'automatic'
  model_storage_limit_gb?: number
  save_chat_history?: boolean
  save_task_history?: boolean
  notify_task_finish?: boolean
  notify_peer_offline?: boolean
  /** Show community model ratings; downloads the public summary once a day while models are browsed. */
  community_ratings?: boolean
  /** Ask for a rating after a model has been used a while. */
  ratings_prompts?: boolean
  /** Look at toskar.ai once a day for a newer version. */
  update_check?: boolean
  tool_terminal?: string
  tool_file_writes?: string
  tool_git?: string
  launch_at_login?: boolean
  discovery_needs_restart?: boolean
  /** App language as a BCP 47 tag, or '' to follow each device's system language. */
  ui_locale?: string
  /** auto (the language the person writes in), app (the App language), or language (assistant_language). */
  assistant_language_mode?: AssistantLanguageMode
  /** The language answers are written in with assistant_language_mode language, such as de. */
  assistant_language?: string
  memory_enabled?: boolean
}

export type AssistantLanguageMode = 'auto' | 'app' | 'language'

export interface Recommendation {
  purpose: string
  roles: ModelRole[]
  models: Model[]
  reason: string
  estimated_storage_bytes: number
  estimated_vram_bytes: number
  /** Community ratings moved the main model ahead of the curated first choice. */
  community_chosen?: boolean
}

export type Purpose = 'general' | 'coding' | 'research' | 'custom'

export interface ChatRequest {
  conversation_id: string
  profile_id?: string
  model_id?: string
  message: string
  stream?: boolean
  execution?: 'automatic' | 'local'
  /** Artifact ids from uploadArtifact. */
  attachments?: string[]
  /** How much work the message gets: auto (default), fast, balanced, or thorough. */
  effort?: 'auto' | 'fast' | 'balanced' | 'thorough'
  /** The person's IANA time zone, for the date and time the answer uses; set by the api client. */
  time_zone?: string
}

export interface StopChatResponse {
  /** Whether a turn was running and has been stopped. */
  stopped: boolean
}

export interface ChatResponse {
  content: string
}

export interface CreateConversationRequest {
  title?: string
  profile_id?: string
  model_id?: string
}

export interface UpdateConversationRequest {
  title?: string
  profile_id?: string
  model_id?: string
  memory_off?: boolean
}

export type MemoryCategory = 'identity' | 'preferences' | 'projects' | 'technical' | 'interests' | 'people' | 'other'

export interface MemoryItem {
  id: string
  content: string
  category: MemoryCategory
  source_type: 'explicit' | 'manual' | string
  source_ref?: string
  enabled: boolean
  /** Never sent to a paired computer (spec §63). */
  local_only?: boolean
  /** The language the memory is written in, detected on the computer; found in any language (spec §18). */
  language?: string
  created_at: string
  updated_at: string
}

export interface RuntimeDetection {
  installed: boolean
  version?: string
  path?: string
  message?: string
  /** What the installed build can run on, such as ["cpu", "vulkan"]. */
  backends?: string[]
}

export interface RuntimeInfo {
  id: string
  display_name: string
  detection: RuntimeDetection
  status: string
}

export interface APIKeyRecord {
  id: string
  name: string
  prefix: string
  created_at: string
  last_used_at?: string
  revoked: boolean
  /** What the key may ask of the assistant (spec §62). */
  permissions?: APIKeyPermissions
  /** "device" for a phone's key from Connect a phone (#216). */
  kind?: string
}

/** A code a phone connects with (#216). */
export interface DevicePairing {
  /** The 6 digits; only when the code is made. */
  code?: string
  expires_at: string
  state: '' | 'waiting' | 'connected' | 'expired' | 'cancelled'
  /** The phone that connected with the code. */
  device?: APIKeyRecord
  /** Where a phone reaches this computer, such as 192.168.1.20:7331. */
  address?: string
  /** False while the API answers only on this computer. */
  reachable: boolean
}

export interface APIKeyPermissions {
  memory: 'never' | 'on_request' | 'always'
  knowledge: 'never' | 'on_request' | 'always'
  tools: 'profile' | 'read_only' | 'none'
  placement: boolean
}

export interface CreateAPIKeyResponse {
  key: APIKeyRecord
  secret: string
}

export interface PairNodeRequest {
  node_id: string
}

export interface PairingSession {
  id: string
  local_node_id: string
  remote_node_id: string
  remote_name: string
  remote_address?: string
  code: string
  state: string
  created_at: string
  expires_at: string
  incoming?: boolean
}

export interface YggdrasilEvent {
  id: string
  type: string
  timestamp: string
  task_id?: string
  node_id?: string
  payload?: Record<string, unknown>
  /** The client contract the event is written in (spec §68). */
  contract?: string
}

export interface ModelDownloadProgressPayload {
  model_id: string
  bytes_downloaded: number
  bytes_total: number
  percent: number
}

export interface ChatTokenPayload {
  conversation_id: string
  content: string
}

export interface OrchestrationRolePayload {
  conversation_id?: string
  role: string
  node_id?: string
  node_name?: string
  task_id?: string
}

export interface ToolRequestedPayload {
  request_id: string
  tool_id: string
  args?: Record<string, unknown>
  reason?: string
  conversation_id?: string
  task_id?: string
}

export interface SettingsPatch {
  ui_locale?: string
  community_ratings?: boolean
  update_check?: boolean
  ratings_prompts?: boolean
  assistant_language_mode?: AssistantLanguageMode
  assistant_language?: string
  memory_enabled?: boolean
  node_name?: string
  lan_api_enabled?: boolean
  discovery_enabled?: boolean
  web_ui_enabled?: boolean
  advanced_mode?: boolean
  model_lifecycle?: 'automatic' | 'manual'
  idle_unload_minutes?: number
  keep_running_in_background?: boolean
  default_profile_id?: string
  default_execution?: 'automatic' | 'local' | 'ask'
  download_behavior?: 'ask' | 'automatic'
  model_storage_limit_gb?: number
  save_chat_history?: boolean
  save_task_history?: boolean
  notify_task_finish?: boolean
  notify_peer_offline?: boolean
  tool_terminal?: string
  tool_file_writes?: string
  tool_git?: string
  launch_at_login?: boolean
}

export interface LogEntry {
  name: string
  kind: string
  size_bytes: number
  modified_at: string
  label: string
}

export interface LogContent {
  name: string
  kind: string
  label: string
  content: string
  truncated: boolean
  size_bytes: number
}

export interface DiagnosticsExportResult {
  path: string
  message?: string
}

export interface GenerationRoleStep {
  role: string
  node_id?: string
  node_name?: string
  model_id?: string
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  ttft_ms: number
  prompt_ms: number
  eval_ms: number
  total_ms: number
  prompt_tok_per_sec: number
  eval_tok_per_sec: number
}

export interface GenerationRun {
  id: string
  conversation_id?: string
  conversation_title?: string
  message_id?: string
  profile_id?: string
  profile_name?: string
  model_id: string
  runtime_id: string
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  ttft_ms: number
  prompt_ms: number
  eval_ms: number
  total_ms: number
  prompt_tok_per_sec: number
  eval_tok_per_sec: number
  role_steps?: GenerationRoleStep[]
  cross_machine?: boolean
  node_count?: number
  created_at: string
  /** The backend and device that produced the reply (#317). */
  backend?: string
  device?: string
}

export interface BenchmarkPrompt {
  id: string
  label: string
  text: string
}

export interface BenchmarkWorkload {
  id: string
  name: string
  description: string
  prompts: BenchmarkPrompt[]
}

export interface BenchmarkRequest {
  model_ids: string[]
  workload_ids: string[]
  runs?: number
}

export interface BenchmarkProgress {
  percent: number
  phase: string
  current_model?: string
  current_workload?: string
  current_prompt?: string
  completed_steps: number
  total_steps: number
  message?: string
}

export interface BenchmarkSample {
  model_id: string
  workload_id: string
  prompt_id: string
  run_index: number
  warmup: boolean
  load_ms?: number
  ttft_ms: number
  prompt_ms: number
  eval_ms: number
  total_ms: number
  prompt_tok_per_sec: number
  eval_tok_per_sec: number
  prompt_tokens: number
  completion_tokens: number
  error?: string
  backend?: string
  device?: string
}

export interface BenchmarkModelSummary {
  model_id: string
  workload_id: string
  samples: number
  avg_ttft_ms: number
  avg_prompt_ms: number
  avg_eval_ms: number
  avg_total_ms: number
  avg_prompt_tok_per_sec: number
  avg_eval_tok_per_sec: number
  load_ms?: number
  winner_score: number
}

export interface BenchmarkJob {
  id: string
  status: 'pending' | 'running' | 'completed' | 'failed' | 'cancelled'
  request: BenchmarkRequest
  progress: BenchmarkProgress
  samples: BenchmarkSample[]
  summaries: BenchmarkModelSummary[]
  winners: Record<string, string>
  error?: string
  created_at: string
  started_at?: string
  completed_at?: string
}

export type TaskStatus = 'pending' | 'running' | 'completed' | 'failed' | 'cancelled'

export interface Task {
  id: string
  profile_id: string
  conversation_id?: string
  prompt: string
  status: TaskStatus
  result?: string
  error?: string
  created_at: string
}

export type AutomationScheduleKind = 'once' | 'daily' | 'weekly' | 'monthly' | 'interval' | 'cron'
export type AutomationNotifyMode = 'always' | 'condition' | 'change' | 'failure' | 'none'
export type AutomationConditionKind = 'threshold' | 'available' | 'significant'
export type AutomationThresholdOp = 'below' | 'above'
export type AutomationRunStatus = 'claimed' | 'running' | 'retrying' | 'succeeded' | 'failed'

export interface AutomationSchedule {
  kind: AutomationScheduleKind
  time_zone: string
  at?: string
  hour?: number
  minute?: number
  /** The first of weekdays, for older clients. */
  weekday?: number
  every_seconds?: number
  /** A weekly schedule's days, Sunday is 0 (#204). */
  weekdays?: number[]
  /** The times of day a daily, weekly, or monthly schedule runs at; hour and minute are the first. */
  times?: AutomationClockTime[]
  /** A monthly schedule's day, 1-31; a shorter month runs on its last day. */
  month_day?: number
  /** A cron schedule's five-field expression, in time_zone. */
  cron?: string
}

export interface AutomationClockTime {
  hour: number
  minute: number
}

export interface AutomationCondition {
  kind: AutomationConditionKind
  op?: AutomationThresholdOp
  value?: number
  /** ISO 4217 code of a threshold's value, such as EUR. Omitted means USD. */
  currency?: string
}

export interface AutomationNotification {
  mode: AutomationNotifyMode
  condition?: AutomationCondition
}

export interface Automation {
  id: string
  name: string
  enabled: boolean
  schedule: AutomationSchedule
  prompt: string
  profile_id?: string
  model_id?: string
  tools: string[]
  notification: AutomationNotification
  /** The language results are written in: see AutomationInput. */
  response_language?: string
  /** The chat it was made from, and that chat's draft (#204). */
  conversation_id?: string
  draft_id?: string
  created_at: string
  updated_at: string
  next_run_at?: string
  last_run_at?: string
  consecutive_failures: number
  last_error?: string
  last_status?: AutomationRunStatus
  last_result?: string
}

export interface AutomationRun {
  id: string
  automation_id: string
  occurrence_at: string
  status: AutomationRunStatus
  started_at?: string
  finished_at?: string
  result?: string
  error?: string
  notification_sent: boolean
  model_id?: string
  node_id?: string
  attempt: number
  retry_at?: string
  /** Why it did or didn't notify, as automations:notice.<notify_detail>, with notify_values for its placeholders (#204). */
  notify_detail?: string
  notify_values?: Record<string, unknown>
}

/** An automation read from a request on the computer, to review and save (#204). */
export interface ParsedAutomation {
  name: string
  prompt: string
  schedule: AutomationSchedule
  notification: AutomationNotification
  /** What was assumed, such as a time of day when none was given. */
  notes: string[]
}

/** One page of an automation's runs, newest first (#204). */
export interface AutomationRunsPage {
  runs: AutomationRun[]
  /** Older runs exist; ask again with before set to the last run's id. */
  more: boolean
}

export interface AutomationDetail extends Automation {
  /** The newest runs; history_more says older ones exist. */
  history_more?: boolean
  history: AutomationRun[]
}

export interface AutomationPreview {
  result?: string
  error?: string
  model_id?: string
  node_id?: string
  would_notify: boolean
  reason: string
}

export interface AutomationInput {
  name: string
  prompt: string
  profile_id?: string
  model_id?: string
  schedule: AutomationSchedule
  notification: AutomationNotification
  tools?: string[]
  enabled?: boolean
  /** account (the assistant language setting), app, auto (the request's language), or a language tag. */
  response_language?: string
  /** From a chat's draft: creating the same draft again returns the automation it made (#204). */
  conversation_id?: string
  draft_id?: string
}


// Mimir: connected knowledge.
// How a database or API knowledge source is reached. Credentials are never
// returned.
export interface KnowledgeRemote {
  driver?: 'sqlite' | 'postgres' | 'mysql'
  database?: string
  query?: string
  url?: string
  items?: string
  header_names?: string[]
  refresh_minutes: number
}

export interface KnowledgeRemoteInput {
  driver?: 'sqlite' | 'postgres' | 'mysql'
  database?: string
  connection_string?: string
  query?: string
  url?: string
  items?: string
  headers?: Record<string, string>
  refresh_minutes?: number
}

export type KnowledgeKind = 'path' | 'text' | 'database' | 'api'

export interface KnowledgeSource {
  id: string
  name: string
  kind: KnowledgeKind
  path?: string
  filename?: string
  status: 'ready' | 'failed' | 'indexing'
  error?: string
  chunk_count: number
  // Passages with a vector for semantic search, and the embedding model that
  // made them. Zero until an embedding model is installed.
  embedded_count?: number
  embedding_model?: string
  /** The language most passages are in, detected on the computer (spec §19). */
  language?: string
  /** Every language found, with how many passages are in it, most first. */
  languages?: { language: string; passages: number }[]
  /** Never sent to a paired computer (spec §63). */
  local_only?: boolean
  remote?: KnowledgeRemote
  created_at: string
  updated_at: string
  refreshed_at?: string
}

export interface KnowledgeHit {
  source_id: string
  source_name: string
  title: string
  body: string
  score: number
  match?: 'keyword' | 'semantic' | 'both'
}

// Train Your Own AI.
export type MaterialUse = 'training' | 'knowledge' | 'both'
export type TrainingPreset = 'quick' | 'balanced' | 'quality'
export type TrainingState =
  | 'queued'
  | 'preparing_dataset'
  | 'loading_model'
  | 'training'
  | 'exporting'
  | 'evaluating'
  | 'complete'
  | 'failed'
  | 'cancelled'

export interface TrainingHyper {
  method?: 'lora' | 'qlora'
  epochs?: number
  rank?: number
  scale?: number
  layers?: number
  learning_rate?: number
  batch_size?: number
  max_seq_length?: number
  iters?: number
  grad_checkpoint?: boolean
}

export interface MaterialSignal {
  kind: string
  detail: string
}

export interface MaterialRecommendation {
  use: MaterialUse
  reasons: string[]
  signals?: MaterialSignal[]
  example_count: number
  can_train: boolean
}

export interface ClassifyResult {
  recommendation: MaterialRecommendation
  use: MaterialUse
  warning?: string
  error?: string
}

export interface TrainingMaterial {
  id: string
  ai_id: string
  name: string
  filename?: string
  use: MaterialUse
  recommended: MaterialRecommendation
  warning?: string
  knowledge_source_id?: string
  example_count: number
  created_at: string
}

export interface TrainingMessage {
  role: 'system' | 'user' | 'assistant' | string
  content: string
}

export type ExampleFlag = 'empty' | 'no_answer' | 'duplicate' | 'too_long' | 'short_answer' | 'volatile_facts'

export interface TrainingExample {
  id: string
  ai_id: string
  material_id?: string
  messages: TrainingMessage[]
  flags?: ExampleFlag[]
  excluded: boolean
  created_at: string
}

export interface DatasetStats {
  total: number
  usable: number
  excluded: number
  flagged: Partial<Record<ExampleFlag, number>>
  tokens: number
  p95_tokens: number
  warnings?: string[]
}

export interface NodeTrainingFit {
  node_id: string
  node_name: string
  local: boolean
  backend?: string
  label: 'comfortable' | 'tight' | 'too_large' | 'unsupported'
  eligible: boolean
  reason: string
  hyper: TrainingHyper
  memory_needed_bytes: number
  memory_available_bytes: number
  download_bytes: number
  storage_needed_bytes: number
  storage_available_bytes: number
  duration_sec: number
  notes?: string[]
}

export interface TrainingPlan {
  ready: boolean
  blockers: string[]
  warnings: string[]
  examples: number
  preset: TrainingPreset
  hyper: TrainingHyper
  knowledge_sources: string[]
  fits?: NodeTrainingFit[]
  chosen?: NodeTrainingFit
  next_revision: number
}

export interface TrainingProgress {
  iter?: number
  iters?: number
  epoch?: number
  epochs?: number
  train_loss?: number
  val_loss?: number
  tokens_per_sec?: number
  peak_memory_gb?: number
  download_bytes?: number
  download_total?: number
  remaining_sec?: number
  detail?: string
}

export interface TrainingJob {
  id: string
  ai_id: string
  revision: number
  node_id: string
  node_name?: string
  backend: string
  state: TrainingState
  progress: TrainingProgress
  hyper: TrainingHyper
  error?: string
  created_at: string
  started_at?: string
  finished_at?: string
}

// A revision merged into one standalone GGUF file.
export interface ExportStatus {
  ai_id: string
  revision: number
  state: 'none' | 'exporting' | 'ready' | 'failed'
  filename?: string
  // The file's size when ready, and the estimate while exporting.
  size_bytes?: number
  error?: string
  created_at?: string
  // Not part of the file; another tool needs them as its system prompt.
  instructions?: string
}

export interface TrainingRevision {
  ai_id: string
  revision: number
  job_id: string
  base_model_id: string
  backend: string
  hyper: TrainingHyper
  example_count: number
  final_train_loss?: number
  final_val_loss?: number
  evaluated: boolean
  created_at: string
}

export interface EvalResult {
  prompt: string
  base: string
  specialized: string
  error?: string
}

export interface EvalRun {
  id: string
  ai_id: string
  revision: number
  status: 'running' | 'complete' | 'failed' | 'cancelled'
  results: EvalResult[] | null
  error?: string
  created_at: string
  finished_at?: string
}

export interface EvalPrompt {
  id: string
  prompt: string
}

export interface BaseModelRef {
  id: string
  display_name: string
  parameters: string
  license: string
  license_note?: string
  installed: boolean
}

export interface SpecializedAI {
  id: string
  slug: string
  name: string
  goal: string
  instructions: string
  base_model_id: string
  preset: TrainingPreset
  advanced?: TrainingHyper
  knowledge_sources: string[]
  deployed_revision: number
  example?: boolean
  created_at: string
  updated_at: string
}

export interface SampleFile {
  filename: string
  name: string
  description: string
  content: string
}

export interface SpecializedAIView extends SpecializedAI {
  model_id: string
  materials: TrainingMaterial[]
  dataset: DatasetStats
  revisions: TrainingRevision[]
  jobs: TrainingJob[]
  eval_runs: EvalRun[]
  test_prompts: EvalPrompt[]
  base_model?: BaseModelRef
  deployable_revisions: number[]
}

export interface BaseModelChoice {
  model_id: string
  display_name: string
  parameters: string
  license: string
  license_note?: string
  installed: boolean
  recommended: boolean
  reasons: string[]
  fit: NodeTrainingFit
}

export interface TrainingBackend {
  id: string
  name: string
  supported: boolean
  reason: string
  installed: boolean
}

export interface SpecializedAIPatch {
  name?: string
  goal?: string
  instructions?: string
  base_model_id?: string
  preset?: TrainingPreset
  advanced?: TrainingHyper
  clear_advanced?: boolean
  knowledge_sources?: string[]
}

export type NotificationSeverity = 'info' | 'success' | 'warning' | 'error'

export interface NotificationDelivery {
  /** desktop, or email:<id> / webhook:<id> for a destination. */
  channel: string
  destination_id?: string
  status: 'delivered' | 'failed' | 'suppressed' | 'pending' | 'held' | 'cancelled'
  attempts: number
  delivered_at?: string
  next_attempt_at?: string
  error?: string
}

export type NotificationCategory = 'automation' | 'approval' | 'model' | 'training' | 'health' | 'system'

/** An email or webhook destination (Gjallarhorn §12–13). Passwords and signing secrets are never returned. */
export interface NotificationDestination {
  id: string
  kind: 'email' | 'webhook' | 'ntfy'
  name: string
  enabled: boolean
  email?: { host: string; port: number; username?: string; from: string; to: string[]; tls?: 'starttls' | 'tls' | 'none' }
  webhook?: { url: string }
  /** Push through ntfy, on ntfy.sh or your own server. */
  ntfy?: NtfyConfig
  categories?: NotificationCategory[]
  min_severity?: '' | NotificationSeverity
  /** A daily digest instead of each notice; errors still go out at once. An empty at turns it off. */
  digest?: { at: string; time_zone?: string }
  has_secret: boolean
}

export interface NtfyConfig {
  server: string
  topic: string
  /** full sends the title and text; private sends only "You have a new Yggdrasil notification". */
  content?: 'full' | 'private'
  /** This Yggdrasil's address; tapping a notification opens it there. */
  open_url?: string
}

export interface NotificationDestinationInput {
  kind?: 'email' | 'webhook' | 'ntfy'
  name?: string
  enabled?: boolean
  email?: NotificationDestination['email']
  webhook?: { url: string }
  ntfy?: NtfyConfig
  categories?: NotificationCategory[]
  min_severity?: '' | NotificationSeverity
  /** A daily digest instead of each notice; errors still go out at once. An empty at turns it off. */
  digest?: { at: string; time_zone?: string }
  password?: string
}

export interface QuietHours {
  enabled: boolean
  start: string
  end: string
  time_zone: string
  allow: 'errors' | 'nothing'
}

/** A Gjallarhorn notification kept in the notification center. */
/** A catalog key with the values it needs, or literal text shown as it is. */
export interface LocalizedText {
  key?: string
  params?: Record<string, unknown>
  text?: string
}

/** Text core keeps to show later in the reader's language (multilingual spec §22). */
export interface LocalizedMessage {
  title: LocalizedText
  body?: LocalizedText[]
}

export interface AppNotification {
  id: string
  created_at: string
  source_type: string
  source_id?: string
  category: NotificationCategory
  severity: NotificationSeverity
  /** In English; message, when there is one, is shown in the App language. */
  title: string
  body: string
  message?: LocalizedMessage
  /** App path back to the source, such as /automations?id=…. */
  link?: string
  read_at?: string
  /** How many times the same notice came within ten minutes. */
  repeat_count?: number
  deliveries?: NotificationDelivery[]
}

export interface NotificationList {
  notifications: AppNotification[]
  unread: number
}

export interface ConnectorField {
  key: string
  label: string
  help?: string
  secret: boolean
  optional?: boolean
  placeholder?: string
}

/** A connected service (spec §32). Secret values are never returned; a stored token shows only its last four characters. */
export interface Connector {
  id: string
  name: string
  description: string
  scopes: string
  fields: ConnectorField[]
  connected: boolean
  status?: 'connected' | 'error'
  account?: string
  connected_at?: string
  checked_at?: string
  error?: string
  values?: Record<string, string>
  tools: { id: string; name: string; description: string; risk: string; default_policy: string }[]
}

/** How the person likes answers (spec §38). Style only: it never changes what tools may do. */
export interface PersonalStyle {
  length?: '' | 'brief' | 'balanced' | 'detailed'
  tone?: '' | 'friendly' | 'neutral' | 'direct'
  format?: '' | 'prose' | 'lists'
  units?: '' | 'metric' | 'imperial'
  about_me?: string
  instructions?: string
}

export type EgressKind = 'web_search' | 'web_page' | 'places' | 'paired_computer' | 'external_server' | 'connector' | 'notification' | 'community_ratings' | 'update_check'

/** The OpenAI-compatible server whose models can be chosen for a chat (#111). */
export interface ExternalServerInfo {
  base_url: string
  /** A stored API key, never its value. */
  has_key: boolean
  models: string[]
  /** Why the model list couldn't be read. */
  error?: string
}

/** A one-line join token's record (#40); the token itself is shown once. */
export interface JoinToken {
  id: string
  created_at: string
  expires_at: string
  used_at?: string
  /** The computer that joined with it. */
  used_by?: string
  revoked_at?: string
  status: 'active' | 'used' | 'revoked' | 'expired'
}

/** A new join token and the commands that use it. */
export interface JoinTokenCreated extends JoinToken {
  token: string
  server: string
  fingerprint: string
  /** yggctl join … for a computer with Yggdrasil installed. */
  command: string
  /** Installs Yggdrasil on Linux or macOS, then joins. */
  install_command: string
  /** The same in PowerShell. */
  windows_command: string
}

/** A structured reason a rating may give (#37). */
export type RatingTag =
  | 'great_responses'
  | 'fast'
  | 'slow'
  | 'stable'
  | 'crashed'
  | 'too_much_memory'
  | 'great_for_coding'
  | 'great_for_chat'
  | 'good_tool_use'
  | 'poor_tool_use'

/** This person's rating of a model, and exactly what sharing it would send. */
export interface ModelRating {
  model_id: string
  /** False when the model cannot be compared with others' ratings; it can still be rated here. */
  rateable: boolean
  reason?: string
  /** Absent when not rated. */
  stars?: number
  tags: RatingTag[]
  /** The language the model was used in, such as es (multilingual spec §23); absent when not said. */
  language?: string
  shared: boolean
  shared_at?: string
  updated_at?: string
  /** The shared rating includes how the model runs here. */
  share_observations: boolean
  /** How the model ran here in the last 30 days, exactly as sharing them would send. */
  observations?: RatingObservations
  /** A good time to ask for a rating. */
  ask: boolean
  shares?: {
    destination: string
    model: { id: string; format: string; quantization: string; runtime: string; backend: string }
    hardware: { platform: string; architecture: string; vendor: string; family: string; memory_type: string; memory_bucket_gb: string }
  }
}

/** How a model ran on this computer in the last 30 days. */
export interface RatingObservations {
  tokens_per_second?: number
  ttft_ms?: number
  starts?: number
  start_failures?: number
  crashed?: boolean
  out_of_memory?: boolean
  context_band?: '0-8k' | '8-32k' | '32-128k' | '128k+'
}

/** One group of hardware's ratings of a model. */
export interface RatingStats {
  tier: 'family' | 'class' | 'backend' | 'global'
  cohort?: string
  ratings: number
  average: number
  weighted_score: number
  confidence: 'limited' | 'early' | 'community'
  tags?: Partial<Record<RatingTag, number>>
  /** Ratings that shared how the model runs. */
  observed?: number
  median_tokens_per_second?: number
  median_ttft_ms?: number
  successful_start_rate?: number
  crash_rate?: number
  out_of_memory_rate?: number
}

/** Everyone's ratings of the models here, by local model ID. */
export interface CommunityRatings {
  enabled: boolean
  fetched_at?: string
  generated_at?: string
  source?: string
  error?: string
  models: Record<string, { similar?: RatingStats; overall?: RatingStats; languages?: LanguageRatingStats[] }>
}

/** A model's ratings given for one language, from everyone who runs it the same way: never split by hardware. */
export interface LanguageRatingStats {
  /** A base tag such as es, or zh-Hans or zh-Hant. */
  language: string
  ratings: number
  average: number
  weighted_score: number
  confidence: 'limited' | 'early' | 'community'
}

/** One time data left this computer (spec §63). */
export interface EgressRecord {
  id: string
  at: string
  kind: EgressKind
  destination: string
  detail?: string
  source?: 'chat' | 'api' | 'automation' | 'training' | string
  conversation_id?: string
  task_id?: string
}

export interface PrivacyOverview {
  /** Days run records are kept; 0 keeps them. */
  retention_days: number
  last_30_days: Partial<Record<EgressKind, number>>
}

export interface RunRecordCounts {
  tasks: number
  automation_runs: number
  egress: number
}

/** A traced request (spec §35). */
export interface RunTrace {
  id: string
  /** The client contract the trace is written in (spec §68). */
  contract?: string
  conversation_id?: string
  profile_id?: string
  source?: string
  strategy: string[]
  effort?: string
  status: 'completed' | 'failed' | 'stopped'
  /** The English text; error_code and error_details show it in the App language. */
  error?: string
  error_code?: string
  error_details?: Record<string, unknown>
  started_at: string
  completed_at?: string
  latency_ms?: number
  pipeline_ms?: number
  models: {
    model_id: string
    role?: string
    node?: string
    calls: number
    load_ms?: number
    first_token_ms?: number
    ttft_ms?: number
    prompt_tokens: number
    completion_tokens: number
    cached_tokens: number
    tok_per_sec?: number
    backend?: string
    device?: string
  }[]
  tools: { tool_id: string; calls: number; failures?: number; total_ms: number }[]
  nodes: string[]
  workers?: number
  parallel?: boolean
  verification_passes: number
  verification_issues?: number
  verification_fixed?: number
  retries: number
  context_tokens?: number
  context_limit?: number
  /** Tool calls answered from a cache, by tool (spec §36). */
  cache_hits?: Record<string, number>
}

/** An environment variable or header of a tool source. Secret values show only their last four characters. */
export interface MCPVariable {
  key: string
  value: string
  secret: boolean
}

/** One tool a tool source provides. */
export interface MCPTool {
  id: string
  name: string
  remote_name: string
  description: string
  risk: string
  /** allow or ask; changed is true when the person set it. */
  policy: string
  changed: boolean
  enabled: boolean
}

/** An MCP tool source. Secret values are never returned. */
export interface MCPServer {
  id: string
  name: string
  description?: string
  preset?: string
  where: 'local' | 'remote'
  command?: string
  args?: string[]
  url?: string
  env: MCPVariable[]
  headers: MCPVariable[]
  enabled: boolean
  allow_sampling: boolean
  always_offer: boolean
  keywords: string[]
  status: 'ready' | 'sign_in' | 'error' | 'off'
  running: boolean
  error?: string
  signed_in?: boolean
  /** A program it needs that is not installed, such as Node.js. */
  missing?: string
  server_name?: string
  server_version?: string
  protocol?: string
  instructions?: string
  has_resources: boolean
  has_prompts: boolean
  tools: MCPTool[]
  added_at: string
  checked_at?: string
  last_used?: string
}

export interface MCPField {
  key: string
  label: string
  help?: string
  secret?: boolean
  optional?: boolean
  placeholder?: string
  default?: string
  kind?: '' | 'text' | 'folder' | 'file' | 'folders'
}

/** A gallery entry: a well-known server set up with a few plain questions. */
export interface MCPGalleryEntry {
  id: string
  name: string
  description: string
  category: string
  homepage?: string
  fields: MCPField[]
  setup?: string
  sign_in?: boolean
  remote: boolean
  missing?: string
  /** The id of a source already added from this entry. */
  added?: string
}

/** How to reach a server: a command on this computer, or a web address. */
export interface MCPSpec {
  name: string
  preset?: string
  command?: string
  args?: string[]
  env?: Record<string, string>
  dir?: string
  url?: string
  headers?: Record<string, string>
  client_id?: string
  client_secret?: string
  keywords?: string[]
}

/** A value a pasted server still needs, such as a token left as a placeholder. */
export interface MCPNeed {
  key: string
  label: string
  secret: boolean
}

export interface MCPParsed {
  spec: MCPSpec
  needs: MCPNeed[]
  missing?: string
}

/** A server set up in another app on this computer. Secret values are hidden. */
export interface MCPImportCandidate {
  app: string
  app_name: string
  spec: MCPSpec
  needs?: MCPNeed[]
  missing?: string
  added: boolean
}

export interface MCPAddRequest {
  preset?: string
  spec?: MCPSpec
  import?: { app: string; name: string }
  values?: Record<string, string>
  redirect_base?: string
}

export interface MCPAdded {
  server: MCPServer
  /** Set when the service needs you to sign in: open it in the browser. */
  sign_in_url?: string
}

export interface MCPUpdate {
  name?: string
  enabled?: boolean
  allow_sampling?: boolean
  always_offer?: boolean
  keywords?: string[]
  /** Tool name to allow, ask, or default. */
  policies?: Record<string, string>
}

export interface MCPLogLine {
  at: string
  level: string
  text: string
}

export interface MCPPrompt {
  name: string
  title?: string
  description?: string
  arguments?: { name: string; description?: string; required?: boolean }[]
}

/** How other apps reach Yggdrasil's own MCP server. */
export interface MCPShare {
  url: string
  command: string
  args: string[]
  needs_key: boolean
}

/** Something Yggdrasil can or cannot do right now (spec §37). */
export interface CapabilityAbility {
  id: string
  label: string
  available: boolean
  via?: string[]
  note?: string
}

/** The capability inventory (spec §37). */
export interface CapabilitySnapshot {
  at: string
  models: { id: string; name: string; running: boolean; on: string[]; support_role?: string }[]
  nodes: { id: string; name: string; local: boolean; online: boolean; trainer?: string }[]
  tools: { id: string; name: string; source: string; enabled: boolean }[]
  connectors: { id: string; name: string; connected: boolean }[]
  providers: { id: string; name: string; kind: string; status: string; healthy: boolean }[]
  artifacts: { count: number; bytes: number }
  abilities: CapabilityAbility[]
}

/** A cache and its policy (spec §36). */
export interface CacheInfo {
  name: string
  label: string
  key: string
  ttl: string
  invalidation?: string
  scope: string
  privacy: 'public' | 'personal'
  persistent?: boolean
  entries: number
  hits: number
  misses: number
  evictions: number
  last_cleared?: string
}

/** The daemon's own memory and background tasks at a moment (#231). */
export interface RuntimeSample {
  at: string
  goroutines: number
  heap_bytes: number
  sys_bytes: number
}

export interface RuntimeHistory {
  started_at: string
  interval_seconds: number
  now: RuntimeSample
  samples: RuntimeSample[]
}

/** The latest release toskar.ai names, from the daily update check. */
export interface LatestRelease {
  version: string
  published_at?: string
  prerelease?: boolean
  notes_url?: string
  download_url?: string
}

/** GET /api/v1/updates: whether a newer Toskar is out. */
export interface UpdatesStatus {
  /** This build checks (not the App Store edition, the desktop app's copy, or a development build). */
  supported: boolean
  /** It checks and the update_check setting is on. */
  enabled: boolean
  current: string
  available: boolean
  checked_at?: string
  latest?: LatestRelease
}
