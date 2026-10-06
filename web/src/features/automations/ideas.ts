import type { AutomationNotification, AutomationSchedule } from '@/types/api'

/**
 * Ready-made automations to start from, for people who aren't sure what to
 * use them for. Each is automations:ideas.<id> in the catalog: a title, a line
 * on what you get, and a request the form's parser reads in that language.
 * `expect` is what the request must turn into; ideas.test.ts checks it in
 * every language.
 */
export const IDEAS: {
  id: 'news' | 'price' | 'stock' | 'releases' | 'page'
  expect: { kind: AutomationSchedule['kind']; mode: AutomationNotification['mode']; condition?: string }
}[] = [
  { id: 'news', expect: { kind: 'daily', mode: 'always' } },
  { id: 'price', expect: { kind: 'daily', mode: 'condition', condition: 'threshold' } },
  { id: 'stock', expect: { kind: 'interval', mode: 'condition', condition: 'available' } },
  { id: 'releases', expect: { kind: 'weekly', mode: 'always' } },
  { id: 'page', expect: { kind: 'daily', mode: 'change' } },
]
