import i18n from '@/i18n'
import { formatNumber } from '@/i18n/format'

export type ContextUsage = {
  promptTokens: number
  limit: number
  instructions: number
  tools: number
  conversation: number
  toolResults: number
  estimated: boolean
  /** Older saved messages the model saw as a summary. */
  summarizedMessages?: number
  /** About how much memory the model's window reserves (its KV cache), when it ran on this computer. */
  memoryBytes?: number
}

/** Local llama.cpp starts at this window unless the model advertises a smaller one. */
export const DEFAULT_CONTEXT_WINDOW = 8192

export function contextWindow(catalog?: number): number {
  if (!catalog || catalog <= 0 || catalog > DEFAULT_CONTEXT_WINDOW) {
    return DEFAULT_CONTEXT_WINDOW
  }
  return catalog
}

export function parseContextUsage(raw: unknown): ContextUsage | null {
  if (!raw || typeof raw !== 'object') return null
  const record = raw as Record<string, unknown>
  const usage: ContextUsage = {
    promptTokens: numberField(record.prompt_tokens),
    limit: numberField(record.limit),
    instructions: numberField(record.instructions),
    tools: numberField(record.tools),
    conversation: numberField(record.conversation),
    toolResults: numberField(record.tool_results),
    estimated: record.estimated === true,
    summarizedMessages: numberField(record.summarized_messages),
    memoryBytes: numberField(record.memory_bytes),
  }
  if (usage.promptTokens <= 0 && usage.instructions + usage.tools + usage.conversation + usage.toolResults <= 0) {
    return null
  }
  return usage
}

function numberField(value: unknown): number {
  const n = typeof value === 'number' ? value : Number(value)
  if (!Number.isFinite(n) || n < 0) return 0
  return Math.round(n)
}

export function formatTokens(n: number): string {
  if (!Number.isFinite(n) || n < 1000) return formatNumber(Math.max(0, Math.round(n) || 0))
  // 12.5K in English, 12,5 Tsd. in German.
  return formatNumber(n, { notation: 'compact', maximumFractionDigits: n >= 100_000 ? 0 : 1 })
}

export function fillPercent(used: number, limit: number): number {
  if (limit <= 0 || used <= 0) return 0
  return Math.round((used / limit) * 100)
}

export type ContextRow = {
  id: 'instructions' | 'tools' | 'conversation' | 'toolResults'
  label: string
  tokens: number
}

export function contextRows(usage: ContextUsage): ContextRow[] {
  const rows: ContextRow[] = [
    { id: 'instructions', label: i18n.t('chat:context.rows.instructions'), tokens: usage.instructions },
    { id: 'tools', label: i18n.t('chat:context.rows.tools'), tokens: usage.tools },
    { id: 'conversation', label: i18n.t('chat:context.rows.conversation'), tokens: usage.conversation },
    { id: 'toolResults', label: i18n.t('chat:context.rows.toolResults'), tokens: usage.toolResults },
  ]
  return rows.filter((row) => row.tokens > 0)
}
