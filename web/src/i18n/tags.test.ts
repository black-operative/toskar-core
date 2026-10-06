import { describe, expect, it } from 'vitest'

// <Trans> parses a string's tags with html-parse-stringify, which treats HTML
// void elements as empty: <link>Knowledge</link> renders an empty link and
// drops the word. Name the components something else, such as <go>.
const voidTags = /<(area|base|br|col|embed|hr|img|input|link|meta|param|source|track|wbr)>[^<]+<\/\1>/

const catalogFiles = import.meta.glob<string>('../../../i18n/locales/*/*.json', { eager: true, query: '?raw', import: 'default' })

function strings(value: unknown, at: string, out: [string, string][]) {
  if (typeof value === 'string') out.push([at, value])
  else if (value && typeof value === 'object') {
    for (const [key, v] of Object.entries(value)) strings(v, at ? `${at}.${key}` : key, out)
  }
}

describe('catalog tags', () => {
  it('never wrap text in an HTML void element', () => {
    const bad: string[] = []
    for (const [file, raw] of Object.entries(catalogFiles)) {
      const found: [string, string][] = []
      strings(JSON.parse(raw), '', found)
      for (const [key, text] of found) if (voidTags.test(text)) bad.push(`${file.replace('../../../i18n/locales/', '')} ${key}`)
    }
    expect(Object.keys(catalogFiles).length).toBeGreaterThan(50)
    expect(bad).toEqual([])
  })
})
