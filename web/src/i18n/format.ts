import i18n, { requestedLocale } from './index'
import { directionOf, pseudoLocales, sourceLanguage } from './languages'

// Locale-aware formatting through Intl (multilingual spec §8). Never build
// dates, numbers, or plurals by hand: German writes 1.234,56 and 30.09.2026.

/**
 * The locale Intl formats in: the one the person asked for, even before its
 * text is translated, so a German system shows 30.09.2026 with English text.
 * The en-XA pseudo-locale formats as English.
 */
export function formatLocale(): string {
  return pseudoLocales.includes(i18n.language) ? sourceLanguage : requestedLocale()
}

/**
 * Joins steps that run in order, such as Planner → Worker, with an arrow that
 * points the way the App language reads.
 */
export function formatSequence(steps: readonly string[]): string {
  return steps.join(directionOf(i18n.language) === 'rtl' ? ' ← ' : ' → ')
}

/** Joins items as the App language lists them: "8:00 AM and 5:00 PM". */
export function formatList(items: readonly string[]): string {
  return new Intl.ListFormat(formatLocale(), { type: 'conjunction' }).format(items)
}

export function formatNumber(value: number, options?: Intl.NumberFormatOptions): string {
  return new Intl.NumberFormat(formatLocale(), options).format(value)
}

/** A number with exactly this many decimals, such as 0.125 or 0,125: for losses, rates, and epochs. */
export function formatDecimal(value: number, digits: number): string {
  return formatNumber(value, { minimumFractionDigits: digits, maximumFractionDigits: digits })
}

/** A duration in milliseconds: 250 ms under a second, 1.2 s from there. */
export function formatMilliseconds(ms: number): string {
  if (ms < 1000) return i18n.t('common:units.ms', { value: formatNumber(ms, { maximumFractionDigits: 0 }) })
  return i18n.t('common:units.seconds', { value: formatDecimal(ms / 1000, 1) })
}

/** A generation speed: 12.5 tok/s, or 150 tok/s from 100 up. */
export function formatTokensPerSecond(n: number): string {
  return i18n.t('common:units.tokPerSec', { value: formatDecimal(n, n >= 100 ? 0 : 1) })
}

export function formatPercent(fraction: number, maximumFractionDigits = 0): string {
  return formatNumber(fraction, { style: 'percent', maximumFractionDigits })
}

export function formatCurrency(value: number, currency: string): string {
  return formatNumber(value, { style: 'currency', currency })
}

/** A price an automation watches: $20, or 19,99 € with cents. Without a currency it is in dollars. */
export function formatPrice(value: number, currency = 'USD'): string {
  const fraction = Number.isInteger(value) ? 0 : 2
  return formatNumber(value, { style: 'currency', currency: currency || 'USD', minimumFractionDigits: fraction, maximumFractionDigits: fraction })
}

/** A currency's name in the App language, such as Euro, or its code when there is none. */
export function currencyName(code: string): string {
  try {
    return new Intl.DisplayNames([formatLocale()], { type: 'currency' }).of(code) ?? code
  } catch {
    return code
  }
}

function asDate(value: Date | string | number): Date {
  return value instanceof Date ? value : new Date(value)
}

/** A date such as Sep 30, 2026 (en-US) or 30.09.2026 (de-DE). */
export function formatDate(value: Date | string | number, options: Intl.DateTimeFormatOptions = { dateStyle: 'medium' }): string {
  return new Intl.DateTimeFormat(formatLocale(), options).format(asDate(value))
}

/** A date and time such as Sep 30, 2026, 8:00 PM (en-US) or 30.09.2026, 20:00 (de-DE). */
export function formatDateTime(value: Date | string | number): string {
  return formatDate(value, { dateStyle: 'medium', timeStyle: 'short' })
}

export function formatTime(value: Date | string | number): string {
  return formatDate(value, { timeStyle: 'short' })
}

const relativeSteps: [Intl.RelativeTimeFormatUnit, number][] = [
  ['second', 60],
  ['minute', 60],
  ['hour', 24],
  ['day', 7],
  ['week', 4.34524],
  ['month', 12],
  ['year', Number.POSITIVE_INFINITY],
]

/**
 * Time from now, such as "5 minutes ago" or "in 2 days", in the UI language.
 * The narrow style is for tight spaces: "5m ago".
 */
export function formatRelativeTime(
  value: Date | string | number,
  now: Date = new Date(),
  style: Intl.RelativeTimeFormatStyle = 'long',
): string {
  let amount = (asDate(value).getTime() - now.getTime()) / 1000
  const formatter = new Intl.RelativeTimeFormat(formatLocale(), { numeric: 'auto', style })
  for (const [unit, size] of relativeSteps) {
    if (Math.abs(amount) < size) return formatter.format(Math.round(amount), unit)
    amount /= size
  }
  return formatter.format(Math.round(amount), 'year')
}

const sizeUnits = ['byte', 'kilobyte', 'megabyte', 'gigabyte', 'terabyte'] as const

/**
 * A size in bytes, such as 1.5 GB or 1,5 GB, with the number and unit in the
 * UI's format. Sizes count in 1024s; `base: 1000` counts in 1000s, as model
 * downloads are sized.
 */
export function formatSize(bytes: number, { base = 1024 }: { base?: 1000 | 1024 } = {}): string {
  let value = bytes
  let unit = 0
  while (Math.abs(value) >= base && unit < sizeUnits.length - 1) {
    value /= base
    unit += 1
  }
  return formatNumber(value, { style: 'unit', unit: sizeUnits[unit], unitDisplay: 'short', maximumFractionDigits: unit === 0 ? 0 : 1 })
}

/** A size already in gigabytes, such as memory reported as 15.8 GB; whole numbers from 10 GB. */
export function formatGigabytes(gb: number): string {
  return formatNumber(gb, { style: 'unit', unit: 'gigabyte', unitDisplay: 'short', maximumFractionDigits: Math.abs(gb) >= 10 ? 0 : 1 })
}
