import { CLIENT_CONTRACT, CLIENT_CONTRACT_HEADER, getApiBase, storedApiKey } from '@/lib/api'
import { hasEventRelay, onRelayedEvent } from '@/lib/desktopBridge'
import type { YggdrasilEvent } from '@/types/api'

export interface EventSubscriptionOptions {
  onEvent: (event: YggdrasilEvent) => void
  onError?: (error: Event) => void
  onOpen?: () => void
}

function parseEventData(raw: string): YggdrasilEvent | null {
  try {
    return JSON.parse(raw) as YggdrasilEvent
  } catch {
    return null
  }
}

const KNOWN_EVENT_TYPES = [
  'chat.token',
  'chat.complete',
  'chat.error',
  'model.download.started',
  'model.download.progress',
  'model.download.completed',
  'model.download.failed',
  'model.load.started',
  'model.load.completed',
  'model.unloaded',
  'orchestration.role',
  'orchestration.final',
  'scheduler.placement',
  'agent.started',
  'agent.completed',
  'tool.requested',
  'tool.started',
  'tool.completed',
  'tool.failed',
  'tool.parsed',
  'chat.model_routed',
  'chat.lookup',
  'chat.stopped',
  'chat.making_file',
  'chat.making_media',
  'plan.created',
  'plan.step',
  'chat.verifying',
  'task.created',
  'task.started',
  'task.completed',
  'task.failed',
  'automation.started',
  'automation.completed',
  'automation.failed',
  'node.discovered',
  'node.paired',
  'node.online',
  'node.offline',
  'training.job',
  'training.eval',
  'training.deployed',
  'knowledge.retrieved',
  'memory.saved',
  'memory.deleted',
  'chat.summarized',
  'notification.created',
  'work.waiting',
] as const

export function subscribeEvents({
  onEvent,
  onError,
  onOpen,
}: EventSubscriptionOptions): () => void {
  // In the desktop app the shell relays the events, which works on Windows,
  // where a stream read through the app arrives only when it ends.
  if (hasEventRelay()) {
    const unsubscribe = onRelayedEvent((json) => {
      const event = parseEventData(json)
      if (event) onEvent(event)
    })
    onOpen?.()
    return unsubscribe
  }

  // jsdom / older runtimes may not provide EventSource.
  if (typeof EventSource === 'undefined') {
    return () => {}
  }

  const url = `${getApiBase()}/api/v1/events`
  const key = storedApiKey()
  if (key) {
    return subscribeEventsWithBearer(url, key, { onEvent, onError, onOpen })
  }
  const source = new EventSource(url)

  source.onopen = () => {
    onOpen?.()
  }

  source.onmessage = (message) => {
    const event = parseEventData(message.data)
    if (event) {
      onEvent(event)
    }
  }

  for (const type of KNOWN_EVENT_TYPES) {
    source.addEventListener(type, (message) => {
      const eventMessage = message as MessageEvent<string>
      const event = parseEventData(eventMessage.data)
      if (event) {
        onEvent(event)
      }
    })
  }

  source.onerror = (error) => {
    onError?.(error)
  }

  return () => {
    source.close()
  }
}

function subscribeEventsWithBearer(
  url: string,
  key: string,
  { onEvent, onError, onOpen }: EventSubscriptionOptions,
): () => void {
  const controller = new AbortController()
  void (async () => {
    try {
      const response = await fetch(url, {
        headers: {
          Accept: 'text/event-stream',
          Authorization: `Bearer ${key}`,
          [CLIENT_CONTRACT_HEADER]: CLIENT_CONTRACT,
        },
        signal: controller.signal,
      })
      if (!response.ok || !response.body) {
        onError?.(new Event('error'))
        return
      }
      onOpen?.()
      const reader = response.body.getReader()
      const decoder = new TextDecoder()
      let buffer = ''
      while (!controller.signal.aborted) {
        const { done, value } = await reader.read()
        if (done) break
        buffer += decoder.decode(value, { stream: true })
        const frames = buffer.split('\n\n')
        buffer = frames.pop() ?? ''
        for (const frame of frames) {
          const data = frame
            .split('\n')
            .filter((line) => line.startsWith('data:'))
            .map((line) => line.slice(5).trimStart())
            .join('\n')
          if (!data) continue
          const event = parseEventData(data)
          if (event) onEvent(event)
        }
      }
    } catch {
      if (!controller.signal.aborted) onError?.(new Event('error'))
    }
  })()
  return () => controller.abort()
}
