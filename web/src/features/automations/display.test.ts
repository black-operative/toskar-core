import { describe, expect, it } from 'vitest'
import { explainRun } from './display'

describe('explainRun', () => {
  it('says when a price missed the condition', () => {
    const notice = explainRun(
      { mode: 'condition', condition: { kind: 'threshold', op: 'below', value: 500 } },
      { status: 'succeeded', result: 'The listing is $640.\n{"price": 640}', notification_sent: false },
      undefined,
    )
    expect(notice.title).toBe('Condition not met')
    expect(notice.detail).toBe('$640 is not below $500.')
  })

  it('says when a price matched', () => {
    const notice = explainRun(
      { mode: 'condition', condition: { kind: 'threshold', op: 'below', value: 500 } },
      { status: 'succeeded', result: 'The listing is $420.\n{"price": 420}', notification_sent: true },
      undefined,
    )
    expect(notice).toEqual({ title: 'Notified', detail: 'The price is below $500.' })
  })

  it('treats a plain in-stock answer as available', () => {
    const notice = explainRun(
      { mode: 'condition', condition: { kind: 'available' } },
      {
        status: 'succeeded',
        result: 'Based on the information available, Nintendo Switch 2 is in stock at Costco. Bundles are listed on their website, or check in-store.',
        notification_sent: false,
      },
      undefined,
    )
    expect(notice).toEqual({ title: 'Not notified', detail: 'It is in stock.' })
  })

  // The computer decides what changed by the values, what it read, and the
  // model's judgment, not the wording; the page follows its decision (#204).
  it("follows the computer's change decision, not the wording", () => {
    const run = { status: 'succeeded' as const, result: 'The page lists 3 jobs.', notification_sent: false }
    expect(explainRun({ mode: 'change' }, run, 'There are three jobs listed.')).toEqual({ title: 'Not notified', detail: 'The result did not change.' })
    expect(explainRun({ mode: 'change' }, { ...run, notification_sent: true }, 'There are two jobs listed.')).toEqual({ title: 'Notified', detail: 'The result changed.' })
    expect(explainRun({ mode: 'change' }, run, undefined)).toEqual({ title: 'Not notified', detail: 'The first result is saved for comparison.' })
  })
})
