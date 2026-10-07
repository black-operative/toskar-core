import type { RequestWords } from './types'

// The words live in the shared catalog, i18n/requests/<language>.json, so the
// daemon reads requests with the same words (#204).
const files = import.meta.glob<RequestWords>('../../../../../i18n/requests/*.json', { eager: true, import: 'default' })

export type { Daypart, RequestWords } from './types'

/** The languages automation requests can be written in, by catalog code. */
export const requestWords: Record<string, RequestWords> = Object.fromEntries(
  Object.entries(files).map(([file, words]) => [file.replace(/^.*\/(.+)\.json$/, '$1'), words]),
)

const en = requestWords.en

/** The words for a language tag: an exact match, then one with the same base language (pt → pt-BR). */
export function requestWordsFor(language: string): RequestWords | undefined {
  const tag = language.toLowerCase()
  const exact = Object.keys(requestWords).find((code) => code.toLowerCase() === tag)
  if (exact) return requestWords[exact]
  const base = tag.split('-')[0]
  const sameBase = Object.keys(requestWords).find((code) => code.toLowerCase().split('-')[0] === base)
  return sameBase ? requestWords[sameBase] : undefined
}

/**
 * The words to read a request with, in order: the App language's, then
 * English, which everyone can write in.
 */
export function requestLanguages(language: string): RequestWords[] {
  const own = requestWordsFor(language)
  return own && own !== en ? [own, en] : [en]
}
