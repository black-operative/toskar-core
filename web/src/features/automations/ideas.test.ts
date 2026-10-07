import { afterEach, describe, expect, it } from 'vitest'
import { applyLanguage } from '@/i18n'
import { IDEAS, buildIdea, ideaDefaults, ideaReady, type IdeaId } from './ideas'
import { readNumber } from './number'

const idea = (id: IdeaId) => IDEAS.find((item) => item.id === id)!
const zone = 'America/Juneau'

afterEach(async () => {
  await applyLanguage('en')
})

// Templates fill in the form from a few fields (#204).
describe('templates', () => {
  it('make the automation each one describes', () => {
    const releases = buildIdea(idea('releases'), { ...ideaDefaults('releases'), software: 'Node.js' }, zone, readNumber)
    expect(releases.schedule).toMatchObject({ kind: 'weekly', weekdays: [5], hour: 9, minute: 0, time_zone: zone })
    expect(releases.name).toBe('Release notes: Node.js')

    const stock = buildIdea(idea('stock'), { url: 'https://www.shop.example/item' }, zone, readNumber)
    expect(stock.schedule).toMatchObject({ kind: 'interval', every_seconds: 21600 })
    expect(stock.notification).toEqual({ mode: 'condition', condition: { kind: 'available' } })
    expect(stock.name).toBe('Back in stock: shop.example')

    const folder = buildIdea(idea('folder'), { ...ideaDefaults('folder'), folder: '~/Documents/Invoices/' }, zone, readNumber)
    expect(folder.notification).toEqual({ mode: 'always' })
    expect(folder.trigger).toEqual({ kind: 'folder', path: '~/Documents/Invoices/' })
    expect(folder.name).toBe('Folder summary: Invoices')
    expect(folder.schedule).toMatchObject({ kind: 'daily', hour: 18 })
  })

  it('need their required fields', () => {
    expect(ideaReady(idea('price'), { ...ideaDefaults('price'), url: 'x' })).toBe(false)
    expect(ideaReady(idea('price'), { ...ideaDefaults('price'), url: 'x', price: '500' })).toBe(true)
    // The news brief starts with topics, so it's ready as it opens.
    expect(ideaReady(idea('news'), ideaDefaults('news'))).toBe(true)
  })

  it('write the task in the App language, with a threshold in its currency', async () => {
    await applyLanguage('de')
    const price = buildIdea(idea('price'), { ...ideaDefaults('price'), url: 'shop.example/x', price: '1.299,99' }, zone, readNumber)
    expect(price.prompt).toBe('Prüfe den aktuellen Preis des Produkts unter https://shop.example/x.')
    expect(price.notification).toEqual({ mode: 'condition', condition: { kind: 'threshold', op: 'below', value: 1299.99, currency: 'EUR' } })
  })
})
