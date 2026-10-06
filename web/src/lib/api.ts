import type {
  RuntimeHistory,
  AIProfile,
  APIKeyPermissions,
  APIKeyRecord,
  BrowseModel,
  ChatRequest,
  ChatResponse,
  Conversation,
  CreateAPIKeyResponse,
  CreateConversationRequest,
  DiagnosticsExportResult,
  GenerationRun,
  BenchmarkJob,
  BenchmarkRequest,
  BenchmarkWorkload,
  HardwareInventory,
  HealthResponse,
  InstallFromURLRequest,
  LogContent,
  LogEntry,
  Message,
  Connector,
  CommunityRatings,
  EgressRecord,
  JoinToken,
  JoinTokenCreated,
  ExternalServerInfo,
  ModelRating,
  RatingTag,
  PrivacyOverview,
  RunTrace,
  CapabilitySnapshot,
  CacheInfo,
  RunRecordCounts,
  MCPAdded,
  MCPAddRequest,
  MCPGalleryEntry,
  MCPImportCandidate,
  MCPLogLine,
  MCPParsed,
  MCPPrompt,
  MCPServer,
  MCPShare,
  MCPSpec,
  MCPUpdate,
  PersonalStyle,
  NotificationDestination,
  NotificationDestinationInput,
  NotificationList,
  QuietHours,
  ToolActivityRecord,
  ToolRecord,
  ToolRun,
  Model,
  ModelsFitResponse,
  Node,
  PairingSession,
  Recommendation,
  RunningModelView,
  RuntimeInfo,
  SettingsPatch,
  SettingsView,
  Task,
  Automation,
  AutomationDetail,
  AutomationInput,
  AutomationPreview,
  AutomationRun,
  UpdateConversationRequest,
  VersionResponse,
  BaseModelChoice,
  ClassifyResult,
  DatasetStats,
  EvalPrompt,
  ExportStatus,
  KnowledgeHit,
  KnowledgeKind,
  KnowledgeRemoteInput,
  KnowledgeSource,
  MaterialUse,
  SpecializedAI,
  SpecializedAIPatch,
  SpecializedAIView,
  TrainingBackend,
  TrainingExample,
  TrainingJob,
  TrainingMaterial,
  TrainingMessage,
  TrainingPlan,
  TrainingPreset,
  SampleFile,
  MemoryCategory,
  MemoryItem,
  Artifact,
  SpeechResult,
  ImageSetup,
  NodeToolProviders,
  FileRef,
  StopChatResponse,
  LiveFigures,
  GPUSetup,
} from '@/types/api'
import type { Upload } from '@/lib/upload'

/** A kind of generated media with its own setup: images or video. */
export type MediaKind = 'images' | 'video'
import { hasEventRelay, onRelayedEvent, saveFromDaemon, signInReturnAddress } from '@/lib/desktopBridge'
import i18n from '@/i18n'
import { formatLocale } from '@/i18n/format'
import { moveStored } from '@/lib/storage'

/** The service's error envelope: a stable code, its English message, and the values the message needs. */
interface ServiceError {
  code?: string
  message?: string
  details?: Record<string, unknown>
}

/**
 * An error from the service. `message` is the text for its stable code in the
 * App language (multilingual spec §10); `serviceMessage` is the service's own
 * English text, for Diagnostics and logs.
 */
export class ApiError extends Error {
  readonly serviceMessage: string

  constructor(
    public readonly status: number,
    serviceMessage: string,
    public readonly code?: string,
    public readonly details?: Record<string, unknown>,
  ) {
    super(errorText(code, serviceMessage, details))
    this.name = 'ApiError'
    this.serviceMessage = serviceMessage
  }
}

/**
 * The text for an error code in the App language, from errors.json, with the
 * values in details; the service's message, as {{detail}}, for a code the
 * catalog does not have or does not say more about.
 */
export function errorText(code: string | undefined, message: string, details?: Record<string, unknown>): string {
  if (!code || !i18n.exists(`errors:${code}`)) return message
  const values: Record<string, unknown> = { detail: message, ...details }
  // A language comes as a tag, such as ja; it is shown by its name in the App language.
  if (typeof values.language === 'string' && /^[A-Za-z]{2,3}(-[A-Za-z0-9]+)*$/.test(values.language)) {
    try {
      values.language = new Intl.DisplayNames([formatLocale()], { type: 'language' }).of(values.language) ?? values.language
    } catch {
      // Keep the tag.
    }
  }
  return i18n.t(`errors:${code}`, values)
}

function strField(raw: Record<string, unknown>, snake: string, pascal: string): string {
  const a = raw[snake]
  const b = raw[pascal]
  if (typeof a === 'string' && a) return a
  if (typeof b === 'string' && b) return b
  if (typeof a === 'string') return a
  if (typeof b === 'string') return b
  return ''
}

/** Accepts snake_case (current) or PascalCase (older daemon) pairing payloads. */
export function normalizePairingSession(raw: Record<string, unknown>): PairingSession {
  return {
    id: strField(raw, 'id', 'ID'),
    local_node_id: strField(raw, 'local_node_id', 'LocalNodeID'),
    remote_node_id: strField(raw, 'remote_node_id', 'RemoteNodeID'),
    remote_name: strField(raw, 'remote_name', 'RemoteName'),
    remote_address: strField(raw, 'remote_address', 'RemoteAddr') || undefined,
    code: strField(raw, 'code', 'Code'),
    state: strField(raw, 'state', 'State'),
    created_at: strField(raw, 'created_at', 'CreatedAt'),
    expires_at: strField(raw, 'expires_at', 'ExpiresAt'),
    incoming: Boolean(raw.incoming ?? raw.Incoming),
  }
}

/**
 * The client contract this app is built for (spec §68). Yggdrasil answers a
 * different major version with 426 and says which side to update.
 */
export const CLIENT_CONTRACT = '1.0'
// The name from before the Toskar rename, which every version of the daemon
// accepts and allows in CORS. Toskar-Client-Contract works only with newer
// ones, and the phone and desktop apps can point this UI at an older
// computer (#237).
export const CLIENT_CONTRACT_HEADER = 'Yggdrasil-Client-Contract'

const storedApiKeyName = 'toskar.apiKey'
const storedApiKeyIdName = 'toskar.apiKeyId'
if (typeof window !== 'undefined') {
  moveStored('yggdrasil.apiKey', storedApiKeyName)
  moveStored('yggdrasil.apiKeyId', storedApiKeyIdName)
}

export function storedApiKey(): string {
  if (typeof window === 'undefined') return ''
  return window.localStorage.getItem(storedApiKeyName) ?? ''
}

export function rememberApiKey(secret: string, id?: string) {
  if (typeof window === 'undefined') return
  window.localStorage.setItem(storedApiKeyName, secret)
  if (id) window.localStorage.setItem(storedApiKeyIdName, id)
}

export function forgetApiKey(id?: string) {
  if (typeof window === 'undefined') return
  if (id && window.localStorage.getItem(storedApiKeyIdName) !== id) return
  window.localStorage.removeItem(storedApiKeyName)
  window.localStorage.removeItem(storedApiKeyIdName)
}

function authHeaders(): Record<string, string> {
  const key = storedApiKey()
  if (!key) return {}
  return { Authorization: `Bearer ${key}` }
}

export function getApiBase(): string {
  if (typeof window === 'undefined') {
    return ''
  }
  const w = window as YggdrasilWindow
  return w.__TOSKAR_API_BASE__ ?? w.__YGGDRASIL_API_BASE__ ?? ''
}

async function parseJson<T>(response: Response): Promise<T | null> {
  const text = await response.text()
  if (!text) {
    return null
  }
  return JSON.parse(text) as T
}

async function request<T>(path: string, init?: RequestInit): Promise<T | null> {
  const url = `${getApiBase()}${path}`

  let response: Response
  try {
    response = await fetch(url, {
      ...init,
      headers: {
        Accept: 'application/json',
        [CLIENT_CONTRACT_HEADER]: CLIENT_CONTRACT,
        ...authHeaders(),
        ...(init?.body ? { 'Content-Type': 'application/json' } : {}),
        ...init?.headers,
      },
    })
  } catch (err) {
    const detail = err instanceof Error ? err.message : 'network error'
    throw new ApiError(
      0,
      `Could not reach the local Toskar service (${detail}). If this is the desktop app, quit and reopen it so the daemon restarts.`,
      'SERVICE_UNREACHABLE',
      { detail },
    )
  }

  if (response.status === 404) {
    return null
  }

  if (!response.ok) {
    const body = await parseJson<{ error?: ServiceError }>(response)
    throw new ApiError(
      response.status,
      body?.error?.message ?? response.statusText ?? `HTTP ${response.status}`,
      body?.error?.code,
      body?.error?.details,
    )
  }

  if (response.status === 204) {
    return null
  }

  return parseJson<T>(response)
}

export async function endpointExists(path: string): Promise<boolean> {
  try {
    const response = await fetch(`${getApiBase()}${path}`, {
      method: 'HEAD',
      headers: authHeaders(),
    })
    return response.ok
  } catch {
    return false
  }
}

export interface StreamChatOptions {
  body: ChatRequest
  signal?: AbortSignal
  onToken: (content: string) => void
  onDone?: () => void
  /** The error's text, and its stable code when the service sent one (error_code, contract 1.3). */
  onError?: (message: string, code?: string) => void
}

/** Save a stored file to the user's computer. It is fetched with the same
 * credentials as other API calls, so it works when the API needs a key. */
export async function downloadArtifact(file: Pick<FileRef, 'id' | 'name'>): Promise<void> {
  // The desktop app's web view can't download; its shell saves the file.
  if ((await saveFromDaemon(file.name, `/api/v1/artifacts/${file.id}/content`)) !== null) return
  const response = await fetch(`${getApiBase()}/api/v1/artifacts/${file.id}/content`, { headers: authHeaders() })
  if (!response.ok) {
    throw response.status === 404
      ? new ApiError(404, 'This file is no longer available.', 'FILE_NOT_FOUND')
      : new ApiError(response.status, response.statusText)
  }
  const url = URL.createObjectURL(await response.blob())
  const link = document.createElement('a')
  link.href = url
  link.download = file.name
  document.body.appendChild(link)
  link.click()
  link.remove()
  setTimeout(() => URL.revokeObjectURL(url), 10_000)
}

/** A stored file's contents as a URL the page can play or show; revoke it when done. */
export async function artifactObjectUrl(id: string): Promise<string> {
  const response = await fetch(`${getApiBase()}/api/v1/artifacts/${id}/content?inline=1`, { headers: authHeaders() })
  if (!response.ok) {
    throw response.status === 404
      ? new ApiError(404, 'This file is no longer available.', 'FILE_NOT_FOUND')
      : new ApiError(response.status, response.statusText)
  }
  return URL.createObjectURL(await response.blob())
}

export async function streamChat({
  body,
  signal,
  onToken,
  onDone,
  onError,
}: StreamChatOptions): Promise<void> {
  // In the desktop app the reply's text comes from the shell's event relay:
  // on Windows the response below arrives only once it ends, so its tokens
  // would show all at once. The response still says when the reply is done.
  const relayed = hasEventRelay() && Boolean(body.conversation_id)
  if (!relayed) return readChatStream({ body, signal, onToken, onDone, onError })
  // The daemon publishes chat.complete after a reply's last token, and the
  // relay keeps their order, so once it arrives every token has.
  let finished = () => {}
  const relayFinished = new Promise<void>((resolve) => {
    finished = resolve
  })
  const stopRelay = onRelayedEvent((json) => {
    let event: { type?: string; payload?: { conversation_id?: string; content?: string } }
    try {
      event = JSON.parse(json)
    } catch {
      return
    }
    if (event.payload?.conversation_id !== body.conversation_id) return
    if (event.type === 'chat.token' && event.payload.content) onToken(event.payload.content)
    if (event.type === 'chat.complete' || event.type === 'chat.stopped') finished()
  })
  let done = false
  try {
    await readChatStream({ body, signal, onToken: () => {}, onDone: () => (done = true), onError })
    if (done) {
      // The response can end before the relay delivers the last tokens.
      await Promise.race([relayFinished, new Promise((resolve) => setTimeout(resolve, relayDrainMs))])
      onDone?.()
    }
  } finally {
    stopRelay()
  }
}

/** The browser's time zone, such as America/Juneau, so answers know the person's date and time. */
export function localTimeZone(): string | undefined {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || undefined
  } catch {
    return undefined
  }
}

/** How long a finished reply waits for the desktop relay to deliver its last tokens. */
const relayDrainMs = 3000

async function readChatStream({
  body,
  signal,
  onToken,
  onDone,
  onError,
}: StreamChatOptions): Promise<void> {
  const url = `${getApiBase()}/api/v1/chat`
  const response = await fetch(url, {
    method: 'POST',
    headers: {
      Accept: 'text/event-stream',
      'Content-Type': 'application/json',
      ...authHeaders(),
    },
    body: JSON.stringify({ time_zone: localTimeZone(), ...body, stream: true }),
    signal,
  })

  if (!response.ok) {
    const errBody = await parseJson<{ error?: ServiceError }>(response)
    throw new ApiError(
      response.status,
      errBody?.error?.message ?? response.statusText,
      errBody?.error?.code,
      errBody?.error?.details,
    )
  }

  const reader = response.body?.getReader()
  if (!reader) {
    throw new ApiError(500, 'Streaming not supported', 'SSE_UNSUPPORTED')
  }

  const decoder = new TextDecoder()
  let buffer = ''
  let currentEvent = 'message'
  // error_code arrives just before error, which carries the text.
  let errorCode: string | undefined

  while (true) {
    const { done, value } = await reader.read()
    if (done) {
      break
    }

    buffer += decoder.decode(value, { stream: true })
    const lines = buffer.split('\n')
    buffer = lines.pop() ?? ''

    for (const line of lines) {
      if (line.startsWith('event:')) {
        currentEvent = line.slice(6).trim()
        continue
      }
      if (!line.startsWith('data:')) {
        continue
      }
      const data = line.slice(5).trim()
      if (currentEvent === 'error_code') {
        try {
          errorCode = (JSON.parse(data) as ServiceError).code
        } catch {
          // an unreadable code: the text in error still says what failed
        }
        continue
      }
      if (currentEvent === 'error') {
        onError?.(data, errorCode)
        return
      }
      if (currentEvent === 'token') {
        try {
          const parsed = JSON.parse(data) as { content?: string }
          if (parsed.content) {
            onToken(parsed.content)
          }
        } catch {
          // ignore malformed token payloads
        }
      }
      if (currentEvent === 'done') {
        onDone?.()
        return
      }
    }
  }

  onDone?.()
}

export const api = {
  getHealth: () => request<HealthResponse>('/api/v1/health'),

  getVersion: () => request<VersionResponse>('/api/v1/version'),

  getHardware: () => request<HardwareInventory>('/api/v1/hardware'),

  getModels: () => request<Model[]>('/api/v1/models'),

  recommendModels: (purpose: string) =>
    request<Recommendation>(`/api/v1/models/recommend?purpose=${encodeURIComponent(purpose)}`),

  getModelsFit: () => request<ModelsFitResponse[]>('/api/v1/models/fit'),

  browseModels: (q = '', limit = 24) => {
    const params = new URLSearchParams()
    if (q) params.set('q', q)
    params.set('limit', String(limit))
    return request<BrowseModel[]>(`/api/v1/models/browse?${params}`)
  },

  listRunningModels: () => request<RunningModelView[]>('/api/v1/models/running'),

  installModel: (id: string, opts?: { wait?: boolean; node_id?: string }) => {
    const params = new URLSearchParams()
    if (opts?.wait) params.set('wait', 'true')
    if (opts?.node_id) params.set('node_id', opts.node_id)
    const qs = params.toString()
    return request<{ status: string; model_id: string }>(
      `/api/v1/models/${id}/install${qs ? `?${qs}` : ''}`,
      {
        method: 'POST',
        body: opts?.node_id ? JSON.stringify({ node_id: opts.node_id }) : undefined,
      },
    )
  },

  installModelFromURL: (body: InstallFromURLRequest) =>
    request<{ status: string; model_id: string }>('/api/v1/models/install-from-url', {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  startModel: (id: string, nodeId?: string) =>
    request<RunningModelView>(`/api/v1/models/${id}/start`, {
      method: 'POST',
      body: JSON.stringify({ node_id: nodeId ?? '' }),
    }),

  stopModel: (id: string, opts?: { instance_id?: string; node_id?: string }) =>
    request<{ ok: boolean }>(`/api/v1/models/${id}/stop`, {
      method: 'POST',
      body: JSON.stringify({
        instance_id: opts?.instance_id ?? '',
        node_id: opts?.node_id ?? '',
      }),
    }),

  deleteModel: (id: string, opts?: { node_id?: string }) => {
    const params = new URLSearchParams()
    if (opts?.node_id) params.set('node_id', opts.node_id)
    const qs = params.toString()
    return request<null>(`/api/v1/models/${id}${qs ? `?${qs}` : ''}`, {
      method: 'DELETE',
      body: opts?.node_id ? JSON.stringify({ node_id: opts.node_id }) : undefined,
    })
  },

  listRuntimes: () => request<RuntimeInfo[]>('/api/v1/runtimes'),

  installRuntime: (id: string) =>
    request<{ status: string; runtime_id: string }>(`/api/v1/runtimes/${id}/install`, {
      method: 'POST',
    }),

  getProfiles: () => request<AIProfile[]>('/api/v1/profiles'),

  createProfile: (profile: Omit<AIProfile, 'id'> & { id?: string }) =>
    request<AIProfile>('/api/v1/profiles', {
      method: 'POST',
      body: JSON.stringify(profile),
    }),

  updateProfile: (id: string, profile: Partial<AIProfile> & { roles?: AIProfile['roles'] }) =>
    request<AIProfile>(`/api/v1/profiles/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(profile),
    }),

  deleteProfile: (id: string) =>
    request<null>(`/api/v1/profiles/${id}`, { method: 'DELETE' }),

  /** Puts a built-in profile back to how Yggdrasil ships it. */
  resetProfile: (id: string) => request<AIProfile>(`/api/v1/profiles/${id}/reset`, { method: 'POST' }),

  listTools: () => request<ToolRecord[]>('/api/v1/tools'),
  listToolRuns: async (toolId?: string) =>
    (await request<ToolRun[]>(`/api/v1/tools/runs?limit=20${toolId ? `&tool_id=${encodeURIComponent(toolId)}` : ''}`)) ?? [],
  toolActivity: () => request<ToolActivityRecord[]>('/api/v1/tools/activity'),

  /** Each computer's providers for tools that can run on any paired computer. */
  toolProviders: () => request<NodeToolProviders[]>('/api/v1/tools/providers'),
  setToolEnabled: (id: string, enabled: boolean) =>
    request<{ id: string; enabled: boolean }>(`/api/v1/tools/${encodeURIComponent(id)}/enabled`, {
      method: 'POST',
      body: JSON.stringify({ enabled }),
    }),
  testTool: (id: string, args: Record<string, unknown>) =>
    request<Record<string, unknown>>(`/api/v1/tools/${encodeURIComponent(id)}/test`, {
      method: 'POST',
      body: JSON.stringify({ args }),
    }),

  decideTool: (body: {
    request_id: string
    allow: boolean
    allow_session?: boolean
  }) =>
    request<{ status: string }>('/api/v1/tools/decide', {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  getNodes: () => request<Node[]>('/api/v1/nodes'),

  refreshNodes: () =>
    request<Node[]>('/api/v1/nodes/refresh', {
      method: 'POST',
      body: '{}',
    }),

  pairNode: async (nodeId: string) => {
    const raw = await request<Record<string, unknown>>('/api/v1/nodes/pair', {
      method: 'POST',
      body: JSON.stringify({ node_id: nodeId }),
    })
    return raw ? normalizePairingSession(raw) : null
  },

  listPairingOffers: async () => {
    const raw = await request<Record<string, unknown>[]>('/api/v1/nodes/pairing/pending')
    return (raw ?? []).map(normalizePairingSession)
  },

  claimPairing: async (nodeId: string, code: string) => {
    const raw = await request<Record<string, unknown>>('/api/v1/nodes/pair/claim', {
      method: 'POST',
      body: JSON.stringify({ node_id: nodeId, code }),
    })
    return raw ? normalizePairingSession(raw) : null
  },

  approvePairing: async (sessionId: string, code?: string) => {
    const raw = await request<Record<string, unknown>>(`/api/v1/nodes/${sessionId}/pair/approve`, {
      method: 'POST',
      body: JSON.stringify({ code: code ?? '' }),
    })
    return raw ? normalizePairingSession(raw) : null
  },

  revokeNode: (id: string) =>
    request<null>(`/api/v1/nodes/${id}/revoke`, { method: 'POST' }),

  listApiKeys: () => request<APIKeyRecord[]>('/api/v1/api-keys'),

  createApiKey: (name: string) =>
    request<CreateAPIKeyResponse>('/api/v1/api-keys', {
      method: 'POST',
      body: JSON.stringify({ name }),
    }),

  deleteApiKey: (id: string) =>
    request<null>(`/api/v1/api-keys/${id}`, { method: 'DELETE' }),

  setApiKeyPermissions: (id: string, permissions: APIKeyPermissions) =>
    request<APIKeyRecord>(`/api/v1/api-keys/${id}/permissions`, { method: 'PUT', body: JSON.stringify(permissions) }),

  rotateApiKey: (id: string) =>
    request<CreateAPIKeyResponse>(`/api/v1/api-keys/${id}/rotate`, {
      method: 'POST',
    }),

  getSettings: () => request<SettingsView>('/api/v1/settings'),

  updateSettings: (patch: SettingsPatch) =>
    request<SettingsView>('/api/v1/settings', {
      method: 'PATCH',
      body: JSON.stringify(patch),
    }),

  resetApp: (opts?: { delete_models?: boolean }) =>
    request<SettingsView>('/api/v1/settings/reset', {
      method: 'POST',
      body: JSON.stringify({ delete_models: Boolean(opts?.delete_models) }),
    }),

  getConversations: () => request<Conversation[]>('/api/v1/conversations'),

  createConversation: (body: CreateConversationRequest) =>
    request<Conversation>('/api/v1/conversations', {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  updateConversation: (id: string, body: UpdateConversationRequest) =>
    request<Conversation>(`/api/v1/conversations/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(body),
    }),

  deleteConversation: (id: string) =>
    request<null>(`/api/v1/conversations/${id}`, {
      method: 'DELETE',
    }),

  getMessages: (conversationId: string) =>
    request<Message[]>(`/api/v1/conversations/${conversationId}/messages`),

  sendChat: (body: ChatRequest) =>
    request<ChatResponse>('/api/v1/chat', {
      method: 'POST',
      body: JSON.stringify({ time_zone: localTimeZone(), ...body, stream: false }),
    }),

  listLogs: () => request<LogEntry[]>('/api/v1/logs'),

  getPerformance: (opts?: { sort?: string; order?: 'asc' | 'desc'; limit?: number }) => {
    const params = new URLSearchParams()
    if (opts?.sort) params.set('sort', opts.sort)
    if (opts?.order) params.set('order', opts.order)
    if (opts?.limit) params.set('limit', String(opts.limit))
    const qs = params.toString()
    return request<GenerationRun[]>(`/api/v1/performance${qs ? `?${qs}` : ''}`)
  },

  listTasks: () => request<Task[]>('/api/v1/tasks'),

  listAutomations: () => request<Automation[]>('/api/v1/automations'),

  getAutomation: (id: string) => request<AutomationDetail>(`/api/v1/automations/${id}`),

  previewAutomation: (body: AutomationInput) =>
    request<AutomationPreview>('/api/v1/automations/preview', {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  createAutomation: (body: AutomationInput) =>
    request<Automation>('/api/v1/automations', {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  updateAutomation: (id: string, body: Partial<AutomationInput>) =>
    request<Automation>(`/api/v1/automations/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(body),
    }),

  deleteAutomation: (id: string) =>
    request<null>(`/api/v1/automations/${id}`, { method: 'DELETE' }),

  runAutomation: (id: string) =>
    request<AutomationRun>(`/api/v1/automations/${id}/run`, { method: 'POST' }),

  pauseAutomation: (id: string) =>
    request<Automation>(`/api/v1/automations/${id}/pause`, { method: 'POST' }),

  resumeAutomation: (id: string) =>
    request<Automation>(`/api/v1/automations/${id}/resume`, { method: 'POST' }),

  listBenchmarkWorkloads: () =>
    request<BenchmarkWorkload[]>('/api/v1/benchmarks/workloads'),

  listBenchmarks: () => request<BenchmarkJob[]>('/api/v1/benchmarks'),

  startBenchmark: (body: BenchmarkRequest) =>
    request<BenchmarkJob>('/api/v1/benchmarks', {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  getBenchmark: (id: string) => request<BenchmarkJob>(`/api/v1/benchmarks/${id}`),

  cancelBenchmark: (id: string) =>
    request<{ ok: boolean }>(`/api/v1/benchmarks/${id}/cancel`, { method: 'POST' }),

  getLog: (name: string, tailBytes = 262144) =>
    request<LogContent>(
      `/api/v1/logs/${encodeURIComponent(name)}?tail_bytes=${tailBytes}`,
    ),

  listMemory: () => request<{ memories: MemoryItem[]; categories: MemoryCategory[] }>('/api/v1/memory'),

  addMemory: (content: string, category?: MemoryCategory) =>
    request<MemoryItem>('/api/v1/memory', { method: 'POST', body: JSON.stringify({ content, category }) }),

  updateMemory: (id: string, body: { content?: string; category?: MemoryCategory; enabled?: boolean; local_only?: boolean }) =>
    request<MemoryItem>(`/api/v1/memory/${id}`, { method: 'PATCH', body: JSON.stringify(body) }),

  deleteMemory: (id: string) => request<null>(`/api/v1/memory/${id}`, { method: 'DELETE' }),

  /** Upload a file to attach to a chat message. */
  uploadArtifact: (upload: Upload, conversationId?: string) =>
    request<Artifact>('/api/v1/artifacts', {
      method: 'POST',
      body: JSON.stringify({
        name: upload.filename,
        text: upload.text,
        content_base64: upload.contentBase64,
        conversation_id: conversationId,
      }),
    }),

  /** Read text aloud on this computer; the audio is kept as a file in the chat. */
  readAloud: (text: string, conversationId?: string) =>
    request<SpeechResult>('/api/v1/speech', {
      method: 'POST',
      body: JSON.stringify({ text, conversation_id: conversationId }),
    }),

  /** What image (images) or video generation has installed, and any setup in progress. */
  getMediaSetup: (kind: MediaKind) => request<ImageSetup>(`/api/v1/${kind}/setup`),

  /** Install stable-diffusion.cpp and a model of that kind in the background. */
  startMediaSetup: (kind: MediaKind, modelId: string) =>
    request<ImageSetup>(`/api/v1/${kind}/setup`, { method: 'POST', body: JSON.stringify({ model_id: modelId }) }),

  /** Stop the setup; what was downloaded is kept. */
  cancelMediaSetup: (kind: MediaKind) => request<ImageSetup>(`/api/v1/${kind}/setup`, { method: 'DELETE' }),

  /** Delete an installed model of that kind. */
  removeMediaModel: (kind: MediaKind, id: string) => request<ImageSetup>(`/api/v1/${kind}/models/${id}`, { method: 'DELETE' }),

  /** What image generation has installed, and any setup in progress. */
  getImageSetup: () => request<ImageSetup>('/api/v1/images/setup'),

  /** Install stable-diffusion.cpp and an image model in the background. */
  startImageSetup: (modelId: string) =>
    request<ImageSetup>('/api/v1/images/setup', { method: 'POST', body: JSON.stringify({ model_id: modelId }) }),

  /** Stop the image setup; what was downloaded is kept. */
  cancelImageSetup: () => request<ImageSetup>('/api/v1/images/setup', { method: 'DELETE' }),

  /** Delete an installed image model. */
  removeImageModel: (id: string) => request<ImageSetup>(`/api/v1/images/models/${id}`, { method: 'DELETE' }),

  /** Stop a conversation's running turn on the computer running it; what was written is kept. */
  stopChat: (conversationId: string) =>
    request<StopChatResponse>('/api/v1/chat/stop', { method: 'POST', body: JSON.stringify({ conversation_id: conversationId }) }),

  getPersonalStyle: async () => (await request<PersonalStyle>('/api/v1/personalization')) ?? {},

  setPersonalStyle: (style: PersonalStyle) =>
    request<PersonalStyle>('/api/v1/personalization', { method: 'PUT', body: JSON.stringify(style) }),

  listCaches: async () => (await request<CacheInfo[]>('/api/v1/caches')) ?? [],

  clearCache: (name: string) => request<null>(`/api/v1/caches/${encodeURIComponent(name)}/clear`, { method: 'POST' }),

  getCapabilities: () => request<CapabilitySnapshot>('/api/v1/capabilities'),

  getRun: (id: string) => request<RunTrace>(`/api/v1/runs/${encodeURIComponent(id)}`),

  listEgress: async (conversationId?: string) =>
    (await request<EgressRecord[]>(`/api/v1/egress${conversationId ? `?conversation_id=${encodeURIComponent(conversationId)}` : ''}`)) ?? [],

  getPrivacy: () => request<PrivacyOverview>('/api/v1/privacy'),

  setRunRetention: (days: number) =>
    request<PrivacyOverview>('/api/v1/privacy', { method: 'PUT', body: JSON.stringify({ retention_days: days }) }),

  /** Makes a one-time join token for adding a computer (#40); the token is in the answer only. */
  createJoinToken: (ttlMinutes?: number) =>
    request<JoinTokenCreated>('/api/v1/join-tokens', { method: 'POST', body: JSON.stringify(ttlMinutes ? { ttl_minutes: ttlMinutes } : {}) }),

  getExternalServer: () => request<ExternalServerInfo>('/api/v1/external-server'),

  setExternalServer: (body: { base_url: string; api_key?: string; clear_key?: boolean }) =>
    request<ExternalServerInfo>('/api/v1/external-server', { method: 'PUT', body: JSON.stringify(body) }),

  listJoinTokens: async () => (await request<JoinToken[]>('/api/v1/join-tokens')) ?? [],

  revokeJoinToken: (id: string) => request<JoinToken>(`/api/v1/join-tokens/${encodeURIComponent(id)}`, { method: 'DELETE' }),

  getModelRating: (id: string) => request<ModelRating>(`/api/v1/models/${encodeURIComponent(id)}/rating`),

  /** Saves a rating; share sends it to the community, false keeps it here and withdraws it if it was shared. */
  putModelRating: (id: string, body: { stars: number; tags: RatingTag[]; share: boolean; observations?: boolean; language?: string }) =>
    request<ModelRating>(`/api/v1/models/${encodeURIComponent(id)}/rating`, { method: 'PUT', body: JSON.stringify(body) }),

  deleteModelRating: (id: string) => request<null>(`/api/v1/models/${encodeURIComponent(id)}/rating`, { method: 'DELETE' }),

  dismissModelRating: (id: string) => request<null>(`/api/v1/models/${encodeURIComponent(id)}/rating/dismiss`, { method: 'POST' }),

  getCommunityRatings: () => request<CommunityRatings>('/api/v1/ratings/community'),

  deleteRunRecords: () => request<RunRecordCounts>('/api/v1/privacy/delete-runs', { method: 'POST' }),

  listMCPServers: async () => (await request<MCPServer[]>('/api/v1/mcp/servers')) ?? [],

  /** Checks the source connects and lists its tools, then stores it. The first start of a local one can take a minute. */
  addMCPServer: async (body: MCPAddRequest) => {
    const added = await request<MCPAdded>('/api/v1/mcp/servers', {
      method: 'POST',
      body: JSON.stringify({ redirect_base: await signInReturnAddress(), ...body }),
    })
    if (!added) throw new ApiError(404, 'That gallery entry or app setting was not found.', 'TOOL_SOURCE_ENTRY_NOT_FOUND')
    return added
  },

  updateMCPServer: (id: string, update: MCPUpdate) =>
    request<MCPServer>(`/api/v1/mcp/servers/${encodeURIComponent(id)}`, { method: 'PATCH', body: JSON.stringify(update) }),

  /** Changes how a source is reached, then checks it. Blank secret values keep the stored ones. */
  replaceMCPServer: (id: string, spec: MCPSpec, values?: Record<string, string>) =>
    request<MCPServer>(`/api/v1/mcp/servers/${encodeURIComponent(id)}`, { method: 'PUT', body: JSON.stringify({ spec, values }) }),

  removeMCPServer: (id: string) => request<null>(`/api/v1/mcp/servers/${encodeURIComponent(id)}`, { method: 'DELETE' }),

  checkMCPServer: (id: string) =>
    request<MCPServer>(`/api/v1/mcp/servers/${encodeURIComponent(id)}/check`, { method: 'POST' }),

  /** Returns the address to open in the browser to sign in. */
  signInMCPServer: async (id: string) =>
    (
      await request<{ url: string }>(`/api/v1/mcp/servers/${encodeURIComponent(id)}/sign-in`, {
        method: 'POST',
        body: JSON.stringify({ redirect_base: await signInReturnAddress() }),
      })
    )?.url ?? '',

  signOutMCPServer: (id: string) =>
    request<MCPServer>(`/api/v1/mcp/servers/${encodeURIComponent(id)}/sign-out`, { method: 'POST' }),

  mcpServerLogs: async (id: string) =>
    (await request<MCPLogLine[]>(`/api/v1/mcp/servers/${encodeURIComponent(id)}/logs`)) ?? [],

  mcpServerPrompts: async (id: string) =>
    (await request<MCPPrompt[]>(`/api/v1/mcp/servers/${encodeURIComponent(id)}/prompts`)) ?? [],

  getMCPPrompt: async (id: string, name: string, args: Record<string, string>) =>
    (
      await request<{ text: string }>(
        `/api/v1/mcp/servers/${encodeURIComponent(id)}/prompts/${encodeURIComponent(name)}`,
        { method: 'POST', body: JSON.stringify({ arguments: args }) },
      )
    )?.text ?? '',

  mcpGallery: async () => (await request<MCPGalleryEntry[]>('/api/v1/mcp/gallery')) ?? [],

  mcpImportCandidates: async () => (await request<MCPImportCandidate[]>('/api/v1/mcp/import')) ?? [],

  /** Reads pasted settings, a web address, or a command line into servers. */
  parseMCP: async (text: string) =>
    (await request<MCPParsed[]>('/api/v1/mcp/parse', { method: 'POST', body: JSON.stringify({ text }) })) ?? [],

  mcpShare: () => request<MCPShare>('/api/v1/mcp/share'),

  listConnectors: async () => (await request<Connector[]>('/api/v1/connectors')) ?? [],

  /** Checks the values with the service, then stores them. A blank secret keeps the stored one. */
  connectService: (id: string, values: Record<string, string>) =>
    request<Connector>(`/api/v1/connectors/${id}`, { method: 'PUT', body: JSON.stringify({ values }) }),

  checkConnector: (id: string) => request<Connector>(`/api/v1/connectors/${id}/check`, { method: 'POST' }),

  disconnectService: (id: string) => request<null>(`/api/v1/connectors/${id}`, { method: 'DELETE' }),

  listNotifications: async (unreadOnly = false) =>
    (await request<NotificationList>(`/api/v1/notifications${unreadOnly ? '?unread=1' : ''}`)) ?? {
      notifications: [],
      unread: 0,
    },

  /** Marks notifications read; no ids marks every one read. */
  markNotificationsRead: (ids: string[] = []) =>
    request<null>('/api/v1/notifications/read', { method: 'POST', body: JSON.stringify({ ids }) }),

  dismissNotification: (id: string) => request<null>(`/api/v1/notifications/${id}/dismiss`, { method: 'POST' }),

  listNotificationDestinations: async () =>
    (await request<NotificationDestination[]>('/api/v1/notifications/destinations')) ?? [],
  createNotificationDestination: (input: NotificationDestinationInput) =>
    request<{ destination: NotificationDestination; secret?: string }>('/api/v1/notifications/destinations', {
      method: 'POST',
      body: JSON.stringify(input),
    }),
  updateNotificationDestination: (id: string, input: NotificationDestinationInput) =>
    request<NotificationDestination>(`/api/v1/notifications/destinations/${id}`, { method: 'PATCH', body: JSON.stringify(input) }),
  deleteNotificationDestination: (id: string) =>
    request<null>(`/api/v1/notifications/destinations/${id}`, { method: 'DELETE' }),
  testNotificationDestination: async (id: string) =>
    (await request<{ ok: boolean; error?: string; permanent?: boolean }>(`/api/v1/notifications/destinations/${id}/test`, {
      method: 'POST',
    })) ?? { ok: false, error: 'No response' },
  rotateNotificationSecret: (id: string) =>
    request<{ secret: string }>(`/api/v1/notifications/destinations/${id}/rotate-secret`, { method: 'POST' }),
  getQuietHours: () => request<QuietHours>('/api/v1/notifications/quiet-hours'),
  setQuietHours: (q: QuietHours) =>
    request<QuietHours>('/api/v1/notifications/quiet-hours', { method: 'PUT', body: JSON.stringify(q) }),

  deleteArtifact: (id: string) => request<null>(`/api/v1/artifacts/${id}`, { method: 'DELETE' }),

  listKnowledge: () => request<KnowledgeSource[]>('/api/v1/knowledge/sources'),

  createKnowledge: (body: {
    name?: string
    kind: KnowledgeKind
    path?: string
    filename?: string
    text?: string
    content_base64?: string
    remote?: KnowledgeRemoteInput
  }) =>
    request<KnowledgeSource>('/api/v1/knowledge/sources', { method: 'POST', body: JSON.stringify(body) }),

  updateKnowledge: (id: string, body: { name?: string; text?: string; local_only?: boolean }) =>
    request<KnowledgeSource>(`/api/v1/knowledge/sources/${id}`, { method: 'PATCH', body: JSON.stringify(body) }),

  knowledgeContent: (id: string) => request<{ text: string }>(`/api/v1/knowledge/sources/${id}/content`),

  refreshKnowledge: (id: string) =>
    request<KnowledgeSource>(`/api/v1/knowledge/sources/${id}/refresh`, { method: 'POST' }),

  deleteKnowledge: (id: string) => request<null>(`/api/v1/knowledge/sources/${id}`, { method: 'DELETE' }),

  searchKnowledge: (query: string, sourceIds?: string[]) =>
    request<KnowledgeHit[]>('/api/v1/knowledge/search', {
      method: 'POST',
      body: JSON.stringify({ query, source_ids: sourceIds }),
    }),

  trainingBackends: () => request<TrainingBackend[]>('/api/v1/training/backends'),

  baseModels: (goal: string) =>
    request<BaseModelChoice[]>(`/api/v1/training/base-models?goal=${encodeURIComponent(goal)}`),

  classifyMaterial: (filename: string, text: string, use?: MaterialUse, contentBase64?: string) =>
    request<ClassifyResult>('/api/v1/training/classify', {
      method: 'POST',
      body: JSON.stringify({ filename, text, use, content_base64: contentBase64 }),
    }),

  listAIs: () => request<SpecializedAI[]>('/api/v1/training/ais'),

  trainingSamples: () => request<SampleFile[]>('/api/v1/training/samples'),

  createExampleAI: () => request<SpecializedAI>('/api/v1/training/example', { method: 'POST' }),

  listDeployedAIs: () => request<Model[]>('/api/v1/training/deployed'),

  createAI: (body: { name: string; goal: string; instructions?: string; base_model_id?: string; preset?: TrainingPreset }) =>
    request<SpecializedAI>('/api/v1/training/ais', { method: 'POST', body: JSON.stringify(body) }),

  getAI: (id: string) => request<SpecializedAIView>(`/api/v1/training/ais/${id}`),

  updateAI: (id: string, body: SpecializedAIPatch) =>
    request<SpecializedAI>(`/api/v1/training/ais/${id}`, { method: 'PATCH', body: JSON.stringify(body) }),

  deleteAI: (id: string) => request<null>(`/api/v1/training/ais/${id}`, { method: 'DELETE' }),

  addMaterial: (id: string, body: { name?: string; filename: string; text: string; use?: MaterialUse; content_base64?: string }) =>
    request<TrainingMaterial>(`/api/v1/training/ais/${id}/materials`, { method: 'POST', body: JSON.stringify(body) }),

  deleteMaterial: (id: string, materialId: string) =>
    request<null>(`/api/v1/training/ais/${id}/materials/${materialId}`, { method: 'DELETE' }),

  addConversations: (id: string, conversationIds: string[]) =>
    request<TrainingMaterial>(`/api/v1/training/ais/${id}/conversations`, {
      method: 'POST',
      body: JSON.stringify({ conversation_ids: conversationIds }),
    }),

  listExamples: (id: string) =>
    request<{ examples: TrainingExample[]; stats: DatasetStats }>(`/api/v1/training/ais/${id}/examples`),

  addExample: (id: string, messages: TrainingMessage[]) =>
    request<null>(`/api/v1/training/ais/${id}/examples`, { method: 'POST', body: JSON.stringify({ messages }) }),

  updateExample: (id: string, exampleId: string, body: { messages?: TrainingMessage[]; excluded?: boolean }) =>
    request<null>(`/api/v1/training/ais/${id}/examples/${exampleId}`, { method: 'PATCH', body: JSON.stringify(body) }),

  deleteExample: (id: string, exampleId: string) =>
    request<null>(`/api/v1/training/ais/${id}/examples/${exampleId}`, { method: 'DELETE' }),

  trainingPlan: (id: string) => request<TrainingPlan>(`/api/v1/training/ais/${id}/plan`),

  startTraining: (id: string, nodeId?: string) =>
    request<TrainingJob>(`/api/v1/training/ais/${id}/train`, {
      method: 'POST',
      ...(nodeId ? { body: JSON.stringify({ node_id: nodeId }) } : {}),
    }),

  cancelTraining: (jobId: string) => request<TrainingJob>(`/api/v1/training/jobs/${jobId}/cancel`, { method: 'POST' }),

  setTestPrompts: (id: string, prompts: string[]) =>
    request<EvalPrompt[]>(`/api/v1/training/ais/${id}/test-prompts`, { method: 'PUT', body: JSON.stringify({ prompts }) }),

  evaluateRevision: (id: string, revision: number) =>
    request<{ status: string }>(`/api/v1/training/ais/${id}/revisions/${revision}/evaluate`, { method: 'POST' }),

  deployRevision: (id: string, revision: number) =>
    request<SpecializedAI>(`/api/v1/training/ais/${id}/revisions/${revision}/deploy`, { method: 'POST' }),

  undeployAI: (id: string) => request<SpecializedAI>(`/api/v1/training/ais/${id}/undeploy`, { method: 'POST' }),

  exportStatus: (id: string, revision: number) =>
    request<ExportStatus>(`/api/v1/training/ais/${id}/revisions/${revision}/export`),

  startExport: (id: string, revision: number) =>
    request<ExportStatus>(`/api/v1/training/ais/${id}/revisions/${revision}/export`, { method: 'POST' }),

  deleteExport: (id: string, revision: number) =>
    request<null>(`/api/v1/training/ais/${id}/revisions/${revision}/export`, { method: 'DELETE' }),

  // The exported file is several GB, so the browser downloads it directly
  // instead of through fetch.
  exportFilePath: (id: string, revision: number) => `/api/v1/training/ais/${id}/revisions/${revision}/export/file`,

  exportFileUrl: (id: string, revision: number) =>
    `${getApiBase()}/api/v1/training/ais/${id}/revisions/${revision}/export/file`,

  getRuntimeHistory: () => request<RuntimeHistory>('/api/v1/diagnostics/runtime'),
  /** Each computer's live CPU, memory, and GPU figures, this one first (#317). */
  getLiveFigures: () => request<LiveFigures[]>('/api/v1/performance/live'),
  /** What this computer still needs for Toskar to use its GPU (#317). */
  getGPUSetup: () => request<GPUSetup>('/api/v1/diagnostics/gpu'),

  exportDiagnostics: (includeConversations = false) =>
    request<DiagnosticsExportResult>('/api/v1/diagnostics', {
      method: 'POST',
      body: JSON.stringify({ include_conversations: includeConversations }),
    }),
}
