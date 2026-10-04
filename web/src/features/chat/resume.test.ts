import { describe, expect, it } from 'vitest'
import type { Message } from '@/types/api'
import { RESUME_WINDOW_MS, chatToResume, lastContextUsage } from './resume'

const now = Date.UTC(2026, 9, 3, 12)

describe('chatToResume', () => {
  it('reopens the chat left a little while ago', () => {
    expect(chatToResume({ id: 'c1', at: now - 5 * 60 * 1000 }, now)).toBe('c1')
  })

  it('starts a new chat after being away a long time', () => {
    expect(chatToResume({ id: 'c1', at: now - RESUME_WINDOW_MS }, now)).toBeNull()
    expect(chatToResume({ id: 'c1', at: now - 3 * 60 * 60 * 1000 }, now)).toBeNull()
  })

  it('starts a new chat when none was open', () => {
    expect(chatToResume(null, now)).toBeNull()
    expect(chatToResume({ id: '', at: now }, now)).toBeNull()
  })
})

function message(role: string, context?: Record<string, unknown>): Message {
  return { id: `${role}-${Math.random()}`, conversation_id: 'c1', role, content: 'x', created_at: '', meta: context ? { context } : undefined } as Message
}

describe('lastContextUsage', () => {
  it("reads the reading saved with the chat's latest answer", () => {
    const usage = lastContextUsage([
      message('assistant', { prompt_tokens: 100, limit: 8192 }),
      message('user'),
      message('assistant', { prompt_tokens: 2400, limit: 8192, conversation: 2000, memory_bytes: 1 << 30 }),
      message('user'),
    ])
    expect(usage?.promptTokens).toBe(2400)
    expect(usage?.memoryBytes).toBe(1 << 30)
  })

  it('has none for an answer saved before readings were kept', () => {
    expect(lastContextUsage([message('assistant', { prompt_tokens: 100, limit: 8192 }), message('assistant')])).toBeNull()
    expect(lastContextUsage([])).toBeNull()
    expect(lastContextUsage(undefined)).toBeNull()
  })
})
