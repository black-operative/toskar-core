import i18n from '@/i18n'
import { isDesktopShell, openExternal } from '@/lib/desktopBridge'
import type { MCPServer, MCPSpec } from '@/types/api'

export const SOURCES_KEY = ['mcp-servers'] as const

export function errorText(err: unknown): string {
  return err instanceof Error ? err.message : i18n.t('tools:status.generic')
}

/**
 * Opens a service's sign-in. A window opened during the click is reused, so
 * the browser does not block it; otherwise a new one is opened. The desktop
 * app opens it in the person's browser, and the source list notices when it
 * finishes.
 */
export function openSignIn(url: string, popup?: Window | null) {
  if (isDesktopShell()) {
    openExternal(url)
    return
  }
  if (popup && !popup.closed) {
    popup.location.href = url
    return
  }
  window.open(url, 'toskar-sign-in', 'width=520,height=720')
}

/** A one-line summary of how a server is reached. */
export function specSummary(spec: Pick<MCPSpec, 'command' | 'args' | 'url'>): string {
  if (spec.url) return spec.url
  return [spec.command, ...(spec.args ?? []).map((a) => (a === '' ? '••••' : a))].join(' ')
}

/** Lines of KEY=value or Name: value into a map. */
export function linesToMap(text: string, sep: '=' | ':'): Record<string, string> {
  const out: Record<string, string> = {}
  for (const line of text.split('\n')) {
    const i = line.indexOf(sep)
    if (i <= 0) continue
    out[line.slice(0, i).trim()] = line.slice(i + 1).trim()
  }
  return out
}

/** Quotes an argument that has spaces, so splitArgs reads it back whole. */
export function quoteArg(a: string): string {
  return /\s/.test(a) ? `"${a}"` : a
}

/** Splits a command line on spaces, keeping quoted parts together. */
export function splitArgs(line: string): string[] {
  const out: string[] = []
  const re = /"([^"]*)"|'([^']*)'|(\S+)/g
  let m: RegExpExecArray | null
  while ((m = re.exec(line)) !== null) out.push(m[1] ?? m[2] ?? m[3])
  return out
}

export type Tone = 'ok' | 'warn' | 'bad' | 'idle'

/** What a source's status line says, in plain words. */
export function statusOf(s: MCPServer): { text: string; tone: Tone } {
  if (s.status === 'off') return { text: i18n.t('tools:status.off'), tone: 'idle' }
  if (s.status === 'sign_in') return { text: i18n.t('tools:status.signIn'), tone: 'warn' }
  if (s.status === 'error') {
    const text = s.error ? i18n.t('tools:status.needsAttentionError', { error: s.error }) : i18n.t('tools:status.needsAttention')
    return { text, tone: 'bad' }
  }
  if (s.missing) return { text: i18n.t('tools:status.needsRuntime', { runtime: s.missing }), tone: 'warn' }
  const tools = i18n.t('tools:status.tools', { count: s.tools.length })
  if (s.where === 'remote') return { text: i18n.t('tools:status.readyRemote', { tools }), tone: 'ok' }
  return { text: i18n.t(s.running ? 'tools:status.running' : 'tools:status.readyLocal', { tools }), tone: 'ok' }
}

export const toneClass: Record<Tone, string> = {
  ok: 'bg-success',
  warn: 'bg-warning',
  bad: 'bg-danger',
  idle: 'bg-ink-faint',
}
