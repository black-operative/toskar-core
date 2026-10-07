import i18n from '@/i18n'
import type { AutomationNotification, AutomationSchedule, AutomationTrigger } from '@/types/api'

/**
 * Ready-made automations to start from (#204). Each is a template with a
 * few fields, such as a product link and a price, that fills in the form
 * directly: automations:ideas.<id> in the catalog has its title, a line on
 * what you get, its name and task with the fields in them, and an example
 * request to describe it in your own words instead.
 * internal/automations/request_test.go checks every language's example
 * requests read as intended.
 */
export type IdeaId = 'news' | 'price' | 'stock' | 'releases' | 'page' | 'folder'

export type IdeaField = 'topics' | 'url' | 'price' | 'currency' | 'software' | 'folder' | 'time' | 'weekday'

export interface Idea {
  id: IdeaId
  /** The fields the person fills in, in order; the first ones are required. */
  fields: IdeaField[]
  required: IdeaField[]
}

export const IDEAS: Idea[] = [
  { id: 'news', fields: ['topics', 'time'], required: ['topics'] },
  { id: 'price', fields: ['url', 'price', 'currency', 'time'], required: ['url', 'price'] },
  { id: 'stock', fields: ['url'], required: ['url'] },
  { id: 'releases', fields: ['software', 'weekday', 'time'], required: ['software'] },
  { id: 'page', fields: ['url', 'time'], required: ['url'] },
  { id: 'folder', fields: ['folder', 'time'], required: ['folder'] },
]

export type IdeaValues = Partial<Record<IdeaField, string>>

// A threshold in the App language's usual currency, as requests read one.
const LANGUAGE_CURRENCY: Record<string, string> = {
  en: 'USD', de: 'EUR', es: 'EUR', fr: 'EUR', it: 'EUR', ja: 'JPY', ko: 'KRW', 'pt-BR': 'BRL', 'zh-Hans': 'CNY', 'zh-Hant': 'TWD',
}

/** What a template's fields start as. */
export function ideaDefaults(id: IdeaId): IdeaValues {
  const language = i18n.resolvedLanguage ?? i18n.language
  const times: Record<IdeaId, string> = { news: '07:30', price: '08:00', stock: '', releases: '09:00', page: '18:00', folder: '18:00' }
  return {
    topics: id === 'news' ? i18n.t('automations:ideas.news.defaultTopics') : '',
    currency: LANGUAGE_CURRENCY[language] ?? 'USD',
    time: times[id],
    weekday: '5',
  }
}

/** Whether every required field has something in it. */
export function ideaReady(idea: Idea, values: IdeaValues): boolean {
  return idea.required.every((field) => (values[field] ?? '').trim() !== '')
}

export interface IdeaAutomation {
  name: string
  prompt: string
  schedule: AutomationSchedule
  notification: AutomationNotification
  /** What it watches, for a template that runs when something changed. */
  trigger?: AutomationTrigger
}

/** The automation a template's fields make, in the App language. */
export function buildIdea(idea: Idea, values: IdeaValues, timeZone: string, readPrice: (text: string) => number | null): IdeaAutomation {
  const url = withScheme((values.url ?? '').trim())
  const params = {
    url,
    site: siteOf(url),
    topics: (values.topics ?? '').trim(),
    software: (values.software ?? '').trim(),
    folder: (values.folder ?? '').trim(),
    folderName: baseName((values.folder ?? '').trim()),
  }
  const [hour, minute] = (values.time || '08:00').split(':').map(Number)
  const at = { hour: hour || 0, minute: minute || 0 }
  const daily: AutomationSchedule = { kind: 'daily', time_zone: timeZone, ...at, times: [at] }
  let schedule = daily
  let notification: AutomationNotification = { mode: 'always' }
  let trigger: AutomationTrigger | undefined
  switch (idea.id) {
    case 'price':
      notification = {
        mode: 'condition',
        condition: { kind: 'threshold', op: 'below', value: readPrice((values.price ?? '').trim()) ?? Number.NaN, currency: values.currency || 'USD' },
      }
      break
    case 'stock':
      schedule = { kind: 'interval', time_zone: timeZone, every_seconds: 6 * 3600 }
      notification = { mode: 'condition', condition: { kind: 'available' } }
      break
    case 'releases': {
      const day = Number(values.weekday ?? 5)
      schedule = { kind: 'weekly', time_zone: timeZone, ...at, times: [at], weekday: day, weekdays: [day] }
      break
    }
    case 'page':
      notification = { mode: 'change' }
      break
    case 'folder':
      // The run can't open files outside Toskar's workspace, so the folder
      // is watched: it runs, given what changed, only when something did.
      trigger = { kind: 'folder', path: params.folder }
      break
  }
  return {
    name: i18n.t(`automations:ideas.${idea.id}.name`, params),
    prompt: i18n.t(`automations:ideas.${idea.id}.task`, params),
    schedule,
    notification,
    trigger,
  }
}

function withScheme(url: string): string {
  if (!url || /^[a-z][a-z0-9+.-]*:\/\//i.test(url)) return url
  return `https://${url}`
}

function siteOf(url: string): string {
  try {
    return new URL(url).hostname.replace(/^www\./, '')
  } catch {
    return url
  }
}

function baseName(path: string): string {
  const parts = path.replace(/[\\/]+$/, '').split(/[\\/]/)
  return parts[parts.length - 1] || path
}
