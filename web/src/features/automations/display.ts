import i18n from '@/i18n'
import { formatDate, formatPrice } from '@/i18n/format'
import type { AutomationNotification, AutomationRun } from '@/types/api'
import { formatWhen } from './parseRequest'

export interface NoticeExplanation {
  title: string
  detail: string
}

export function explainRun(
  notification: AutomationNotification,
  run: Pick<AutomationRun, 'status' | 'result' | 'notification_sent'>,
  previousResult: string | undefined,
  previousNotified = false,
): NoticeExplanation {
  if (run.status === 'failed' || run.status === 'retrying') {
    return { title: run.notification_sent ? i18n.t('automations:notice.notified') : i18n.t('automations:notice.notNotified'), detail: '' }
  }
  if (notification.mode === 'none') {
    return { title: i18n.t('automations:notice.notNotified'), detail: i18n.t('automations:notice.storesResult') }
  }
  if (notification.mode === 'always') {
    return { title: run.notification_sent ? i18n.t('automations:notice.notified') : i18n.t('automations:notice.notNotified'), detail: '' }
  }
  if (notification.mode === 'change') {
    if (!previousResult) {
      return { title: i18n.t('automations:notice.notNotified'), detail: i18n.t('automations:notice.firstSaved') }
    }
    // The computer decides what changed, by the values, what it read, and
    // the model's judgment, not the wording (#204); its decision is whether
    // it notified.
    if (!run.notification_sent) {
      return { title: i18n.t('automations:notice.notNotified'), detail: i18n.t('automations:notice.unchanged') }
    }
    return { title: i18n.t('automations:notice.notified'), detail: i18n.t('automations:notice.changed') }
  }
  return explainCondition(notification, run.result, previousResult, previousNotified, run.notification_sent)
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

function explainCondition(
  notification: AutomationNotification,
  result: string | undefined,
  previous: string | undefined,
  previousNotified: boolean,
  notified: boolean,
): NoticeExplanation {
  const condition = notification.condition
  const signal = readSignal(result)
  if (!condition || condition.kind === 'threshold') {
    const currency = condition?.currency
    const amount = formatAmount(condition?.value ?? 0, currency)
    const above = condition?.op === 'above'
    if (signal.price == null) {
      return { title: notified ? i18n.t('automations:notice.notified') : i18n.t('automations:notice.notNotified'), detail: notified ? '' : i18n.t('automations:notice.noPrice') }
    }
    const matched = above ? signal.price > (condition?.value ?? 0) : signal.price < (condition?.value ?? 0)
    if (!matched) {
      return {
        title: i18n.t('automations:notice.conditionNotMet'),
        detail: i18n.t(above ? 'automations:notice.notAbove' : 'automations:notice.notBelow', { price: formatAmount(signal.price, currency), amount }),
      }
    }
    return {
      title: notified ? i18n.t('automations:notice.notified') : i18n.t('automations:notice.notNotified'),
      detail: i18n.t(above ? 'automations:notice.priceAbove' : 'automations:notice.priceBelow', { amount }),
    }
  }
  if (condition.kind === 'available') {
    const available = itemAvailable(result)
    if (available !== true) {
      return { title: i18n.t('automations:notice.conditionNotMet'), detail: i18n.t('automations:notice.notAvailable') }
    }
    if (previousNotified && itemAvailable(previous) === true) {
      return { title: i18n.t('automations:notice.notNotified'), detail: i18n.t('automations:notice.alreadyAvailable') }
    }
    return { title: notified ? i18n.t('automations:notice.notified') : i18n.t('automations:notice.notNotified'), detail: i18n.t('automations:notice.inStock') }
  }
  if (signal.significant !== true) {
    return { title: i18n.t('automations:notice.conditionNotMet'), detail: i18n.t('automations:notice.notSignificant') }
  }
  return { title: notified ? i18n.t('automations:notice.notified') : i18n.t('automations:notice.notNotified'), detail: i18n.t('automations:notice.significant') }
}

function itemAvailable(result: string | undefined): boolean | undefined {
  const signal = readSignal(result)
  if (signal.available != null) return signal.available
  return inferAvailable(result)
}

function inferAvailable(result: string | undefined): boolean | undefined {
  if (!result) return undefined
  let positive = false
  let negative = false
  for (const sentence of result.split(/[.!?\n]/)) {
    const line = sentence.trim().toLowerCase()
    if (!line) continue
    if (availabilityDenied(line)) {
      negative = true
      continue
    }
    if (availabilityStated(line)) positive = true
  }
  if (positive && !negative) return true
  if (negative && !positive) return false
  return undefined
}

function availabilityDenied(line: string): boolean {
  return [
    'out of stock',
    'not in stock',
    "isn't in stock",
    'is not in stock',
    'sold out',
    'unavailable',
    'not available',
    "isn't available",
    'is not available',
    'no longer available',
  ].some((phrase) => line.includes(phrase))
}

function availabilityStated(line: string): boolean {
  if (line.includes('if ') || line.includes('whether ') || line.includes('unable') || line.includes('cannot') || line.includes('could not') || line.includes("can't")) {
    return false
  }
  return line.includes('in stock') || line.includes('back in stock') || line.includes('now available') || line.includes('is available') || line.includes('are available')
}

function readSignal(result: string | undefined): { price?: number; available?: boolean; significant?: boolean } {
  if (!result) return {}
  const matches = result.match(/\{[^{}]*"(?:price|available|significant)"[^{}]*\}/g) ?? []
  let price: number | undefined
  let available: boolean | undefined
  let significant: boolean | undefined
  for (const raw of matches) {
    try {
      const body = JSON.parse(raw) as { price?: unknown; available?: unknown; significant?: unknown }
      if (typeof body.price === 'number') price = body.price
      if (typeof body.available === 'boolean') available = body.available
      if (typeof body.significant === 'boolean') significant = body.significant
    } catch {
      continue
    }
  }
  return { price, available, significant }
}

function formatAmount(value: number, currency: string | undefined): string {
  return formatPrice(value, currency)
}

function durationLabel(ms: number): string {
  const total = Math.max(0, Math.round(ms / 1000))
  const minutes = Math.floor(total / 60)
  const seconds = total % 60
  if (minutes === 0) return i18n.t('automations:time.seconds', { s: seconds })
  if (seconds === 0) return i18n.t('automations:time.minutes', { m: minutes })
  return i18n.t('automations:time.minutesSeconds', { m: minutes, s: seconds })
}
