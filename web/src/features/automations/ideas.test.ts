import { afterEach, describe, expect, it } from 'vitest'
import i18n, { applyLanguage, availableLanguages } from '@/i18n'
import { pseudoLocales } from '@/i18n/languages'
import { IDEAS } from './ideas'
import { parseAutomationRequest } from './parseRequest'

const zone = 'America/Los_Angeles'
const morning = new Date('2026-09-28T15:00:00Z')

afterEach(async () => {
  await applyLanguage('en')
})

// Every idea's request must fill in the form as intended, in every language.
describe('automation ideas', () => {
  for (const language of availableLanguages.filter((l) => !pseudoLocales.includes(l))) {
    for (const idea of IDEAS) {
      it(`${language}: ${idea.id}`, async () => {
        await applyLanguage(language)
        const request = i18n.t(`automations:ideas.${idea.id}.request`)
        const parsed = parseAutomationRequest(request, morning, zone)
        expect(parsed.schedule.kind).toBe(idea.expect.kind)
        expect(parsed.notification.mode).toBe(idea.expect.mode)
        if (idea.expect.condition) expect(parsed.notification.condition?.kind).toBe(idea.expect.condition)
        expect(parsed.notes).toEqual([])
      })
    }
  }
})
