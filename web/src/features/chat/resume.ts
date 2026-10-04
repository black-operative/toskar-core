import type { LastChat } from '@/stores/uiStore'
import type { Message } from '@/types/api'
import { parseContextUsage, type ContextUsage } from './contextUsage'

/**
 * How long after leaving Chat it reopens the same chat. After longer, it
 * starts a new one, as when you come back to a conversation another day.
 * The iPhone app resumes its chats after the same 30 minutes.
 */
export const RESUME_WINDOW_MS = 30 * 60 * 1000

/** The chat to reopen when Chat opens, or null to start a new one. */
export function chatToResume(last: LastChat | null, now: number): string | null {
  if (!last?.id || !Number.isFinite(last.at)) return null
  return now - last.at < RESUME_WINDOW_MS ? last.id : null
}

/** The context reading saved with a chat's latest answer, for the gauge. */
export function lastContextUsage(messages: Message[] | null | undefined): ContextUsage | null {
  for (let i = (messages?.length ?? 0) - 1; i >= 0; i--) {
    const message = messages![i]
    if (message.role !== 'assistant') continue
    return parseContextUsage(message.meta?.context)
  }
  return null
}
