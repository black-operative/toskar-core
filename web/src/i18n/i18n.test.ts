import { afterEach, describe, expect, it } from 'vitest'
import i18n, { applyLanguage, availableLanguages, resolveLanguage } from './index'
import {
  formatDate,
  formatDateTime,
  formatDecimal,
  formatGigabytes,
  formatMilliseconds,
  formatNumber,
  formatPercent,
  formatPrice,
  formatRelativeTime,
  formatSize,
  formatTokensPerSecond,
} from './format'
import { formatTokens } from '@/features/chat/contextUsage'
import { directionOf } from './languages'
import { pseudoLocalize } from './pseudo'

afterEach(async () => {
  await applyLanguage('en')
  localStorage.clear()
})

describe('resolveLanguage', () => {
  const available = ['en', 'es', 'de', 'pt-BR', 'zh-Hant']

  it('prefers the saved App language', () => {
    expect(resolveLanguage('de', ['es-MX'], available)).toBe('de')
  })

  it('follows the system languages, by exact tag or base language', () => {
    expect(resolveLanguage('', ['es-MX', 'en'], available)).toBe('es') // es-MX → es
    expect(resolveLanguage('', ['fr-FR', 'de-AT'], available)).toBe('de') // first one with a catalog
    expect(resolveLanguage('', ['pt-BR'], available)).toBe('pt-BR')
    expect(resolveLanguage('', ['zh-Hant'], available)).toBe('zh-Hant')
  })

  it('matches Chinese by script and Portuguese by region', () => {
    const tier1 = ['en', 'pt-BR', 'zh-Hans', 'zh-Hant']
    expect(resolveLanguage('', ['zh-TW'], tier1)).toBe('zh-Hant')
    expect(resolveLanguage('', ['zh-HK'], tier1)).toBe('zh-Hant')
    expect(resolveLanguage('', ['zh-CN'], tier1)).toBe('zh-Hans')
    expect(resolveLanguage('', ['zh'], tier1)).toBe('zh-Hans')
    expect(resolveLanguage('', ['pt-PT'], tier1)).toBe('pt-BR')
    expect(resolveLanguage('', ['pt'], tier1)).toBe('pt-BR')
    // Simplified Chinese readers don't get Traditional, or the other way round.
    expect(resolveLanguage('', ['zh-CN'], ['en', 'zh-Hant'])).toBe('en')
  })

  it('shows each catalog in its own language, including region and script tags', async () => {
    for (const language of availableLanguages) {
      await applyLanguage(language)
      expect(i18n.resolvedLanguage).toBe(language)
    }
  })

  it('ends at English', () => {
    expect(resolveLanguage('', ['fr-FR'], available)).toBe('en')
    expect(resolveLanguage('', [], available)).toBe('en')
    expect(resolveLanguage('ja', ['fr'], available)).toBe('en') // a saved language with no catalog yet
  })

  it('keeps the pseudo-locale', () => {
    expect(resolveLanguage('en-XA', ['de'], available)).toBe('en-XA')
  })
})

describe('the pseudo-locale', () => {
  it('accents, lengthens, and brackets text but keeps placeholders and markup', () => {
    const out = pseudoLocalize('Models for {{name}} <b>now</b>')
    expect(out).toMatch(/^\[!! .* !!\]$/)
    expect(out).toContain('{{name}}')
    expect(out).toContain('<b>')
    expect(out).toContain('</b>')
    expect(out).not.toContain('Models')
    expect(out.length).toBeGreaterThan('Models for {{name}} <b>now</b>'.length * 1.2)
  })

  it('applies to every string when chosen', async () => {
    await applyLanguage('en-XA')
    expect(i18n.t('nav.models')).toBe(pseudoLocalize('Models'))
    expect(i18n.t('subsystems.connected', { count: 3 })).toBe(pseudoLocalize('3 connected'))
    expect(document.documentElement.lang).toBe('en-XA')
  })
})

describe('the page', () => {
  it('is marked with the language and its direction', async () => {
    await applyLanguage('en')
    expect(document.documentElement.lang).toBe('en')
    expect(document.documentElement.dir).toBe('ltr')
  })

  it('knows right-to-left languages, even without a catalog', () => {
    for (const tag of ['ar', 'he-IL', 'fa', 'ur-PK']) expect(directionOf(tag), tag).toBe('rtl')
    for (const tag of ['en', 'ja', 'de-AT']) expect(directionOf(tag), tag).toBe('ltr')
  })

  it('remembers the App language for the next start, and forgets it for the system default', async () => {
    await applyLanguage('en')
    expect(localStorage.getItem('ygg.ui_locale')).toBe('en')
    await applyLanguage('')
    expect(localStorage.getItem('ygg.ui_locale')).toBeNull()
  })
})

describe('plurals', () => {
  it('use plural rules, not count === 1', () => {
    expect(i18n.t('chat:steps.correctedCode', { count: 1 })).toBe('Checked the code and fixed 1 error')
    expect(i18n.t('chat:steps.correctedCode', { count: 2 })).toBe('Checked the code and fixed 2 errors')
    expect(i18n.t('chat:steps.correctedCode', { count: 0 })).toBe('Checked the code and fixed 0 errors')
  })
})

// Intl separates a number from its unit with a no-break space in some locales.
const spaced = (text: string) => text.replace(/\s/g, ' ')

describe('formatting', () => {
  const when = new Date(Date.UTC(2026, 8, 30, 20, 0))

  it('follows the locale asked for, even before its text is translated', async () => {
    await applyLanguage('nl')
    expect(i18n.language).toBe('en') // no Dutch catalog yet: the text stays English
    expect(document.documentElement.lang).toBe('en')
    expect(formatNumber(1234.56)).toBe('1.234,56')
    expect(spaced(formatSize(1.5 * 1024 ** 3))).toBe('1,5 GB')
    await applyLanguage('en')
    expect(formatNumber(1234.56)).toBe('1,234.56')
    expect(spaced(formatSize(1.5 * 1024 ** 3))).toBe('1.5 GB')
    expect(spaced(formatSize(512))).toBe('512 byte')
  })

  it('formats dates and times', async () => {
    await applyLanguage('de-DE')
    expect(formatDate(when, { dateStyle: 'medium', timeZone: 'UTC' })).toBe('30.09.2026')
    await applyLanguage('en-US')
    expect(formatDate(when, { dateStyle: 'medium', timeZone: 'UTC' })).toBe('Sep 30, 2026')
    expect(formatDateTime(when)).toMatch(/2026/)
  })

  it('formats relative time', async () => {
    await applyLanguage('en')
    const now = new Date(Date.UTC(2026, 8, 30, 20, 0))
    expect(formatRelativeTime(new Date(now.getTime() - 5 * 60_000), now)).toBe('5 minutes ago')
    expect(formatRelativeTime(new Date(now.getTime() + 2 * 86_400_000), now)).toBe('in 2 days')
    expect(formatRelativeTime(new Date(now.getTime() - 86_400_000), now)).toBe('yesterday')
  })

  it('formats the pseudo-locale as English', async () => {
    await applyLanguage('en-XA')
    expect(formatNumber(1234.5)).toBe('1,234.5')
  })

  it('formats decimals, sizes, speeds, durations, prices, and percentages for the locale', async () => {
    await applyLanguage('de')
    expect(formatDecimal(0.1234, 3)).toBe('0,123')
    expect(spaced(formatGigabytes(15.84))).toBe('16 GB')
    expect(spaced(formatGigabytes(7.84))).toBe('7,8 GB')
    expect(spaced(formatSize(1_500_000_000, { base: 1000 }))).toBe('1,5 GB')
    expect(spaced(formatPercent(0.45))).toBe('45 %')
    expect(formatTokens(12_500)).toBe('12.500') // German has no short form for thousands
    await applyLanguage('en')
    expect(formatDecimal(2, 1)).toBe('2.0')
    expect(formatMilliseconds(250)).toBe('250 ms')
    expect(formatMilliseconds(1234)).toBe('1.2 s')
    expect(formatTokensPerSecond(12.34)).toBe('12.3 tok/s')
    expect(formatPrice(20)).toBe('$20')
    expect(formatPrice(19.5)).toBe('$19.50')
    expect(formatPercent(0.45)).toBe('45%')
    expect(formatTokens(12_500)).toBe('12.5K')
    expect(formatTokens(950)).toBe('950')
  })

  it('formats numbers in text, but not ports and other labels', async () => {
    await applyLanguage('de')
    expect(i18n.t('knowledge:row.passages', { count: 1234 })).toContain('1.234')
    await applyLanguage('en')
    expect(i18n.t('knowledge:row.passages', { count: 1234 })).toContain('1,234')
    expect(i18n.t('knowledge:row.passages', { count: 1 })).not.toContain('passages')
    expect(i18n.t('apiAccess:service.lanHint', { port: 7331 })).toContain(':7331/v1')
  })

  it('says how long ago, narrowly where space is tight', async () => {
    await applyLanguage('en')
    const now = new Date(Date.UTC(2026, 8, 30, 20, 0))
    expect(formatRelativeTime(new Date(now.getTime() - 5 * 60_000), now, 'narrow')).toBe('5m ago')
    await applyLanguage('de')
    expect(formatRelativeTime(new Date(now.getTime() - 5 * 60_000), now)).toBe('vor 5 Minuten')
  })
})
