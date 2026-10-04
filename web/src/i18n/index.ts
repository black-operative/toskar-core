import i18n, { type FormatterModule, type Resource } from 'i18next'
import { notifyDesktopLanguage } from '@/lib/desktopBridge'
import { moveStored } from '@/lib/storage'
import { initReactI18next } from 'react-i18next'
import { directionOf, languages, pseudoLocale, pseudoLocales, pseudoRtlLocale, sourceLanguage } from './languages'
import { pseudoLocalize, pseudoRtlLocalize } from './pseudo'

// Yggdrasil's UI text comes from the shared catalog in i18n/locales at the
// repository root (multilingual spec §3–7): one folder per language, one JSON
// file per namespace. English is the source; a key missing in a language
// falls back through its base language to English (es-MX → es → en).

const files = import.meta.glob<Record<string, unknown>>('../../../i18n/locales/*/*.json', {
  eager: true,
  import: 'default',
})

/** The catalog as i18next resources: language → namespace → keys. */
export function catalogResources(): Resource {
  const resources: Resource = {}
  for (const [path, keys] of Object.entries(files)) {
    const match = /locales\/([^/]+)\/([^/]+)\.json$/.exec(path)
    if (!match) continue
    const [, language, namespace] = match
    resources[language] = { ...(resources[language] ?? {}), [namespace]: keys }
  }
  return resources
}

const resources = catalogResources()

/** Placeholders whose numbers are labels, not amounts: shown as they are, never as 7,331. */
const rawNumbers = new Set(['port', 'code', 'id', 'version', 'revision', 'commit', 'year'])

/**
 * Numbers in text, such as {{count}} or {{total}}, in the UI's number format
 * (multilingual spec §8): 1,234 in English, 1.234 in German. Plural forms
 * still choose by the number itself.
 */
const numberFormatter: FormatterModule = {
  type: 'formatter',
  init() {},
  add() {},
  addCached() {},
  format(value, _format, _lng, options) {
    // Anything but a number passes through as it is.
    if (typeof value !== 'number' || rawNumbers.has(String(options?.interpolationkey ?? ''))) return value
    const locale = pseudoLocales.includes(i18n.language) ? sourceLanguage : requestedLocale()
    return new Intl.NumberFormat(locale).format(value)
  },
}

/** Languages that have a catalog, which the App language can be set to. */
export const availableLanguages = Object.keys(resources)

/** Where the last applied language is kept, so the next start shows it before settings load. */
const storedKey = 'toskar.ui_locale'
moveStored('ygg.ui_locale', storedKey)

function readStored(): string {
  try {
    return localStorage.getItem(storedKey) ?? ''
  } catch {
    return ''
  }
}

/** A tag's script, such as Hant for zh-TW, or '' when Intl can't tell. */
function scriptOf(tag: string): string {
  try {
    return new Intl.Locale(tag).maximize().script ?? ''
  } catch {
    return ''
  }
}

/**
 * Picks the language to show. A saved App language wins; otherwise the first
 * of the system's languages that has a catalog, matched exactly, by base
 * language (de-AT → de), by script (zh-TW → zh-Hant), or by another region in
 * the same script (pt-PT → pt-BR, never Simplified for Traditional Chinese);
 * otherwise English.
 */
export function resolveLanguage(saved: string, system: readonly string[], available: readonly string[] = availableLanguages): string {
  if (pseudoLocales.includes(saved)) return saved
  const lower = available.map((a) => a.toLowerCase())
  const match = (tag: string): string | null => {
    const exact = lower.indexOf(tag.toLowerCase())
    if (exact >= 0) return available[exact]
    const language = tag.split('-')[0].toLowerCase()
    const base = lower.indexOf(language)
    if (base >= 0) return available[base]
    const script = scriptOf(tag)
    const byScript = lower.indexOf(`${language}-${script.toLowerCase()}`)
    if (script && byScript >= 0) return available[byScript]
    const sibling = available.find((a) => a.split('-')[0].toLowerCase() === language && scriptOf(a) === script)
    return sibling ?? null
  }
  if (saved) {
    const found = match(saved)
    if (found) return found
  }
  for (const tag of system) {
    const found = match(tag)
    if (found) return found
  }
  return sourceLanguage
}

/** The browser's or operating system's preferred languages. */
export function systemLanguages(): string[] {
  if (typeof navigator === 'undefined') return []
  if (navigator.languages?.length) return [...navigator.languages]
  return navigator.language ? [navigator.language] : []
}

/** A tag Intl can format in, or '' for one it can't. */
function canonical(tag: string): string {
  try {
    return Intl.getCanonicalLocales(tag)[0] ?? ''
  } catch {
    return ''
  }
}

// The locale the person asked for: the App language, or the system's first
// language. Text falls back to English while a language has no catalog, but
// Intl formats dates and numbers in any locale, so they follow this.
let requested = canonical(readStored()) || canonical(systemLanguages()[0] ?? '') || sourceLanguage

/** The locale dates, numbers, and sizes are formatted in. */
export function requestedLocale(): string {
  return requested
}

/** Marks the page with its language and direction, for screen readers and RTL layout. */
function markDocument(language: string) {
  if (typeof document === 'undefined') return
  document.documentElement.lang = language
  document.documentElement.dir = directionOf(language)
}

void i18n
  .use(initReactI18next)
  .use(numberFormatter)
  .use({
    type: 'postProcessor',
    name: 'pseudo',
    process: (value: string) =>
      i18n.language === pseudoLocale ? pseudoLocalize(value) : i18n.language === pseudoRtlLocale ? pseudoRtlLocalize(value) : value,
  })
  .init({
    resources,
    lng: resolveLanguage(readStored(), systemLanguages()),
    // Every language, and the en-XA pseudo-locale, ends at English; i18next
    // tries the base language first (es-MX → es).
    fallbackLng: sourceLanguage,
    // With nonExplicitSupportedLngs, i18next checks a tag's base language,
    // so pt-BR and zh-Hans need pt and zh listed or they fall back to English.
    supportedLngs: [...new Set([...availableLanguages, ...availableLanguages.map((code) => code.split('-')[0]), ...pseudoLocales])],
    nonExplicitSupportedLngs: true,
    defaultNS: 'common',
    ns: [...new Set(Object.values(resources).flatMap((r) => Object.keys(r)))],
    postProcess: ['pseudo'],
    // React escapes. Every number is formatted for the locale; see numberFormatter.
    interpolation: { escapeValue: false, alwaysFormat: true },
    returnNull: false,
    // Never show a raw key: missing text falls back to English, and the
    // catalog tests catch keys English lacks.
    parseMissingKeyHandler: (key) => {
      if (import.meta.env.DEV) console.warn(`Missing translation: ${key}`)
      const last = key.split(/[.:]/).pop() ?? key
      return last.replace(/([a-z])([A-Z])/g, '$1 $2').replace(/^./, (c) => c.toUpperCase())
    },
    react: { useSuspense: false },
  })

markDocument(i18n.language)
i18n.on('languageChanged', markDocument)
// The desktop shell's menus and tray follow the page's language.
void notifyDesktopLanguage(i18n.language)
i18n.on('languageChanged', (language) => void notifyDesktopLanguage(language))

/** Shows the UI in a language now, and remembers it for the next start. */
export async function applyLanguage(saved: string): Promise<void> {
  const language = resolveLanguage(saved, systemLanguages())
  requested = canonical(saved) || canonical(systemLanguages()[0] ?? '') || sourceLanguage
  try {
    if (saved) localStorage.setItem(storedKey, saved)
    else localStorage.removeItem(storedKey)
  } catch {
    // Storage can be unavailable; the daemon still keeps the setting.
  }
  if (i18n.language !== language) await i18n.changeLanguage(language)
}

export { languages, pseudoLocale, pseudoRtlLocale, directionOf }
export default i18n
