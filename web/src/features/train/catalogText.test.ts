import { afterEach, describe, expect, it } from 'vitest'
import i18n, { applyLanguage } from '@/i18n'
import { capabilityLabel } from '@/features/profiles/capabilities'
import { purposeLabel, strategyLabel } from '@/features/profiles/profilePresentation'
import { flagInfo, formatDuration, stateLabel, stepLabel } from './display'

afterEach(async () => {
  await applyLanguage('en')
  localStorage.clear()
})

const pseudo = /^\[!! .* !!\]$/

describe('knowledge, train, profiles, and diagnostics text from the catalog', () => {
  it('uses plural forms', () => {
    expect(i18n.t('diagnostics:abilities.models', { count: 1 })).toBe('1 model installed')
    expect(i18n.t('diagnostics:abilities.models', { count: 2 })).toBe('2 models installed')
    expect(i18n.t('diagnostics:rows.runtime.running', { count: 1 })).toBe('1 model running')
    expect(i18n.t('diagnostics:rows.bifrost.offline', { count: 3 })).toBe('3 computers offline')
  })

  it('leaves ids the catalog lacks as they are', () => {
    expect(purposeLabel('coding')).not.toBe('coding')
    expect(purposeLabel('astronomy')).toBe('astronomy')
  })

  it('follows the App language', async () => {
    await applyLanguage('en-XA')
    expect(stateLabel('training')).toMatch(pseudo)
    expect(stepLabel('plan')).toMatch(pseudo)
    expect(flagInfo('duplicate')?.label).toMatch(pseudo)
    expect(formatDuration(90)).toMatch(pseudo)
    expect(capabilityLabel('internet')).toMatch(pseudo)
    expect(strategyLabel({ orchestrator_id: 'team' }, false).name).toMatch(pseudo)
    expect(i18n.t('knowledge:page.title')).toMatch(pseudo)
    expect(i18n.t('diagnostics:page.normal')).toMatch(pseudo)
  })
})
