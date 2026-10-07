import { describe, expect, it } from 'vitest'
import { explainRun } from './display'

// The computer decides whether a run notifies and records why; the page
// shows its decision (#204).
describe('explainRun', () => {
  it('says when a price missed the condition', () => {
    const notice = explainRun({
      status: 'succeeded',
      notification_sent: false,
      notify_detail: 'notBelow',
      notify_values: { price: 640, amount: 500, currency: 'USD' },
    })
    expect(notice).toEqual({ title: 'Condition not met', detail: '$640 is not below $500.' })
  })

  it('says when a price matched', () => {
    const notice = explainRun({ status: 'succeeded', notification_sent: true, notify_detail: 'priceBelow', notify_values: { price: 420, amount: 500, currency: 'USD' } })
    expect(notice).toEqual({ title: 'Notified', detail: 'The price is below $500.' })
  })

  it("follows the computer's change decision", () => {
    expect(explainRun({ status: 'succeeded', notification_sent: false, notify_detail: 'unchanged' })).toEqual({ title: 'Not notified', detail: 'The result did not change.' })
    expect(explainRun({ status: 'succeeded', notification_sent: true, notify_detail: 'changed' })).toEqual({ title: 'Notified', detail: 'The result changed.' })
    expect(explainRun({ status: 'succeeded', notification_sent: false, notify_detail: 'firstSaved' })).toEqual({ title: 'Not notified', detail: 'The first result is saved for comparison.' })
  })

  it('says only whether an older run notified', () => {
    expect(explainRun({ status: 'succeeded', notification_sent: true })).toEqual({ title: 'Notified', detail: '' })
    expect(explainRun({ status: 'failed', notification_sent: false, notify_detail: 'noPrice' })).toEqual({ title: 'Not notified', detail: '' })
  })
})
