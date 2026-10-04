import { afterEach, describe, expect, it } from 'vitest'
import { readScreenshotLaunch, screenshotPath } from './screenshotMode'

describe('screenshotPath', () => {
  it('maps App Store screen ids onto existing routes', () => {
    expect(screenshotPath('chat')).toBe('/chat')
    expect(screenshotPath('models')).toBe('/models')
    expect(screenshotPath('computers')).toBe('/nodes')
    expect(screenshotPath('performance')).toBe('/performance')
    expect(screenshotPath('diagnostics')).toBe('/diagnostics')
    expect(screenshotPath('api-manager')).toBe('/api-access')
    expect(screenshotPath('automations')).toBe('/automations')
    expect(screenshotPath('automations-create')).toBe('/automations')
    expect(screenshotPath('unknown')).toBe('/chat')
  })
})

describe('readScreenshotLaunch', () => {
  const w = window as unknown as Record<string, unknown>
  afterEach(() => {
    delete w.__TOSKAR_SCREENSHOT__
    delete w.__YGGDRASIL_SCREENSHOT__
  })

  // The desktop shell sets __TOSKAR_SCREENSHOT__; one from before the
  // rename sets __YGGDRASIL_SCREENSHOT__ (#237).
  it('reads the shell’s boot setting under either name', () => {
    w.__TOSKAR_SCREENSHOT__ = { enabled: true, screen: 'models' }
    expect(readScreenshotLaunch()).toEqual({ enabled: true, screen: 'models', path: '/models' })
    delete w.__TOSKAR_SCREENSHOT__
    w.__YGGDRASIL_SCREENSHOT__ = { enabled: true, screen: 'computers' }
    expect(readScreenshotLaunch()).toEqual({ enabled: true, screen: 'computers', path: '/nodes' })
  })
})
