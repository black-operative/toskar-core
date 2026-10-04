import { afterEach, describe, expect, it } from 'vitest'
import { applyLanguage } from '@/i18n'
import type { Model } from '@/types/api'
import { languageLevelFor, languageSummary } from './modelPresentation'

afterEach(async () => {
  await applyLanguage('en')
  localStorage.clear()
})

const qwen: Pick<Model, 'languages'> = {
  languages: [
    { language: 'en', level: 'excellent', confidence: 'high', sources: ['model_card'] },
    { language: 'zh-Hans', level: 'excellent', confidence: 'high', sources: ['model_card'] },
    { language: 'de', level: 'good', confidence: 'medium', sources: ['model_card', 'maintainer'] },
    { language: 'pt', level: 'good', confidence: 'medium', sources: ['model_card', 'maintainer'] },
  ],
}

describe('model languages', () => {
  it('leads with the App language, and counts the rest', () => {
    expect(languageSummary(qwen, 'en')?.text).toBe('English: Excellent · 3 more languages')
    expect(languageSummary(qwen, 'de')?.text).toBe('German: Good · 3 more languages')
    const title = languageSummary(qwen, 'en')?.title ?? ''
    expect(title).toContain('German: Good · medium confidence · model card, Toskar maintainers')
  })

  it('matches another region, and Chinese only by script', () => {
    expect(languageLevelFor(qwen, 'pt-BR')?.language).toBe('pt')
    expect(languageLevelFor(qwen, 'zh-Hans')?.level).toBe('excellent')
    expect(languageLevelFor(qwen, 'zh-Hant')).toBeUndefined()
    expect(languageSummary(qwen, 'ko')?.text).toBe('Not rated for Korean · 4 more languages')
  })

  it('says nothing for a model without levels, and treats pseudo-locales as English', () => {
    expect(languageSummary({}, 'de')).toBeNull()
    expect(languageSummary(qwen, 'en-XA')?.text).toContain('Excellent')
  })

  it('names languages in the App language', async () => {
    await applyLanguage('de')
    expect(languageSummary(qwen, 'de')?.text).toMatch(/^Deutsch: /)
  })
})
