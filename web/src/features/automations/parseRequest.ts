import i18n from '@/i18n'
import { formatDate, formatList, formatPrice } from '@/i18n/format'
import type { AutomationClockTime, AutomationCondition, AutomationNotification, AutomationSchedule, AutomationTrigger } from '@/types/api'
// Labels and prompt helpers for automations. Requests are read on the
// computer (POST /api/v1/automations/parse, #204), with the words in
// i18n/requests; what this shows, such as schedules, is in the App language.

/** A weekday's name in the App language: 0 is Sunday. */
export function weekdayName(index: number): string {
  return i18n.t(`automations:weekdays.${index}`)
}

// The result instructions older pages stored in prompts; the computer adds
// them when a run starts now (#204), and these show the task without them.
const PRICE_INSTRUCTION_LINE = /Include a JSON object in the result with the numeric price(?: in [A-Z]{3})?, for example \{"price": 420\}\./g
const AVAILABLE_INSTRUCTION =
  'Include a JSON object in the result, {"available": true} when the item is available and {"available": false} when it is not.'
const SIGNIFICANT_INSTRUCTION =
  'Include a JSON object in the result, {"significant": true} when this is worth a notification and {"significant": false} when it is not.'

export function localTimeZone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
}

export function scheduleLabel(schedule: AutomationSchedule): string {
  switch (schedule.kind) {
    case 'once':
      return schedule.at
        ? i18n.t('automations:schedule.onceAt', { when: formatWhen(schedule.at, schedule.time_zone) })
        : i18n.t('automations:schedule.once')
    case 'daily':
      return i18n.t('automations:schedule.daily', { time: timesLabel(schedule) })
    case 'weekly': {
      const days = scheduleWeekdays(schedule)
      const time = timesLabel(schedule)
      const key = days.join(',')
      if (key === '0,1,2,3,4,5,6') return i18n.t('automations:schedule.daily', { time })
      if (key === '1,2,3,4,5') return i18n.t('automations:schedule.weekdaysOnly', { time })
      if (key === '0,6') return i18n.t('automations:schedule.weekends', { time })
      if (days.length === 1) return i18n.t('automations:schedule.weekly', { day: weekdayName(days[0]), time })
      return i18n.t('automations:schedule.weeklyDays', { days: formatList(days.map(weekdayName)), time })
    }
    case 'monthly':
      return i18n.t('automations:schedule.monthly', { day: schedule.month_day ?? 1, time: timesLabel(schedule) })
    case 'cron':
      return i18n.t('automations:schedule.cron', { cron: schedule.cron ?? '' })
    case 'interval':
      return intervalLabel(schedule.every_seconds ?? 0)
    default:
      return i18n.t('automations:schedule.scheduled')
  }
}

/** When an automation runs: its schedule, or what it watches and how often it checks (#204). */
export function whenLabel(item: { schedule: AutomationSchedule; trigger?: AutomationTrigger }): string {
  const schedule = scheduleLabel(item.schedule)
  const trigger = item.trigger
  const target = trigger?.kind === 'folder' ? trigger.path : trigger?.url
  if (!trigger?.kind || !target) return schedule
  if (trigger.kind === 'folder') {
    const name = target.replace(/[\\/]+$/, '').split(/[\\/]/).pop() || target
    return i18n.t('automations:trigger.folder', { name, schedule })
  }
  return i18n.t(`automations:trigger.${trigger.kind}`, { site: siteOf(target), schedule })
}

function siteOf(url: string): string {
  try {
    return new URL(url).hostname.replace(/^www\./, '')
  } catch {
    return url
  }
}

export function notificationLabel(notification: AutomationNotification): string {
  switch (notification.mode) {
    case 'none':
      return i18n.t('automations:notify.none')
    case 'change':
      return i18n.t('automations:notify.change')
    case 'condition':
      return conditionLabel(notification.condition)
    default:
      return i18n.t('automations:notify.always')
  }
}

export function formatWhen(iso: string | undefined, timeZone: string): string {
  if (!iso) return i18n.t('automations:time.notScheduled')
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return i18n.t('automations:time.notScheduled')
  return formatDate(date, { timeZone: timeZone || 'UTC', dateStyle: 'medium', timeStyle: 'short' })
}

export function civilInputValue(iso: string | undefined, timeZone: string): string {
  if (!iso) return ''
  const parts = zonedParts(new Date(iso), timeZone)
  const month = String(parts.month).padStart(2, '0')
  const day = String(parts.day).padStart(2, '0')
  const hour = String(parts.hour).padStart(2, '0')
  const minute = String(parts.minute).padStart(2, '0')
  return `${parts.year}-${month}-${day}T${hour}:${minute}`
}

export function civilToISO(value: string, timeZone: string): string {
  const match = value.match(/^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})/)
  if (!match) {
    throw new Error(i18n.t('automations:parse.chooseDateTime'))
  }
  return instantInZone(
    Number(match[1]),
    Number(match[2]),
    Number(match[3]),
    Number(match[4]),
    Number(match[5]),
    timeZone,
  ).toISOString()
}

// visibleTask removes the machine-readable result instruction from a stored prompt.
export function visibleTask(prompt: string): string {
  let text = prompt.trim()
  text = text.replace(PRICE_INSTRUCTION_LINE, '')
  for (const line of [AVAILABLE_INSTRUCTION, SIGNIFICANT_INSTRUCTION]) {
    text = text.split(line).join('')
  }
  return text.replace(/\n{3,}/g, '\n\n').trim()
}

export function resultProse(result: string | undefined): string {
  if (!result) return ''
  const withoutInstruction = visibleTask(result)
  return withoutInstruction
    .replace(/```json[\s\S]*?```/g, '')
    .replace(/\{[^{}]*"(?:price|available|significant)"[^{}]*\}/g, '')
    .replace(/\n{3,}/g, '\n\n')
    .trim()
}

function formatAmount(value: number, currency: string | undefined): string {
  return formatPrice(value, currency)
}

function conditionLabel(condition: AutomationCondition | undefined): string {
  if (!condition) return i18n.t('automations:notify.condition')
  if (condition.kind === 'threshold') {
    return i18n.t(condition.op === 'above' ? 'automations:notify.priceAbove' : 'automations:notify.priceBelow', {
      amount: formatAmount(condition.value ?? 0, condition.currency),
    })
  }
  if (condition.kind === 'available') return i18n.t('automations:notify.available')
  return i18n.t('automations:notify.significant')
}

function intervalLabel(seconds: number): string {
  if (seconds <= 0) return i18n.t('automations:schedule.onInterval')
  if (seconds % 86400 === 0) {
    const days = seconds / 86400
    return days === 1 ? i18n.t('automations:schedule.everyDay') : i18n.t('automations:schedule.days', { count: days })
  }
  if (seconds % 3600 === 0) {
    const hours = seconds / 3600
    return hours === 1 ? i18n.t('automations:schedule.everyHour') : i18n.t('automations:schedule.hours', { count: hours })
  }
  if (seconds % 60 === 0) {
    const minutes = seconds / 60
    return minutes === 1 ? i18n.t('automations:schedule.everyMinute') : i18n.t('automations:schedule.minutes', { count: minutes })
  }
  return i18n.t('automations:schedule.seconds', { count: seconds })
}

/** A time of day in the App language's clock: 6:30 PM in English, 18:30 in German. */
/** A schedule's times of day, sorted; older schedules have one, in hour and minute. */
export function scheduleTimes(schedule: AutomationSchedule): AutomationClockTime[] {
  const times = schedule.times?.length ? schedule.times : [{ hour: schedule.hour ?? 0, minute: schedule.minute ?? 0 }]
  return [...times].sort((a, b) => a.hour * 60 + a.minute - (b.hour * 60 + b.minute))
}

/** A weekly schedule's days, sorted; older schedules have one, in weekday. */
export function scheduleWeekdays(schedule: AutomationSchedule): number[] {
  const days = schedule.weekdays?.length ? schedule.weekdays : [schedule.weekday ?? 1]
  return [...new Set(days)].sort((a, b) => a - b)
}

function timesLabel(schedule: AutomationSchedule): string {
  return formatList(scheduleTimes(schedule).map((t) => clockLabel(t.hour, t.minute)))
}

function clockLabel(hour: number, minute: number): string {
  return formatDate(new Date(2000, 0, 1, hour, minute), { hour: 'numeric', minute: '2-digit' })
}

interface CivilParts {
  year: number
  month: number
  day: number
  hour: number
  minute: number
}

function zonedParts(date: Date, timeZone: string): CivilParts {
  const fmt = new Intl.DateTimeFormat('en-US', {
    timeZone,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
  })
  const parts = Object.fromEntries(fmt.formatToParts(date).map((part) => [part.type, part.value]))
  return {
    year: Number(parts.year),
    month: Number(parts.month),
    day: Number(parts.day),
    hour: Number(parts.hour) % 24,
    minute: Number(parts.minute),
  }
}

function instantInZone(year: number, month: number, day: number, hour: number, minute: number, timeZone: string): Date {
  const utcGuess = Date.UTC(year, month - 1, day, hour, minute)
  const observed = zonedParts(new Date(utcGuess), timeZone)
  const observedUTC = Date.UTC(observed.year, observed.month - 1, observed.day, observed.hour, observed.minute)
  return new Date(utcGuess - (observedUTC - utcGuess))
}
