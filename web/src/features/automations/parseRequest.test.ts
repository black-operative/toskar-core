import { afterEach, describe, expect, it } from 'vitest'
import { applyLanguage } from '@/i18n'
import { civilToISO, notificationLabel, visibleTask } from './parseRequest'
import { readNumber } from './number'

// Requests are read on the computer (internal/automations/request_test.go
// has every language's cases, #204); these cover what the page still does.

const zone = 'America/Los_Angeles'

afterEach(async () => {
  await applyLanguage('en')
  localStorage.clear()
})

describe('civil times', () => {
  it('converts a civil time in Pacific time to the matching UTC instant', () => {
    expect(civilToISO('2026-01-15T09:00', zone)).toBe('2026-01-15T17:00:00.000Z')
  })
})

describe('prices', () => {
  it('reads numbers as people write them', () => {
    expect(readNumber('1,299.99')).toBe(1299.99)
    expect(readNumber('1.299,99')).toBe(1299.99)
    expect(readNumber('1 299,99')).toBe(1299.99)
    expect(readNumber('2.500')).toBe(2500)
    expect(readNumber('4.99')).toBe(4.99)
    expect(readNumber('19,5')).toBe(19.5)
    expect(readNumber('about 5')).toBeNull()
  })

  it('shows the task without an instruction an older page stored', () => {
    expect(visibleTask('Prüfe dieses Produkt.\n\nInclude a JSON object in the result with the numeric price in EUR, for example {"price": 420}.')).toBe('Prüfe dieses Produkt.')
  })

  it('shows a threshold in its currency, and an older one without a currency in dollars', async () => {
    expect(notificationLabel({ mode: 'condition', condition: { kind: 'threshold', op: 'below', value: 500 } })).toBe('Notify when the price is below $500')
    await applyLanguage('de')
    expect(notificationLabel({ mode: 'condition', condition: { kind: 'threshold', op: 'below', value: 500, currency: 'EUR' } })).toMatch(/500\s€/)
  })
})
