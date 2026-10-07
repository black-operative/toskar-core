import { afterEach, describe, expect, it } from 'vitest'
import { applyLanguage } from '@/i18n'
import { civilToISO, notificationLabel, scheduleLabel, visibleTask } from './parseRequest'
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

// Several weekdays, several times a day, monthly, and cron (#204).
describe('schedule labels', () => {
  const tz = { time_zone: zone }
  it('names days and times as people say them', async () => {
    expect(scheduleLabel({ kind: 'daily', ...tz, times: [{ hour: 17, minute: 0 }, { hour: 8, minute: 0 }] })).toMatch(/^Every day at 8:00\sAM and 5:00\sPM$/)
    expect(scheduleLabel({ kind: 'weekly', ...tz, weekdays: [1, 2, 3, 4, 5], hour: 9 })).toMatch(/^Weekdays at 9:00\sAM$/)
    expect(scheduleLabel({ kind: 'weekly', ...tz, weekdays: [0, 6], hour: 10 })).toMatch(/^Weekends at 10:00\sAM$/)
    expect(scheduleLabel({ kind: 'weekly', ...tz, weekdays: [1, 3, 5], hour: 9 })).toMatch(/^Every Monday, Wednesday, and Friday at 9:00\sAM$/)
    // An older schedule with one day reads as before.
    expect(scheduleLabel({ kind: 'weekly', ...tz, weekday: 2, hour: 9 })).toMatch(/^Every Tuesday at 9:00\sAM$/)
    expect(scheduleLabel({ kind: 'monthly', ...tz, month_day: 15, hour: 9 })).toMatch(/^Monthly on day 15 at 9:00\sAM$/)
    expect(scheduleLabel({ kind: 'cron', ...tz, cron: '0 9 * * 1-5' })).toBe('Custom schedule: 0 9 * * 1-5')
    await applyLanguage('de')
    expect(scheduleLabel({ kind: 'weekly', ...tz, weekdays: [1, 3], hour: 9 })).toBe('Jeden Montag und Mittwoch um 9:00')
  })
})
