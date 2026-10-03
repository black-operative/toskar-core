import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useMemo, useRef, useState, type MouseEvent } from 'react'
import { Trans, useTranslation } from 'react-i18next'
import { Link, useLocation, useSearchParams } from 'react-router-dom'
import i18n from '@/i18n'
import { LoadingSpinner } from '@/components/ui/LoadingSpinner'
import { RealmKicker } from '@/components/ui/Realm'
import { Ratatoskr } from '@/components/ui/Ratatoskr'
import { useMascotState } from '@/lib/ratatoskr/useMascotState'
import { ApiError, api, streamChat } from '@/lib/api'
import { ATTACH_ACCEPT, MAX_ATTACH_BYTES, isAttachable, readUpload } from '@/lib/upload'
import { subscribeEvents } from '@/lib/events'
import { useDialog } from '@/lib/useDialog'
import { useUIStore } from '@/stores/uiStore'
import type {
  AIProfile,
  ChatTokenPayload,
  Conversation,
  Message,
  OrchestrationRolePayload,
  ToolRequestedPayload,
} from '@/types/api'
import { capabilityGap, type CapabilityGap } from '@/features/models/capabilityGap'
import { activeCapabilities, capabilityLabel, setCapability } from '@/features/profiles/capabilities'
import { isTeamProfile } from '@/features/profiles/profilePresentation'
import { canChat, modelToolAssessment } from '@/features/models/modelPresentation'
import { CapabilityNotice } from './CapabilityNotice'
import { ChatActivity } from './ChatActivity'
import { ChatHistoryDrawer, useCanPinChatHistory } from './ChatHistoryDrawer'
import { AnswerDetails } from './AnswerDetails'
import { FileChip, PendingFileChip, type PendingFile } from './FileChips'
import { MemoryToggle } from './MemoryToggle'
import { ReadAloudButton } from './ReadAloud'
import { SetupOfferCard } from './SetupOffer'
import { ChatErrorCard } from './ChatErrorCard'
import { ChatMarkdown } from './ChatMarkdown'
import { ContextUsageButton } from './ContextUsageButton'
import { contextWindow, parseContextUsage, type ContextUsage } from './contextUsage'
import { displayChatText } from './displayChatText'
import { parseModelFailure, type ModelFailure } from './modelFailure'
import { ModelFailureNotice } from './ModelFailureNotice'
import { toolDisplayName } from './toolNames'
import { useChatFollow } from './useChatFollow'
import { RatingDialogHost, RatingPrompt } from '@/features/models/ratings'

type TeamStep = {
  role: string
  nodeId?: string
  nodeName?: string
}

type PendingToolPrompt = {
  requestId: string
  toolId: string
  reason?: string
  argsSummary: string
  rawArgs?: string
}

type RunMode = 'automatic' | 'local'

/** How much work a message gets (spec §15). Auto lets Yggdrasil decide. */
type Effort = 'auto' | 'fast' | 'balanced' | 'thorough'
const EFFORT_KEY = 'ygg.chat.effort'
// The efforts, in order; their names and hints are chat:effort.<id> in the catalog.
const EFFORTS: Effort[] = ['auto', 'fast', 'balanced', 'thorough']

function savedEffort(): Effort {
  try {
    const v = localStorage.getItem(EFFORT_KEY)
    return EFFORTS.some((e) => e === v) ? (v as Effort) : 'auto'
  } catch {
    return 'auto'
  }
}

/** The Model choice that lets Yggdrasil pick an installed model for each message. */
const AUTO_MODEL_ID = 'auto'

// The landing page's suggestions; each label and prompt is chat:landing.suggestions.<id>.
const SUGGESTIONS = ['explain', 'code', 'plan'] as const

const PROFILE_PURPOSE_ORDER = ['general', 'coding', 'research', 'custom'] as const

function sortProfiles(profiles: AIProfile[]): AIProfile[] {
  return [...profiles].sort((a, b) => {
    const ai = PROFILE_PURPOSE_ORDER.indexOf(
      a.purpose as (typeof PROFILE_PURPOSE_ORDER)[number],
    )
    const bi = PROFILE_PURPOSE_ORDER.indexOf(
      b.purpose as (typeof PROFILE_PURPOSE_ORDER)[number],
    )
    const ax = ai === -1 ? 99 : ai
    const bx = bi === -1 ? 99 : bi
    if (ax !== bx) return ax - bx
    return a.name.localeCompare(b.name)
  })
}

function formatRoleLabel(role: string): string {
  if (!role) return i18n.t('chat:roles.role')
  // A plan's worker slots are "worker:1", "worker:2", …
  const worker = /^worker:(\d+)$/.exec(role)
  if (worker) return i18n.t('chat:roles.worker', { n: worker[1] })
  if (role === 'planner') return i18n.t('chat:roles.planner')
  if (role === 'reviewer') return i18n.t('chat:roles.reviewer')
  const slot = /^([a-z]+):(\d+)$/.exec(role)
  if (slot) return `${slot[1].charAt(0).toUpperCase()}${slot[1].slice(1)} ${slot[2]}`
  return role.charAt(0).toUpperCase() + role.slice(1)
}

function hostLabel(value: string): string {
  try {
    return new URL(value).hostname || value
  } catch {
    return value.length > 48 ? `${value.slice(0, 48)}…` : value
  }
}

function friendlyToolDetail(args: Record<string, unknown> | undefined): string {
  if (!args) return ''
  for (const key of ['query', 'url', 'path', 'command', 'message', 'revision']) {
    const value = args[key]
    if (typeof value === 'string' && value.trim()) return value.trim()
  }
  return ''
}

function formatToolArgs(args: Record<string, unknown> | undefined): string {
  if (!args || Object.keys(args).length === 0) {
    return i18n.t('chat:tools.noArguments')
  }
  try {
    const raw = JSON.stringify(args, null, 2)
    return raw.length > 400 ? `${raw.slice(0, 400)}…` : raw
  } catch {
    return i18n.t('chat:tools.argumentsUnavailable')
  }
}

function toolProgress(toolId?: string, summary?: string): string {
  const t = i18n.getFixedT(null, 'chat')
  if (toolId === 'internet.search') return t('status.searchingWeb')
  if (toolId === 'internet.open') return summary ? t('status.readingHost', { host: hostLabel(summary) }) : t('status.readingPage')
  if (toolId === 'filesystem.read') return t('status.readingFile')
  if (toolId === 'filesystem.search') return t('status.searchingFiles')
  if (toolId === 'spreadsheet.analyze') return t('status.readingSpreadsheet')
  if (toolId === 'files.create') return t('status.creatingFile')
  if (toolId === 'code.execute') return t('status.runningCode')
  if (toolId === 'terminal') return t('status.runningCommand')
  if (toolId === 'git.status' || toolId === 'git.diff' || toolId === 'git.log' || toolId === 'git.show') {
    return t('status.checkingGit')
  }
  return toolId ? t('status.usingTool', { tool: toolDisplayName(toolId) }) : t('status.working')
}

export function ChatPage() {
  const { t } = useTranslation('chat')
  const queryClient = useQueryClient()
  const [searchParams, setSearchParams] = useSearchParams()
  const activeProfileId = useUIStore((s) => s.activeProfileId)
  const setActiveProfileId = useUIStore((s) => s.setActiveProfileId)
  const advancedMode = useUIStore((s) => s.advancedMode)
  const chatHistoryPinned = useUIStore((s) => s.chatHistoryPinned)
  const setChatHistoryPinned = useUIStore((s) => s.setChatHistoryPinned)
  const pinnedConversationIds = useUIStore((s) => s.pinnedConversationIds)
  const togglePinnedConversation = useUIStore((s) => s.togglePinnedConversation)
  const canPinHistory = useCanPinChatHistory()

  const [selectedId, setSelectedId] = useState<string | null>(null)
  const location = useLocation()
  // Another page can start a chat with text ready to send, such as a tool
  // source's ready-made prompt.
  const [draft, setDraft] = useState(() => (location.state as { draft?: string } | null)?.draft ?? '')
  const [effort, setEffortState] = useState<Effort>(savedEffort)
  const setEffort = (next: Effort) => {
    setEffortState(next)
    try {
      localStorage.setItem(EFFORT_KEY, next)
    } catch {
      // Remembering the choice is a convenience; the chat works without it.
    }
  }
  // Files added to the composer for the next message.
  const [pendingFiles, setPendingFiles] = useState<PendingFile[]>([])
  const fileInputRef = useRef<HTMLInputElement>(null)
  const [draftProfileId, setDraftProfileId] = useState<string | null>(null)
  const [draftModelId, setDraftModelId] = useState<string | null>(null)
  const [modelChoice, setModelChoice] = useState<{ chatId: string | null; modelId: string } | null>(null)
  // The model Auto (or a fallback) used for the latest turn in a chat.
  const [routedModel, setRoutedModel] = useState<{ chatId: string; modelId: string } | null>(null)
  const [streamingContent, setStreamingContent] = useState<string | null>(null)
  const [isSending, setIsSending] = useState(false)
  const [statusMessage, setStatusMessage] = useState<string | null>(null)
  const [sendError, setSendError] = useState<string | null>(null)
  // The stable code of the error in sendError, kept with its text so a later error never takes it.
  const [sendErrorCode, setSendErrorCode] = useState<{ text: string; code: string } | null>(null)
  // Memory Off chosen before the conversation exists; applied when it is created.
  const [memoryOffDraft, setMemoryOffDraft] = useState(false)
  const [modelFailure, setModelFailure] = useState<ModelFailure | null>(null)
  const [responseInterrupted, setResponseInterrupted] = useState(false)
  const [capabilityNotice, setCapabilityNotice] = useState<CapabilityGap | null>(null)
  const [toolTraces, setToolTraces] = useState<{ label: string; detail: string; status: string }[]>([])
  const [toolDetailsOpen, setToolDetailsOpen] = useState(false)
  const [teamSteps, setTeamSteps] = useState<TeamStep[]>([])
  // The parts of a request Yggdrasil is working through, shown while it runs.
  const [planSteps, setPlanSteps] = useState<{ step: string; status: 'pending' | 'running' | 'done' | 'failed' }[]>([])
  const [pendingTool, setPendingTool] = useState<PendingToolPrompt | null>(null)
  const [toolDeciding, setToolDeciding] = useState(false)
  const [renamingId, setRenamingId] = useState<string | null>(null)
  const [renameValue, setRenameValue] = useState('')
  const [historyOpen, setHistoryOpen] = useState(false)
  const [pendingDelete, setPendingDelete] = useState<Conversation | null>(null)
  const [listError, setListError] = useState<string | null>(null)
  const [runMode, setRunMode] = useState<RunMode | null>(null)
  const [executionAsked, setExecutionAsked] = useState(false)
  // Chat options (profile, run on, effort, memory) sit behind one button.
  const [optionsOpen, setOptionsOpen] = useState(false)
  const optionsRef = useRef<HTMLDivElement | null>(null)
  const optionsButtonRef = useRef<HTMLButtonElement | null>(null)
  useEffect(() => {
    if (!optionsOpen) return
    const onPointer = (event: globalThis.MouseEvent) => {
      if (!optionsRef.current?.contains(event.target as Node)) setOptionsOpen(false)
    }
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return
      setOptionsOpen(false)
      optionsButtonRef.current?.focus()
    }
    document.addEventListener('mousedown', onPointer)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onPointer)
      document.removeEventListener('keydown', onKey)
    }
  }, [optionsOpen])
  const { scrollerRef, contentRef, showJump, jumpToLatest, followLatest } = useChatFollow(selectedId)

  useEffect(() => {
    setContextUsage(null)
  }, [selectedId])
  const abortRef = useRef<AbortController | null>(null)
  const lastUserMessageRef = useRef('')
  const [toolFailure, setToolFailure] = useState<string | null>(null)
  const [contextUsage, setContextUsage] = useState<ContextUsage | null>(null)
  const streamingConvRef = useRef<string | null>(null)
  const composerRef = useRef<HTMLTextAreaElement | null>(null)
  const profileSelectRef = useRef<HTMLSelectElement | null>(null)
  const modelSelectRef = useRef<HTMLSelectElement | null>(null)
  const streamingTextRef = useRef('')
  // The turn that just finished. Its stream can still deliver tokens after
  // chat.complete (a memory reply finishes instantly); they must not draw a
  // second copy of the saved answer.
  const completedConvRef = useRef<string | null>(null)
  // Set when the user presses Stop, so the closed stream is not shown as an error.
  const stoppedRef = useRef(false)

  const conversationsQuery = useQuery({
    queryKey: ['conversations'],
    queryFn: () => api.getConversations(),
    retry: false,
  })

  const profilesQuery = useQuery({
    queryKey: ['profiles'],
    queryFn: () => api.getProfiles(),
    retry: false,
  })

  const modelsQuery = useQuery({
    queryKey: ['models'],
    queryFn: () => api.getModels(),
    retry: false,
  })

  const specializedQuery = useQuery({
    queryKey: ['training', 'deployed'],
    queryFn: () => api.listDeployedAIs(),
    retry: false,
  })

  const nodesQuery = useQuery({
    queryKey: ['nodes'],
    queryFn: () => api.getNodes(),
    retry: false,
    staleTime: 30_000,
  })

  const settingsQuery = useQuery({
    queryKey: ['settings'],
    queryFn: () => api.getSettings(),
    retry: false,
    staleTime: 60_000,
  })
  const toolsQuery = useQuery({
    queryKey: ['tools'],
    queryFn: () => api.listTools(),
    retry: false,
    staleTime: 60_000,
  })
  // Read aloud runs on this computer, where speech can be installed.
  const speechTool = toolsQuery.data?.find((t) => t.id === 'speech.synthesize')
  const canReadAloud = !!speechTool && speechTool.health !== 'unavailable'

  const messagesQuery = useQuery({
    queryKey: ['messages', selectedId],
    queryFn: () => (selectedId ? api.getMessages(selectedId) : null),
    enabled: Boolean(selectedId),
    retry: false,
  })

  useEffect(() => {
    const fromQuery = searchParams.get('c')
    const profileFromQuery = searchParams.get('profile')
    const startNew = searchParams.get('new') === '1'
    if (!fromQuery && !profileFromQuery && !startNew) return

    if (fromQuery) {
      setSelectedId(fromQuery)
    }
    if (profileFromQuery) {
      setActiveProfileId(profileFromQuery)
      setDraftProfileId(profileFromQuery)
    }
    if (startNew) {
      setSelectedId(null)
      setSendError(null)
      setStreamingContent(null)
      setTeamSteps([])
      setPlanSteps([])
      setDraft('')
      setListError(null)
    }
    setSearchParams({}, { replace: true })
  }, [searchParams, setSearchParams, setActiveProfileId])

  const conversations = conversationsQuery.data ?? []
  const selectedConversation =
    conversations.find((c) => c.id === selectedId) ?? null
  // Embedding, reranker, and classifier models help Yggdrasil but cannot chat.
  const installedModels = (modelsQuery.data ?? []).filter(
    (m) => (m.installed || (m.installed_on?.length ?? 0) > 0) && canChat(m),
  )
  // Deployed specialized AIs answer through their base model, on this computer.
  const specializedModels = (specializedQuery.data ?? []).filter((m) => m.installed)
  const sortedProfiles = useMemo(
    () => sortProfiles(profilesQuery.data ?? []),
    [profilesQuery.data],
  )

  const hasCluster = useMemo(() => {
    const nodes = nodesQuery.data ?? []
    const online = nodes.filter((n) => n.status === 'online')
    if (online.length > 1) return true
    return nodes.some((n) => !n.is_local && (n.paired || n.status === 'online'))
  }, [nodesQuery.data])

  const preferredProfileId = settingsQuery.data?.default_profile_id
  const preferredExists = preferredProfileId
    ? sortedProfiles.some((p) => p.id === preferredProfileId)
    : false

  const defaultProfileId =
    activeProfileId ||
    (preferredExists ? preferredProfileId : null) ||
    (hasCluster
      ? sortedProfiles.find((p) => isTeamProfile(p))?.id ||
        sortedProfiles.find((p) => p.id === 'programming')?.id ||
        sortedProfiles.find((p) => p.purpose === 'coding')?.id
      : null) ||
    sortedProfiles.find((p) => p.id === 'general-assistant')?.id ||
    sortedProfiles.find((p) => p.purpose === 'general')?.id ||
    sortedProfiles[0]?.id ||
    null

  const profileIdForChat =
    selectedConversation?.profile_id ||
    draftProfileId ||
    defaultProfileId ||
    undefined

  const chosenModelId = modelChoice && modelChoice.chatId === selectedId ? modelChoice.modelId : null
  const modelIdForChat =
    chosenModelId ||
    selectedConversation?.model_id ||
    draftModelId ||
    (installedModels.length > 0 ? AUTO_MODEL_ID : undefined)
  const isAuto = modelIdForChat === AUTO_MODEL_ID
  // With Auto, the model shown is the one the latest turn used.
  const shownModelId = isAuto
    ? routedModel && routedModel.chatId === selectedId
      ? routedModel.modelId
      : null
    : modelIdForChat

  const modelLocations =
    installedModels.find((m) => m.id === shownModelId)?.installed_on ?? []

  const chatModel =
    [...(modelsQuery.data ?? []), ...specializedModels].find((model) => model.id === shownModelId) ?? null
  const activeProfile =
    sortedProfiles.find((p) => p.id === profileIdForChat) ?? null
  const terminalAllowed = activeProfile?.tools?.find((tool) => tool.tool_id === 'terminal')?.policy !== 'deny'
  const internetAllowed =
    activeProfile?.tools == null
      ? true
      : activeProfile.tools.some(
          (tool) =>
            (tool.tool_id === 'internet.search' || tool.tool_id === 'internet.open') &&
            tool.policy !== 'deny',
        )
  const chatToolAssessment = chatModel ? modelToolAssessment(chatModel, { terminalAllowed }) : null
  const capabilityLine = activeCapabilities(activeProfile?.tools).map((id) =>
    id === 'internet' ? t('composer.web') : capabilityLabel(id),
  )
  const activeIsTeam = isTeamProfile(activeProfile)

  const defaultExecution = settingsQuery.data?.default_execution ?? 'automatic'
  const downloadBehavior = settingsQuery.data?.download_behavior ?? 'ask'

  useEffect(() => {
    if (runMode != null) return
    if (defaultExecution === 'ask') return
    if (defaultExecution === 'local' || defaultExecution === 'automatic') {
      setRunMode(defaultExecution)
    }
  }, [defaultExecution, runMode])

  const effectiveRunMode: RunMode = runMode ?? (activeIsTeam ? 'automatic' : 'local')

  useEffect(() => {
    if (!draftProfileId && defaultProfileId) {
      setDraftProfileId(defaultProfileId)
    }
  }, [defaultProfileId, draftProfileId])

  useEffect(() => {
    if (!draftModelId && installedModels.length > 0) {
      setDraftModelId(AUTO_MODEL_ID)
    }
  }, [draftModelId, installedModels])

  useEffect(() => {
    composerRef.current?.focus()
  }, [selectedId])

  const deleteConversation = useMutation({
    mutationFn: (id: string) => api.deleteConversation(id),
    onSuccess: (_data, id) => {
      setPendingDelete(null)
      setListError(null)
      queryClient.setQueryData<Conversation[]>(['conversations'], (current) =>
        (current ?? []).filter((c) => c.id !== id),
      )
      queryClient.invalidateQueries({ queryKey: ['conversations'] })
      queryClient.removeQueries({ queryKey: ['messages', id] })
      if (selectedId === id) {
        setSelectedId(null)
        setSendError(null)
        setStreamingContent(null)
        setTeamSteps([])
        setPlanSteps([])
      }
    },
    onError: (error) => {
      setPendingDelete(null)
      setListError(
        error instanceof Error ? error.message : t('send.deleteFailed'),
      )
    },
  })

  const updateConversation = useMutation({
    mutationFn: ({
      id,
      model_id,
      profile_id,
      title,
      memory_off,
    }: {
      id: string
      model_id?: string
      profile_id?: string
      title?: string
      memory_off?: boolean
    }) => api.updateConversation(id, { model_id, profile_id, title, memory_off }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['conversations'] })
      setRenamingId(null)
    },
  })

  const handleChatComplete = useCallback(
    (conversationId: string) => {
      if (conversationId && conversationId === streamingConvRef.current) {
        setStreamingContent(null)
        streamingConvRef.current = null
        completedConvRef.current = conversationId
        setIsSending(false)
        setStatusMessage(null)
        setPendingTool(null)
        // The saved answer now carries its sources and steps.
        setToolTraces([])
        abortRef.current = null
        queryClient.invalidateQueries({ queryKey: ['messages', conversationId] })
        queryClient.invalidateQueries({ queryKey: ['conversations'] })
      }
    },
    [queryClient],
  )

  const decidePendingTool = async (allow: boolean, allowSession = false) => {
    if (!pendingTool || toolDeciding) return
    setToolDeciding(true)
    try {
      await api.decideTool({
        request_id: pendingTool.requestId,
        allow,
        allow_session: allowSession,
      })
      setPendingTool(null)
      setStatusMessage(
        allow ? (allowSession ? t('status.toolAllowedSession') : t('status.toolAllowed')) : t('status.toolDenied'),
      )
    } catch (error) {
      setSendError(
        error instanceof Error
          ? error.message
          : t('tools.decideFailed'),
      )
    } finally {
      setToolDeciding(false)
    }
  }

  // Both dialogs keep keyboard focus inside while open. Escape is the safe
  // choice: keep the chat, or deny the tool.
  const deleteDialogRef = useDialog(Boolean(pendingDelete), () => {
    if (!deleteConversation.isPending) setPendingDelete(null)
  })
  const toolDialogRef = useDialog(Boolean(pendingTool), () => {
    if (!toolDeciding) void decidePendingTool(false)
  })

  // The event stream is opened once. Its handler reads the current chat,
  // model locations, and completion handler from this ref; depending on
  // them reopened the stream on nearly every render and dropped events.
  const eventContext = useRef({ selectedId, handleChatComplete, modelLocations })
  eventContext.current = { selectedId, handleChatComplete, modelLocations }

  useEffect(() => {
    const unsubscribe = subscribeEvents({
      onEvent: (event) => {
        const { selectedId, handleChatComplete, modelLocations } = eventContext.current
        if (event.type === 'tool.requested') {
          const payload = event.payload as ToolRequestedPayload | undefined
          if (!payload?.request_id || !payload.tool_id) return
          const conversationId = payload.conversation_id
          if (
            conversationId &&
            conversationId !== selectedId &&
            conversationId !== streamingConvRef.current
          ) {
            return
          }
          setPendingTool({
            requestId: payload.request_id,
            toolId: payload.tool_id,
            reason: payload.reason,
            argsSummary: friendlyToolDetail(payload.args) || t('tools.noDetails'),
            rawArgs: formatToolArgs(payload.args),
          })
          setStatusMessage(t('status.waitingForPermission', { tool: toolDisplayName(payload.tool_id) }))
        }
        if (event.type === 'tool.started') {
          const toolId = event.payload?.tool_id as string | undefined
          const summary = event.payload?.summary as string | undefined
          setStatusMessage(toolProgress(toolId, summary))
          if (toolId) {
            setToolTraces((current) => [
              ...current,
              { label: toolDisplayName(toolId), detail: summary || '', status: 'running' },
            ])
          }
        }
        if (event.type === 'tool.completed') {
          const ms = Number(event.payload?.duration_ms)
          const done = Number.isFinite(ms) && ms >= 0 ? t('tools.completedIn', { ms: Math.round(ms) }) : t('tools.completed')
          setStatusMessage(t('status.checkingSources'))
          setToolFailure(null)
          setToolTraces((current) => {
            const next = [...current]
            for (let i = next.length - 1; i >= 0; i -= 1) {
              if (next[i].status === 'running') {
                next[i] = { ...next[i], status: done }
                break
              }
            }
            return next
          })
        }
        if (event.type === 'tool.failed') {
          if (event.payload?.malformed) return
          const toolId = event.payload?.tool_id as string | undefined
          const label = toolId ? toolDisplayName(toolId) : t('tools.unnamed')
          setToolFailure(label)
          setStatusMessage(t('status.toolFailed', { tool: label }))
          setToolTraces((current) => {
            const next = [...current]
            for (let i = next.length - 1; i >= 0; i -= 1) {
              if (next[i].status === 'running') {
                next[i] = { ...next[i], status: t('tools.traceFailed') }
                break
              }
            }
            return next
          })
        }
        if (event.type === 'orchestration.role') {
          const payload = event.payload as OrchestrationRolePayload | undefined
          const conversationId = payload?.conversation_id
          if (
            conversationId &&
            conversationId !== selectedId &&
            conversationId !== streamingConvRef.current
          ) {
            return
          }
          if (!payload?.role) return
          const nodeName =
            payload.node_name ||
            modelLocations.find((n) => n.node_id === payload.node_id)?.node_name ||
            payload.node_id
          setTeamSteps((current) => {
            if (current.some((step) => step.role === payload.role)) {
              return current.map((step) =>
                step.role === payload.role
                  ? { role: payload.role, nodeId: payload.node_id, nodeName }
                  : step,
              )
            }
            return [
              ...current,
              { role: payload.role, nodeId: payload.node_id, nodeName },
            ]
          })
          setStatusMessage(
            nodeName
              ? t('status.roleOn', { role: formatRoleLabel(payload.role), computer: nodeName })
              : t('status.roleRunning', { role: formatRoleLabel(payload.role) }),
          )
        }
        if (event.type === 'chat.model_routed') {
          const conversationId = event.payload?.conversation_id as string | undefined
          if (!conversationId || (conversationId !== selectedId && conversationId !== streamingConvRef.current)) return
          const routedId = event.payload?.model_id as string | undefined
          if (routedId) setRoutedModel({ chatId: conversationId, modelId: routedId })
          const name = (event.payload?.model_name as string | undefined) || routedId
          if (name) setStatusMessage(t(event.payload?.fallback ? 'status.switchingTo' : 'status.using', { model: name }))
        }
        if (event.type === 'plan.created' || event.type === 'plan.step' || event.type === 'chat.verifying') {
          const conversationId = event.payload?.conversation_id as string | undefined
          if (conversationId && conversationId !== selectedId && conversationId !== streamingConvRef.current) return
          if (event.type === 'chat.verifying') {
            setStatusMessage(t('status.verifying'))
          } else if (event.type === 'plan.created') {
            const steps = (event.payload?.steps as string[] | undefined) ?? []
            setPlanSteps(steps.map((step) => ({ step, status: 'pending' })))
            setStatusMessage(t('status.workingThrough', { count: steps.length }))
          } else {
            const index = Number(event.payload?.index)
            const status = event.payload?.status as 'running' | 'done' | 'failed'
            setPlanSteps((cur) => cur.map((s, i) => (i === index ? { ...s, status } : s)))
            if (status === 'running') setStatusMessage(t('status.workingOn', { step: event.payload?.step as string }))
            else setStatusMessage(t('status.puttingTogether'))
          }
        }
        if (event.type === 'chat.stopped') {
          const conversationId = event.payload?.conversation_id as string | undefined
          if (!conversationId) return
          // The kept part of the answer is saved; show it in every window.
          queryClient.invalidateQueries({ queryKey: ['messages', conversationId] })
          if (conversationId === streamingConvRef.current) {
            streamingConvRef.current = null
            completedConvRef.current = conversationId
            setIsSending(false)
            setStreamingContent(null)
            setStatusMessage(null)
            setPendingTool(null)
            setPlanSteps([])
          }
        }
        if (event.type === 'chat.making_file') {
          const conversationId = event.payload?.conversation_id as string | undefined
          if (conversationId && conversationId !== selectedId && conversationId !== streamingConvRef.current) return
          const name = event.payload?.name as string | undefined
          setStatusMessage(name ? t('status.writingNamed', { name }) : t('status.writingFile'))
        }
        if (event.type === 'chat.lookup') {
          const conversationId = event.payload?.conversation_id as string | undefined
          if (conversationId && conversationId !== selectedId && conversationId !== streamingConvRef.current) return
          const query = event.payload?.query as string | undefined
          setStatusMessage(query ? t('status.searchingWebFor', { query }) : t('status.searchingWeb'))
        }
        if (event.type === 'model.load.started') {
          const nodeId = event.payload?.node_id as string | undefined
          const role = event.payload?.role as string | undefined
          const nodeName =
            (event.payload?.node_name as string | undefined) ||
            modelLocations.find((n) => n.node_id === nodeId)?.node_name
          setStatusMessage(
            role && nodeName
              ? t('status.loadingRoleOn', { role: formatRoleLabel(role), computer: nodeName })
              : nodeName
                ? t('status.loadingModelOn', { computer: nodeName })
                : t('status.loadingModel'),
          )
        }
        if (event.type === 'model.load.completed') {
          const nodeId = event.payload?.node_id as string | undefined
          const role = event.payload?.role as string | undefined
          const nodeName =
            (event.payload?.node_name as string | undefined) ||
            modelLocations.find((n) => n.node_id === nodeId)?.node_name
          setStatusMessage(
            role && nodeName
              ? t('status.roleGeneratingOn', { role: formatRoleLabel(role), computer: nodeName })
              : nodeName
                ? t('status.generatingOn', { computer: nodeName })
                : t('status.generating'),
          )
        }
        if (event.type === 'chat.token') {
          if (streamingConvRef.current) return
          const payload = event.payload as ChatTokenPayload | undefined
          if (!payload?.conversation_id || payload.conversation_id !== selectedId) {
            return
          }
          if (payload.conversation_id === completedConvRef.current) return
          setStatusMessage(null)
          if (payload.content) {
            setStreamingContent((current) => (current ?? '') + payload.content)
          }
        }
        if (event.type === 'chat.complete') {
          const conversationId = event.payload?.conversation_id as string | undefined
          const nextUsage = parseContextUsage(event.payload?.context)
          if (
            nextUsage &&
            conversationId &&
            conversationId === (streamingConvRef.current ?? selectedId)
          ) {
            setContextUsage(nextUsage)
          }
          const answeredBy = event.payload?.model_id as string | undefined
          if (conversationId && answeredBy && conversationId === (streamingConvRef.current ?? selectedId)) {
            setRoutedModel({ chatId: conversationId, modelId: answeredBy })
          }
          if (conversationId) {
            // A turn finished that this tab did not start (another window),
            // so later turns in it may stream here again.
            if (conversationId !== streamingConvRef.current && conversationId === completedConvRef.current) {
              completedConvRef.current = null
            }
            handleChatComplete(conversationId)
          }
        }
        if (event.type === 'knowledge.retrieved') {
          const conversationId = event.payload?.conversation_id as string | undefined
          if (conversationId && conversationId !== selectedId && conversationId !== streamingConvRef.current) return
          const passages = Number(event.payload?.passages) || 0
          const names = (event.payload?.sources as string[] | undefined) ?? []
          const source = names[0] ?? t('status.yourKnowledge')
          setStatusMessage(
            passages === 0
              ? t('status.checkedKnowledge')
              : names.length > 1
                ? t('status.foundPassagesMore', { count: passages, source, more: names.length - 1 })
                : t('status.foundPassages', { count: passages, source }),
          )
        }
        if (event.type === 'chat.error') {
          setIsSending(false)
          setStreamingContent(null)
          streamingConvRef.current = null
          setStatusMessage(null)
          setPendingTool(null)
          setSendError((event.payload?.message as string) || t('send.generationFailed'))
        }
      },
    })
    return unsubscribe
    // queryClient and t are stable; everything else is read through eventContext.
  }, [queryClient, t])

  const setProfile = (profileId: string) => {
    setDraftProfileId(profileId)
    if (selectedId) {
      updateConversation.mutate({ id: selectedId, profile_id: profileId })
    }
  }

  const chooseRunMode = (mode: RunMode) => {
    setRunMode(mode)
    setExecutionAsked(true)
  }

  const enableInternet = async () => {
    if (!activeProfile) return
    const tools = setCapability(activeProfile.tools ?? [], 'internet', true)
    await api.updateProfile(activeProfile.id, { ...activeProfile, tools })
    await queryClient.invalidateQueries({ queryKey: ['profiles'] })
    setCapabilityNotice(null)
  }

  const setModel = (modelId: string) => {
    setModelChoice({ chatId: selectedId, modelId })
    setDraftModelId(modelId)
    if (selectedId) {
      updateConversation.mutate({ id: selectedId, model_id: modelId })
    }
  }

  const ensureModelReady = async (): Promise<string | null> => {
    if (modelIdForChat) return modelIdForChat
    const catalog = modelsQuery.data ?? []
    const candidate =
      catalog.find((m) => m.installed || (m.installed_on?.length ?? 0) > 0)?.id ||
      activeProfile?.roles?.find((r) => r.model_id)?.model_id ||
      catalog[0]?.id
    if (!candidate) return null

    const already =
      catalog.find((m) => m.id === candidate)?.installed ||
      (catalog.find((m) => m.id === candidate)?.installed_on?.length ?? 0) > 0
    if (already) return candidate

    if (downloadBehavior === 'ask') {
      const name =
        catalog.find((m) => m.id === candidate)?.display_name || candidate
      const ok = window.confirm(t('send.confirmDownload', { model: name }))
      if (!ok) return null
    }

    setStatusMessage(t('status.downloadingModel'))
    await api.installModel(candidate, { wait: true })
    await queryClient.invalidateQueries({ queryKey: ['models'] })
    setDraftModelId(candidate)
    return candidate
  }

  const addFiles = (list: FileList | File[]) => {
    for (const file of Array.from(list)) {
      const key = `${file.name}-${file.size}-${Math.random().toString(36).slice(2)}`
      const base: PendingFile = { key, name: file.name, size: file.size, status: 'uploading' }
      if (!isAttachable(file.name)) {
        setPendingFiles((cur) => [...cur, { ...base, status: 'error', error: t('attachments.unsupported') }])
        continue
      }
      if (file.size > MAX_ATTACH_BYTES) {
        setPendingFiles((cur) => [...cur, { ...base, status: 'error', error: t('attachments.tooLarge') }])
        continue
      }
      setPendingFiles((cur) => [...cur, base])
      void (async () => {
        try {
          const uploaded = await api.uploadArtifact(await readUpload(file), selectedId ?? undefined)
          if (!uploaded) throw new Error(t('attachments.addFailed'))
          setPendingFiles((cur) => cur.map((f) => (f.key === key ? { ...f, status: 'ready', file: uploaded } : f)))
        } catch (err) {
          const error = err instanceof Error ? err.message : t('attachments.addFailed')
          setPendingFiles((cur) => cur.map((f) => (f.key === key ? { ...f, status: 'error', error } : f)))
        }
      })()
    }
  }

  const removePendingFile = (key: string) => {
    setPendingFiles((cur) => {
      const gone = cur.find((f) => f.key === key)
      if (gone?.file) void api.deleteArtifact(gone.file.id).catch(() => undefined)
      return cur.filter((f) => f.key !== key)
    })
  }

  // A setup offered in an answer finishes the request once it is ready
  // (Gungnir §29); the card holds a stable callback to the latest send.
  const sendRef = useRef<(text: string) => void>(() => {})
  useEffect(() => {
    sendRef.current = (text: string) => void sendMessage(text)
  })
  const continueAfterSetup = useCallback((text: string) => sendRef.current(text), [])

  const sendMessage = async (overrideText?: string) => {
    const readyFiles = overrideText == null ? pendingFiles.filter((f) => f.status === 'ready' && f.file) : []
    if (overrideText == null && pendingFiles.some((f) => f.status === 'uploading')) {
      setSendError(t('send.waitForFiles'))
      return
    }
    // A file on its own is a request to look at it.
    const typed = (overrideText ?? draft).trim()
    const message = typed || (readyFiles.length > 0 ? t('send.summarizeFile', { count: readyFiles.length }) : '')
    if (!message || isSending) return

    if (defaultExecution === 'ask' && !executionAsked && runMode == null) {
      setSendError(t('send.chooseRunOn'))
      return
    }

    let modelId: string | undefined = modelIdForChat
    if (!modelId) {
      try {
        modelId = (await ensureModelReady()) ?? undefined
      } catch (error) {
        setSendError(
          error instanceof Error
            ? error.message
            : t('send.downloadFailed'),
        )
        setStatusMessage(null)
        return
      }
      if (!modelId) {
        setSendError(t('send.installFirst'))
        return
      }
    }

    lastUserMessageRef.current = message
    followLatest()
    setDraft('')
    const attachments = readyFiles.map((f) => f.file!)
    if (overrideText == null) setPendingFiles([])
    setIsSending(true)
    setSendError(null)
    setModelFailure(null)
    setResponseInterrupted(false)
    setToolFailure(null)
    const catalog = modelsQuery.data ?? []
    // Auto chooses a capable model itself, so there is nothing to warn about.
    const activeModel = modelId === AUTO_MODEL_ID ? null : (catalog.find((item) => item.id === modelId) ?? chatModel)
    setCapabilityNotice(capabilityGap(message, activeModel, catalog, { terminalAllowed, internetAllowed }))
    setToolTraces([])
    setToolDetailsOpen(false)
    setStatusMessage(t('status.starting'))
    setTeamSteps([])
    setPlanSteps([])
    setPendingTool(null)
    setStreamingContent('')
    streamingTextRef.current = ''
    completedConvRef.current = null
    stoppedRef.current = false

    let conversationId = selectedId
    try {
      if (!conversationId) {
        const created = await api.createConversation({
          title: message.length > 48 ? `${message.slice(0, 48)}…` : message,
          profile_id: profileIdForChat,
          model_id: modelId,
        })
        if (!created?.id) {
          throw new Error(t('send.startFailed'))
        }
        conversationId = created.id
        if (memoryOffDraft) {
          await api.updateConversation(created.id, { memory_off: true })
          setMemoryOffDraft(false)
        }
        setSelectedId(conversationId)
        await queryClient.invalidateQueries({ queryKey: ['conversations'] })
      }

      streamingConvRef.current = conversationId
      const controller = new AbortController()
      abortRef.current = controller

      // A new chat's first fetch can return before the message is saved and
      // wipe the message shown below; cancel it so the message stays.
      await queryClient.cancelQueries({ queryKey: ['messages', conversationId] })

      queryClient.setQueryData<Message[]>(['messages', conversationId], (current) => {
        const optimistic: Message = {
          id: `optimistic-${Date.now()}`,
          conversation_id: conversationId!,
          role: 'user',
          content: message,
          created_at: new Date().toISOString(),
          meta: attachments.length > 0 ? { files: attachments } : undefined,
        }
        return [...(current ?? []), optimistic]
      })

      await streamChat({
        body: {
          conversation_id: conversationId,
          profile_id: profileIdForChat,
          model_id: modelId,
          message,
          stream: true,
          execution: effectiveRunMode,
          effort,
          attachments: attachments.length > 0 ? attachments.map((f) => f.id) : undefined,
        },
        signal: controller.signal,
        onToken: (content) => {
          if (streamingConvRef.current !== conversationId) return
          setStatusMessage(null)
          setToolFailure(null)
          streamingTextRef.current += content
          setStreamingContent((current) => (current ?? '') + content)
        },
        onDone: () => {
          if (streamingConvRef.current !== conversationId) return
          if (!streamingTextRef.current.trim()) {
            setIsSending(false)
            streamingConvRef.current = null
            setStatusMessage(null)
            setModelFailure({
              kind: 'model_health',
              reason: 'runtime_error',
              message: t('send.noReply'),
            })
            queryClient.invalidateQueries({ queryKey: ['messages', conversationId] })
            return
          }
          handleChatComplete(conversationId!)
        },
        onError: (errMessage, code) => {
          setIsSending(false)
          streamingConvRef.current = null
          setStatusMessage(null)
          if (stoppedRef.current) {
            queryClient.invalidateQueries({ queryKey: ['messages', conversationId] })
            return
          }
          const failure = parseModelFailure(errMessage)
          const partial = streamingTextRef.current.trim()
          if (failure) {
            setModelFailure(failure)
            setSendError(null)
            setResponseInterrupted(failure.interrupted || partial.length > 0)
            setStreamingContent(partial ? streamingTextRef.current : null)
          } else {
            setStreamingContent(null)
            setResponseInterrupted(false)
            setSendError(errMessage || t('send.couldNotRespond'))
            if (code && errMessage) setSendErrorCode({ text: errMessage, code })
          }
          queryClient.invalidateQueries({ queryKey: ['messages', conversationId] })
        },
      })
    } catch (error) {
      setIsSending(false)
      setStreamingContent(null)
      streamingConvRef.current = null
      setStatusMessage(null)
      // Stop closes this window's stream; that is not a failure. The kept
      // part of the answer arrives with chat.stopped.
      if (stoppedRef.current || (error instanceof DOMException && error.name === 'AbortError')) {
        if (conversationId) queryClient.invalidateQueries({ queryKey: ['messages', conversationId] })
        return
      }
      if (error instanceof ApiError && error.code) {
        // The card explains the code in the App language; the service's text goes under Details.
        setSendError(error.serviceMessage)
        setSendErrorCode({ text: error.serviceMessage, code: error.code })
      } else {
        setSendError(error instanceof Error ? error.message : t('send.unreachable'))
      }
      if (conversationId) {
        queryClient.invalidateQueries({ queryKey: ['messages', conversationId] })
      }
    }
  }

  const handleStop = () => {
    stoppedRef.current = true
    // Stop the run on the computer too, not only this window's stream (§67).
    const running = streamingConvRef.current ?? selectedId
    if (running) void api.stopChat(running).catch(() => undefined)
    abortRef.current?.abort()
    setIsSending(false)
    setStreamingContent(null)
    streamingConvRef.current = null
    setStatusMessage(null)
    setPendingTool(null)
    if (selectedId) {
      queryClient.invalidateQueries({ queryKey: ['messages', selectedId] })
    }
  }

  const handleDelete = (conversation: Conversation, event: MouseEvent) => {
    event.preventDefault()
    event.stopPropagation()
    setListError(null)
    // Native window.confirm is unreliable in the Wails WebView — use in-app confirm.
    setPendingDelete(conversation)
  }

  const confirmDelete = () => {
    if (!pendingDelete) return
    deleteConversation.mutate(pendingDelete.id)
  }

  const startRename = (conversation: Conversation, event?: MouseEvent) => {
    event?.stopPropagation()
    setRenamingId(conversation.id)
    setRenameValue(conversation.title)
  }

  const commitRename = () => {
    if (!renamingId || !renameValue.trim()) {
      setRenamingId(null)
      return
    }
    updateConversation.mutate({ id: renamingId, title: renameValue.trim() })
  }

  const startNewChat = () => {
    setSelectedId(null)
    setSendError(null)
    setCapabilityNotice(null)
    setToolFailure(null)
    setToolTraces([])
    setToolDetailsOpen(false)
    setStreamingContent(null)
    setTeamSteps([])
    setPlanSteps([])
    setDraft('')
    setListError(null)
    if (defaultExecution === 'ask') {
      setRunMode(null)
      setExecutionAsked(false)
    }
    if (!(chatHistoryPinned && canPinHistory)) {
      setHistoryOpen(false)
    }
    composerRef.current?.focus()
  }

  useEffect(() => {
    if (chatHistoryPinned && canPinHistory) {
      setHistoryOpen(true)
    }
  }, [chatHistoryPinned, canPinHistory])

  const messages = messagesQuery.data ?? []
  const streamingText = streamingContent == null ? '' : displayChatText(streamingContent)
  const replyInProgress = isSending && streamingText.length === 0
  // Ratatoskr: thinking while a reply is written, delivering when it runs
  // on a paired computer. Only events for this chat, or with no chat, count.
  const replyMascot = useMascotState({
    busy: isSending,
    react: ['think', 'deliver'],
    localNodeId: nodesQuery.data?.find((n) => n.is_local)?.id,
    accept: (event) => {
      const conv = event.payload?.conversation_id as string | undefined
      return !conv || conv === selectedId || conv === streamingConvRef.current
    },
  })
  const showLanding = !selectedId && !draft.trim() && !isSending
  // On the landing page the hero's mascot shows a problem, so the cards below
  // it go without one (one mascot per view).
  const landingError = showLanding && Boolean(modelFailure || sendError)
  const historyMode = chatHistoryPinned && canPinHistory ? 'pinned' : 'overlay'
  const showPinnedSidebar = historyOpen && historyMode === 'pinned'

  const runOnTitle = effectiveRunMode === 'automatic' ? t('composer.runOnAutomatic') : t('composer.runOnLocal')
  // Run on stays in view while Settings asks for a choice before sending.
  const needsRunOn = defaultExecution === 'ask' && !executionAsked && runMode == null
  const memoryOff = selectedConversation ? Boolean(selectedConversation.memory_off) : memoryOffDraft
  // The Options button names what differs from the defaults, so the chat's
  // setup shows without opening it.
  const optionsSummary = [
    sortedProfiles.find((p) => p.id === profileIdForChat)?.name,
    effort !== 'auto' ? t(`effort.${effort}.label`) : '',
    !needsRunOn && runMode === 'local' ? t('composer.thisComputer') : '',
    settingsQuery.data?.memory_enabled !== false && memoryOff ? t('memory.off') : '',
  ]
    .filter(Boolean)
    .join(' · ')

  const historyDrawer = (
    <ChatHistoryDrawer
      open={historyOpen}
      mode={historyMode}
      conversations={conversations}
      pinnedIds={pinnedConversationIds}
      selectedId={selectedId}
      loading={conversationsQuery.isLoading}
      error={listError}
      drawerPinned={chatHistoryPinned}
      canPinDrawer={canPinHistory}
      renamingId={renamingId}
      renameValue={renameValue}
      onRenameValueChange={setRenameValue}
      onCommitRename={commitRename}
      onCancelRename={() => setRenamingId(null)}
      onClose={() => setHistoryOpen(false)}
      onNewChat={startNewChat}
      onSelect={(id) => {
        setSelectedId(id)
        if (historyMode === 'overlay') {
          setHistoryOpen(false)
        }
      }}
      onRename={(conversation) => startRename(conversation)}
      onTogglePin={togglePinnedConversation}
      onDelete={handleDelete}
      onToggleDrawerPinned={() => {
        const next = !chatHistoryPinned
        setChatHistoryPinned(next)
        if (next && canPinHistory) {
          setHistoryOpen(true)
        }
      }}
    />
  )

  const composer = (
    <form
      className={['composer', showLanding ? 'composer-landing' : ''].filter(Boolean).join(' ')}
      onSubmit={(event) => {
        event.preventDefault()
        void sendMessage()
      }}
      onDragOver={(event) => {
        if (event.dataTransfer.types.includes('Files')) event.preventDefault()
      }}
      onDrop={(event) => {
        if (event.dataTransfer.files.length === 0) return
        event.preventDefault()
        if (!isSending) addFiles(event.dataTransfer.files)
      }}
    >
      {pendingFiles.length > 0 ? (
        <div className="flex flex-wrap gap-1.5 px-3 pt-3">
          {pendingFiles.map((file) => (
            <PendingFileChip key={file.key} file={file} onRemove={() => removePendingFile(file.key)} />
          ))}
        </div>
      ) : null}
      <input
        ref={fileInputRef}
        type="file"
        multiple
        accept={ATTACH_ACCEPT}
        className="hidden"
        onChange={(event) => {
          if (event.target.files) addFiles(event.target.files)
          event.target.value = ''
        }}
      />
      <textarea
        ref={composerRef}
        // What is typed takes its own direction; the empty box follows the page.
        dir={draft ? 'auto' : undefined}
        value={draft}
        rows={showLanding ? 5 : 4}
        onChange={(event) => setDraft(event.target.value)}
        onPaste={(event) => {
          if (event.clipboardData.files.length > 0) {
            event.preventDefault()
            addFiles(event.clipboardData.files)
          }
        }}
        onKeyDown={(event) => {
          if (event.key === 'Enter' && !event.shiftKey) {
            event.preventDefault()
            void sendMessage()
          }
        }}
        placeholder={t('composer.placeholder')}
        disabled={isSending}
        className="composer-input"
        aria-label={t('composer.message')}
      />
      <div className="composer-toolbar">
        <div className="flex min-w-0 flex-1 flex-wrap items-center gap-2">
          <button
            type="button"
            className="composer-attach"
            onClick={() => fileInputRef.current?.click()}
            disabled={isSending}
            aria-label={t('composer.attach')}
            title={t('composer.attachHint')}
          >
            <svg viewBox="0 0 16 16" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth={1.5} strokeLinecap="round" aria-hidden>
              <path d="M13.5 7.5 8 13a3.5 3.5 0 0 1-5-5l6-6a2.3 2.3 0 0 1 3.3 3.3L6.4 11.2a1.2 1.2 0 0 1-1.6-1.6L10 4.4" />
            </svg>
          </button>
          {needsRunOn ? (
            <label className="composer-select ring-1 ring-primary/50" title={runOnTitle}>
              <span className="composer-select-label">{t('composer.runOn')}</span>
              <select
                value={
                  runMode ??
                  (defaultExecution === 'ask' ? '' : effectiveRunMode)
                }
                disabled={isSending || updateConversation.isPending}
                onChange={(event) => chooseRunMode(event.target.value as RunMode)}
              >
                {defaultExecution === 'ask' && runMode == null ? (
                  <option value="" disabled>
                    {t('composer.choose')}
                  </option>
                ) : null}
                <option value="automatic">{t('composer.automatic')}</option>
                <option value="local">{t('composer.thisComputer')}</option>
              </select>
            </label>
          ) : null}
          <label className="composer-select" title={t('composer.modelHint')}>
            <span className="composer-select-label">{t('composer.model')}</span>
            <select
              ref={modelSelectRef}
              value={modelIdForChat ?? ''}
              disabled={isSending || installedModels.length === 0}
              onChange={(event) => setModel(event.target.value)}
            >
              {installedModels.length === 0 ? (
                // "No model installed" only once the list has loaded.
                <option value="">
                  {modelsQuery.isError
                    ? t('composer.modelsFailed')
                    : modelsQuery.isSuccess
                      ? t('composer.noModel')
                      : t('composer.modelsLoading')}
                </option>
              ) : specializedModels.length === 0 ? (
                <>
                  <option value={AUTO_MODEL_ID}>{t('composer.auto')}</option>
                  {installedModels.map((model) => (
                    <option key={model.id} value={model.id}>
                      {model.display_name || model.id}
                      {model.status === 'external' ? t('composer.external') : ''}
                    </option>
                  ))}
                </>
              ) : (
                <>
                  <option value={AUTO_MODEL_ID}>{t('composer.auto')}</option>
                  <optgroup label={t('composer.models')}>
                    {installedModels.map((model) => (
                      <option key={model.id} value={model.id}>
                        {model.display_name || model.id}
                        {model.status === 'external' ? t('composer.external') : ''}
                      </option>
                    ))}
                  </optgroup>
                  <optgroup label={t('composer.specialized')}>
                    {specializedModels.map((model) => (
                      <option key={model.id} value={model.id}>
                        {model.display_name}
                      </option>
                    ))}
                  </optgroup>
                </>
              )}
            </select>
          </label>
          <div ref={optionsRef} className="relative">
            <button
              ref={optionsButtonRef}
              type="button"
              className="composer-options-button"
              aria-expanded={optionsOpen}
              aria-controls="composer-options"
              title={t('composer.options')}
              onClick={() => setOptionsOpen((open) => !open)}
            >
              <svg viewBox="0 0 16 16" className="h-3.5 w-3.5 shrink-0" fill="none" stroke="currentColor" strokeWidth={1.5} strokeLinecap="round" aria-hidden>
                <path d="M2.5 4.5h7M12.5 4.5h1M2.5 11.5h1M6.5 11.5h7" />
                <circle cx="11" cy="4.5" r="1.5" />
                <circle cx="5" cy="11.5" r="1.5" />
              </svg>
              <span className="sr-only">{t('composer.options')}: </span>
              <span className="truncate">{optionsSummary || t('composer.options')}</span>
            </button>
            {optionsOpen ? (
              <div id="composer-options" role="group" aria-label={t('composer.options')} className="composer-options">
                <label className="composer-select" title={t('composer.profileHint')}>
                  <span className="composer-select-label">{t('composer.profile')}</span>
                  <select
                    ref={profileSelectRef}
                    value={profileIdForChat ?? ''}
                    disabled={isSending || updateConversation.isPending}
                    onChange={(event) => setProfile(event.target.value)}
                  >
                    {sortedProfiles.map((profile: AIProfile) => (
                      <option key={profile.id} value={profile.id}>
                        {profile.name}
                      </option>
                    ))}
                  </select>
                </label>
                {needsRunOn ? null : (
                  <label className="composer-select" title={runOnTitle}>
                    <span className="composer-select-label">{t('composer.runOn')}</span>
                    <select
                      value={
                        runMode ??
                        (defaultExecution === 'ask' ? '' : effectiveRunMode)
                      }
                      disabled={isSending || updateConversation.isPending}
                      onChange={(event) => chooseRunMode(event.target.value as RunMode)}
                    >
                      {defaultExecution === 'ask' && runMode == null ? (
                        <option value="" disabled>
                          {t('composer.choose')}
                        </option>
                      ) : null}
                      <option value="automatic">{t('composer.automatic')}</option>
                      <option value="local">{t('composer.thisComputer')}</option>
                    </select>
                  </label>
                )}
                <label className="composer-select" title={t(`effort.${effort}.hint`)}>
                  <span className="composer-select-label">{t('composer.effort')}</span>
                  <select value={effort} disabled={isSending} onChange={(event) => setEffort(event.target.value as Effort)}>
                    {EFFORTS.map((e) => (
                      <option key={e} value={e} title={t(`effort.${e}.hint`)}>
                        {t(`effort.${e}.label`)}
                      </option>
                    ))}
                  </select>
                </label>
                <MemoryToggle
                  memoryEnabled={settingsQuery.data?.memory_enabled !== false}
                  off={memoryOff}
                  disabled={isSending}
                  onChange={(off) => {
                    if (selectedConversation) {
                      updateConversation.mutate({ id: selectedConversation.id, memory_off: off })
                    } else {
                      setMemoryOffDraft(off)
                    }
                  }}
                />
              </div>
            ) : null}
          </div>
        </div>
        <ContextUsageButton
          usage={contextUsage}
          windowLimit={contextWindow(chatModel?.context)}
        />
        {isSending ? (
          <button type="button" className="btn-secondary px-3 py-2" onClick={handleStop}>
            {t('composer.stop')}
          </button>
        ) : (
          <button
            type="submit"
            className="btn-primary h-9 w-9 shrink-0 rounded-full p-0 text-lg leading-none"
            disabled={!draft.trim() && !pendingFiles.some((f) => f.status === 'ready')}
            aria-label={t('composer.send')}
            title={t('composer.send')}
          >
            ↑
          </button>
        )}
      </div>
            {(chatModel || isAuto) && (
          <p
            className="mt-2 text-xs text-ink-muted"
            title={isAuto ? t('composer.autoHint') : chatToolAssessment?.detail}
          >
            {isAuto
              ? chatModel
                ? t('composer.autoLastUsed', { model: chatModel.display_name || chatModel.id })
                : t('composer.autoPicks')
              : chatModel?.display_name || chatModel?.id}
            {capabilityLine.length > 0 ? ` · ${capabilityLine.join(' · ')}` : ''}
          </p>
        )}
      {!modelIdForChat && modelsQuery.isError && !modelsQuery.data && (
        <p role="alert" className="mt-2 text-xs text-ink-muted">
          {t('composer.modelsFailedHint')}{' '}
          <button
            type="button"
            className="font-medium text-primary underline-offset-2 hover:underline"
            disabled={modelsQuery.isFetching}
            onClick={() => void modelsQuery.refetch()}
          >
            {t('loadError.retry', { ns: 'common' })}
          </button>
        </p>
      )}
      {!modelIdForChat && modelsQuery.isSuccess && (
        <p className="mt-2 text-xs text-ink-muted">
          {downloadBehavior === 'ask' ? t('composer.noModelAsk') : t('composer.noModelDownload')}{' '}
          <Link to="/models" className="font-medium text-primary underline-offset-2 hover:underline">
            {t('composer.openModels')}
          </Link>
        </p>
      )}
      {defaultExecution === 'ask' && !executionAsked && runMode == null && (
        <p className="mt-2 text-xs text-ink-muted">
          <Trans
            t={t}
            i18nKey="composer.chooseRunOn"
            components={{ strong: <span className="font-medium text-ink" /> }}
          />
        </p>
      )}
    </form>
  )

  const suggestions = (
    <div className="mt-5 flex flex-wrap justify-center gap-2">
      <span className="self-center text-xs text-ink-faint">{t('landing.try')}</span>
      {SUGGESTIONS.map((item) => (
        <button
          key={item}
          type="button"
          className="chat-suggestion"
          onClick={() => void sendMessage(t(`landing.suggestions.${item}.prompt`))}
          disabled={isSending}
        >
          {t(`landing.suggestions.${item}.label`)}
        </button>
      ))}
    </div>
  )

  return (
    <div className="page-fill gap-0">
      <div className="flex min-h-0 min-w-0 flex-1 overflow-hidden">
        {showPinnedSidebar ? historyDrawer : null}

        <section className="relative flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden rounded-panel bg-canvas/40 px-1 sm:px-2">
          <div className="flex shrink-0 items-center gap-2 px-1 pb-2 pt-1">
            <button
              type="button"
              className={[
                'inline-flex shrink-0 items-center gap-2 whitespace-nowrap rounded-lg px-2.5 py-1.5 text-sm font-medium transition',
                historyOpen && historyMode === 'overlay'
                  ? 'bg-primary-soft text-primary-active'
                  : 'text-ink-muted hover:bg-raised hover:text-ink',
              ].join(' ')}
              aria-expanded={historyOpen}
              aria-controls="chat-history-drawer"
              onClick={() => setHistoryOpen((open) => !open)}
            >
              <span aria-hidden className="text-base leading-none">
                ☰
              </span>
              {t('header.history')}
            </button>
            <button
              type="button"
              className="btn-primary inline-flex shrink-0 items-center gap-1.5 whitespace-nowrap px-3 py-1.5 text-xs"
              title={t('header.newChat')}
              onClick={startNewChat}
            >
              <span aria-hidden>+</span>
              {t('header.newChat')}
            </button>

            {selectedId ? (
              <div className="ms-auto flex min-w-0 items-center gap-2">
                <h2 className="min-w-0 truncate font-display text-base font-semibold text-ink sm:text-lg">
                  {selectedConversation?.title || t('header.untitledChat')}
                </h2>
                <p className="hidden shrink-0 text-xs text-ink-faint sm:inline" title={runOnTitle}>
                  {activeProfile?.name ?? t('header.assistant')}
                  <span className="mx-1.5 text-ink-faint/60">·</span>
                  {effectiveRunMode === 'automatic' ? t('composer.automatic') : t('composer.thisComputer')}
                </p>
                {selectedConversation && (
                  <button
                    type="button"
                    className="shrink-0 rounded-md px-2 py-1 text-xs text-ink-faint transition hover:bg-danger/15 hover:text-danger"
                    title={t('header.deleteChat')}
                    aria-label={t('header.deleteChat')}
                    onClick={(e) => handleDelete(selectedConversation, e)}
                  >
                    {t('header.delete')}
                  </button>
                )}
              </div>
            ) : null}
          </div>

          {!showPinnedSidebar && historyMode === 'overlay' ? historyDrawer : null}

          <div className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
            <div
              className={[
                'chat-landing-hero flex flex-col items-center px-4',
                showLanding
                  ? 'flex-1 justify-end pb-2 pt-[min(12vh,5rem)]'
                  : 'chat-landing-hero-exit',
              ].join(' ')}
              aria-hidden={!showLanding}
              inert={!showLanding}
            >
              <Ratatoskr state={landingError ? 'error' : 'idle'} size={96} className="mb-3" />
              <RealmKicker path="/chat" className="justify-center" />
              <h1 className="font-display text-2xl font-semibold tracking-tight text-ink sm:text-3xl">
                {t('landing.title')}
              </h1>
              <p className="mt-2 max-w-md text-center text-sm text-ink-muted">{t('landing.subtitle')}</p>
            </div>

            {selectedId ? (
              <div className="relative min-h-0 flex-1">
                <div
                  ref={scrollerRef}
                  className="chat-transcript absolute inset-0 overflow-y-auto px-1 pb-2 [overflow-anchor:none]"
                >
                  <div ref={contentRef} className="space-y-4">
                {messagesQuery.isLoading && <LoadingSpinner />}
                {messages.map((message) => {
                  const text = displayChatText(message.content)
                  if (message.role === 'assistant' && !text) return null
                  return (
                  <div
                    key={message.id}
                    className={[
                      'max-w-[min(42rem,85%)] break-words rounded-2xl px-4 py-3 text-[15px] leading-relaxed',
                      message.role === 'user'
                        ? 'ms-auto bg-primary text-primary-fg'
                        : 'bg-raised/80 text-ink',
                    ].join(' ')}
                  >
                    {message.role === 'assistant' ? (
                      <>
                        <ChatMarkdown text={text} />
                        <AnswerDetails meta={message.meta} />
                        {message.meta?.setup ? (
                          <SetupOfferCard offer={message.meta.setup} onContinue={continueAfterSetup} />
                        ) : null}
                        {canReadAloud ? (
                          <div className="mt-2">
                            <ReadAloudButton text={text} conversationId={message.conversation_id} />
                          </div>
                        ) : null}
                      </>
                    ) : (
                      <>
                        <span dir="auto" className="whitespace-pre-wrap">{text}</span>
                        {message.meta?.files?.length ? (
                          <span className="mt-2 flex flex-wrap justify-end gap-1.5">
                            {message.meta.files.map((file) => (
                              <FileChip key={file.id} file={file} />
                            ))}
                          </span>
                        ) : null}
                      </>
                    )}
                  </div>
                  )
                })}

                {isSending && planSteps.length > 0 && (
                  <ol className="max-w-[min(42rem,85%)] space-y-1.5 border-s-2 border-norn/40 ps-3" aria-label={t('transcript.plan')}>
                    {planSteps.map((s, i) => (
                      <li key={i} className="flex items-start gap-2 text-sm text-ink-muted">
                        <span aria-hidden className={s.status === 'done' ? 'text-success' : s.status === 'failed' ? 'text-warning' : s.status === 'running' ? 'text-norn' : 'text-ink-faint'}>
                          {s.status === 'done' ? '✓' : s.status === 'failed' ? '!' : s.status === 'running' ? '●' : '○'}
                        </span>
                        <span className={s.status === 'running' ? 'text-ink' : ''}>{s.step}</span>
                      </li>
                    ))}
                  </ol>
                )}

                {teamSteps.length > 0 && (
                  <ol className="max-w-[min(42rem,85%)] space-y-1.5 border-s-2 border-primary/30 ps-3">
                    {teamSteps.map((step) => (
                      <li key={step.role} className="text-sm text-ink-muted">
                        <span className="font-medium text-ink">
                          {formatRoleLabel(step.role)}
                        </span>
                        {step.nodeName ? (
                          <span>
                            {' '}
                            <Trans
                              t={t}
                              i18nKey="transcript.onComputer"
                              values={{ computer: step.nodeName }}
                              components={{ computer: <span className="text-info" /> }}
                            />
                          </span>
                        ) : null}
                      </li>
                    ))}
                  </ol>
                )}

                {toolFailure && !isSending && !statusMessage && (
                  <p className="text-sm text-ink-muted">
                    {t('tools.failed', { tool: toolFailure })}
                    {' · '}
                    <button
                      type="button"
                      className="text-primary underline-offset-2 hover:underline"
                      onClick={() => void sendMessage(lastUserMessageRef.current)}
                    >
                      {t('tools.retry')}
                    </button>
                  </p>
                )}
                {toolTraces.length > 0 && (
                  <div className="max-w-[min(42rem,85%)] text-xs text-ink-muted">
                    <button
                      type="button"
                      className="underline-offset-2 hover:underline"
                      onClick={() => setToolDetailsOpen((open) => !open)}
                    >
                      {t('tools.used', { count: toolTraces.length })} {toolDetailsOpen ? '▾' : '▸'}
                    </button>
                    {toolDetailsOpen && (
                      <ul className="mt-2 space-y-1">
                        {toolTraces.map((trace, index) => (
                          <li key={`${trace.label}-${index}`}>
                            <span className="font-medium text-ink">{trace.label}</span>
                            {trace.detail ? ` — ${trace.detail}` : ''}
                            <span className="text-ink-faint"> · {trace.status}</span>
                          </li>
                        ))}
                      </ul>
                    )}
                  </div>
                )}

                {streamingText ? (
                  <div className="max-w-[min(42rem,85%)] break-words rounded-2xl bg-raised/80 px-4 py-3 text-[15px] leading-relaxed text-ink">
                    <ChatMarkdown text={streamingText} />
                    {responseInterrupted && !isSending ? (
                      <p className="mt-2 text-xs text-ink-muted">{t('transcript.interrupted')}</p>
                    ) : null}
                    {isSending && (
                      <span className="chat-caret ms-0.5 inline-block h-4 w-0.5 bg-primary align-middle" />
                    )}
                  </div>
                ) : null}

                {capabilityNotice && (
                  <CapabilityNotice
                    gap={capabilityNotice}
                    onUse={(modelId) => {
                      setModel(modelId)
                      setCapabilityNotice(null)
                    }}
                    onEnableInternet={() => void enableInternet()}
                  />
                )}
                {modelFailure ? (
                  <ModelFailureNotice
                    failure={modelFailure}
                    advanced={advancedMode}
                    onRetry={() => {
                      setModelFailure(null)
                      setResponseInterrupted(false)
                      void sendMessage(lastUserMessageRef.current)
                    }}
                    onChooseModel={() => {
                      ;(profileSelectRef.current ?? modelSelectRef.current)?.focus()
                    }}
                  />
                ) : null}
                {sendError && (
                  <ChatErrorCard
                    raw={sendError}
                    code={sendErrorCode?.text === sendError ? sendErrorCode.code : undefined}
                    mascot={!modelFailure}
                    onRetry={() => {
                      setSendError(null)
                      void sendMessage(lastUserMessageRef.current)
                    }}
                    onNewChat={startNewChat}
                  />
                )}
                {routedModel && routedModel.chatId === selectedId && !isSending ? (
                  <RatingPrompt
                    modelId={routedModel.modelId}
                    modelName={(modelsQuery.data ?? []).find((m) => m.id === routedModel.modelId)?.display_name ?? routedModel.modelId}
                  />
                ) : null}
                <RatingDialogHost />
                  </div>
                </div>
                {showJump ? (
                  <button
                    type="button"
                    className="chat-jump absolute bottom-3 left-1/2 z-10 -translate-x-1/2 rounded-full border border-line/80 bg-surface/95 px-3 py-1 text-xs font-medium text-ink-muted shadow-panel backdrop-blur transition hover:text-ink"
                    onClick={jumpToLatest}
                  >
                    <span aria-hidden>↓ </span>
                    {t('header.latest')}
                  </button>
                ) : null}
              </div>
            ) : (
              !showLanding && <div className="min-h-0 flex-1" aria-hidden />
            )}

            <div
              className={[
                'mx-auto w-full max-w-2xl px-2 transition-[margin,padding] duration-300 ease-out sm:px-4',
                showLanding ? 'mb-1 mt-10 pb-2' : 'mt-auto pb-1 pt-2',
              ].join(' ')}
            >
              {replyInProgress ? (
                <div className="mb-3 px-1">
                  <ChatActivity label={statusMessage} mascot={replyMascot} />
                </div>
              ) : null}
              {composer}
              {showLanding ? suggestions : null}
              {!selectedId && capabilityNotice ? (
                <div className="mt-3">
                  <CapabilityNotice
                    gap={capabilityNotice}
                    onUse={(modelId) => {
                      setModel(modelId)
                      setCapabilityNotice(null)
                    }}
                    onEnableInternet={() => void enableInternet()}
                  />
                </div>
              ) : null}
              {!selectedId && modelFailure ? (
                <div className="mt-3">
                  <ModelFailureNotice
                    failure={modelFailure}
                    advanced={advancedMode}
                    mascot={!showLanding}
                    onRetry={() => {
                      setModelFailure(null)
                      setResponseInterrupted(false)
                      void sendMessage(lastUserMessageRef.current)
                    }}
                    onChooseModel={() => {
                      ;(profileSelectRef.current ?? modelSelectRef.current)?.focus()
                    }}
                  />
                </div>
              ) : null}
              {!selectedId && sendError ? (
                <div className="mt-3 flex justify-center">
                  <ChatErrorCard
                    raw={sendError}
                    code={sendErrorCode?.text === sendError ? sendErrorCode.code : undefined}
                    mascot={!showLanding && !modelFailure}
                  />
                </div>
              ) : null}
            </div>
          </div>
        </section>
      </div>

      {pendingDelete && (
        <div
          ref={deleteDialogRef}
          className="fixed inset-0 z-50 flex items-end justify-center bg-ink/40 p-4 sm:items-center"
          role="dialog"
          aria-modal="true"
          aria-labelledby="delete-chat-title"
        >
          <div className="card w-full max-w-sm space-y-4 shadow-panel">
            <div>
              <h2
                id="delete-chat-title"
                className="font-display text-lg font-semibold text-ink"
              >
                {t('deleteDialog.title')}
              </h2>
              <p className="mt-1 break-words text-sm text-ink-muted">
                {t('deleteDialog.body', { title: pendingDelete.title || t('deleteDialog.untitled') })}
              </p>
            </div>
            <div className="flex flex-wrap justify-end gap-2">
              <button
                type="button"
                className="btn-secondary"
                data-autofocus
                disabled={deleteConversation.isPending}
                onClick={() => setPendingDelete(null)}
              >
                {t('deleteDialog.cancel')}
              </button>
              <button
                type="button"
                className="btn-danger"
                disabled={deleteConversation.isPending}
                onClick={confirmDelete}
              >
                {deleteConversation.isPending ? t('deleteDialog.deleting') : t('deleteDialog.delete')}
              </button>
            </div>
          </div>
        </div>
      )}

      {pendingTool && (
        <div
          ref={toolDialogRef}
          className="fixed inset-0 z-50 flex items-end justify-center bg-ink/40 p-4 sm:items-center"
          role="dialog"
          aria-modal="true"
          aria-labelledby="tool-permission-title"
        >
          <div className="card w-full max-w-md space-y-4 shadow-panel">
            <div>
              <h2
                id="tool-permission-title"
                className="font-display text-lg font-semibold text-ink"
              >
                {t('tools.permissionTitle', { tool: toolDisplayName(pendingTool.toolId) })}
              </h2>
              <p className="mt-1 text-sm text-ink-muted">{pendingTool.reason || t('tools.permissionReason')}</p>
            </div>
            <p className="break-words text-sm text-ink">{pendingTool.argsSummary}</p>
            {advancedMode && pendingTool.rawArgs && (
              <pre className="log-panel max-h-40">{pendingTool.rawArgs}</pre>
            )}
            <div className="flex flex-wrap gap-2">
              <button
                type="button"
                className="btn-primary"
                disabled={toolDeciding}
                onClick={() => void decidePendingTool(true, false)}
              >
                {t('tools.allowOnce')}
              </button>
              <button
                type="button"
                className="btn-secondary"
                disabled={toolDeciding}
                onClick={() => void decidePendingTool(true, true)}
              >
                {t('tools.allowSession')}
              </button>
              <button
                type="button"
                className="btn-secondary"
                data-autofocus
                disabled={toolDeciding}
                onClick={() => void decidePendingTool(false)}
              >
                {t('tools.deny')}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
