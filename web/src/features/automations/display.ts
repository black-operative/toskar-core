import i18n from '@/i18n'
import { formatDate, formatPrice } from '@/i18n/format'
import type { AutomationRun } from '@/types/api'
import { formatWhen } from './parseRequest'

export interface NoticeExplanation {
  title: string
  detail: string
}

// The computer decides whether a run notifies and records why (#204); the
// page shows its decision, notify_detail with notify_values, rather than
// reading the result again. Runs from before it recorded one say only
// whether they notified.
const CONDITION_NOT_MET = new Set(['notAbove', 'notBelow', 'notAvailable', 'notSignificant'])

export function explainRun(run: Pick<AutomationRun, 'status' | 'notification_sent' | 'notify_detail' | 'notify_values'>): NoticeExplanation {
  const sent = run.notification_sent ? i18n.t('automations:notice.notified') : i18n.t('automations:notice.notNotified')
  const key = run.notify_detail
  if (!key || run.status === 'failed' || run.status === 'retrying') {
    return { title: sent, detail: '' }
  }
  const values = run.notify_values ?? {}
  const currency = typeof values.currency === 'string' ? values.currency : undefined
  const params: Record<string, string> = {}
  for (const name of ['price', 'amount'] as const) {
    if (typeof values[name] === 'number') params[name] = formatPrice(values[name], currency)
  }
  return {
    title: CONDITION_NOT_MET.has(key) ? i18n.t('automations:notice.conditionNotMet') : sent,
    detail: i18n.t(`automations:notice.${key}`, params),
  }
}

export function compactWhen(iso: string | undefined, timeZone: string): string {
  if (!iso) return i18n.t('automations:time.notScheduled')
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return i18n.t('automations:time.notScheduled')
  return formatDate(date, {
    timeZone: timeZone || 'UTC',
    month: 'short',
    day: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
  })
}

export function runTiming(run: Pick<AutomationRun, 'occurrence_at' | 'started_at' | 'finished_at'>, timeZone: string): string {
  const when = compactWhen(run.started_at || run.occurrence_at, timeZone)
  if (!run.started_at || !run.finished_at) return when
  const ms = new Date(run.finished_at).getTime() - new Date(run.started_at).getTime()
  if (!Number.isFinite(ms) || ms < 0) return when
  return i18n.t('automations:time.withDuration', { when, duration: durationLabel(ms) })
}

export function clockDetail(iso: string | undefined, timeZone: string): string {
  return formatWhen(iso, timeZone)
}

function durationLabel(ms: number): string {
  const total = Math.max(0, Math.round(ms / 1000))
  const minutes = Math.floor(total / 60)
  const seconds = total % 60
  if (minutes === 0) return i18n.t('automations:time.seconds', { s: seconds })
  if (seconds === 0) return i18n.t('automations:time.minutes', { m: minutes })
  return i18n.t('automations:time.minutesSeconds', { m: minutes, s: seconds })
}
