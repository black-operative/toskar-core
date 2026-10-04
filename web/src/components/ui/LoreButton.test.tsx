import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it } from 'vitest'
import i18n from '@/i18n'
import { realms } from '@/lib/realms'
import { RealmKicker } from './Realm'
import { Ratatoskr } from './Ratatoskr'
import { YggdrasilMark } from './YggdrasilMark'

function kicker(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <RealmKicker />
    </MemoryRouter>,
  )
}

describe('lore', () => {
  it('opens a page title’s lore entry on click', () => {
    kicker('/knowledge')
    const trigger = screen.getByRole('button', { name: /Mimir/ })
    expect(trigger).toHaveAttribute('aria-expanded', 'false')
    fireEvent.click(trigger)
    expect(trigger).toHaveAttribute('aria-expanded', 'true')
    const bubble = screen.getByRole('dialog', { name: 'About Mimir' })
    expect(bubble).toHaveTextContent('well of wisdom')
    expect(bubble).toHaveTextContent('In Toskar')
    expect(bubble).toHaveTextContent(i18n.t('lore:realms.knowledge.meaning'))
  })

  it('closes with Escape and gives focus back', () => {
    kicker('/memory')
    const trigger = screen.getByRole('button', { name: /Muninn/ })
    fireEvent.click(trigger)
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(trigger).toHaveFocus()
  })

  it('closes when clicking elsewhere', () => {
    kicker('/nodes')
    fireEvent.click(screen.getByRole('button', { name: /Bifrost/ }))
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    fireEvent.pointerDown(document.body)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('names the chat page after Ratatoskr, the messenger', () => {
    kicker('/chat')
    fireEvent.click(screen.getByRole('button', { name: /Ratatoskr/ }))
    expect(screen.getByRole('dialog', { name: 'About Ratatoskr' })).toHaveTextContent('squirrel')
  })

  it('has lore in the catalog for every realm', () => {
    for (const realm of Object.values(realms)) {
      for (const field of ['meaning', 'story', 'rune']) {
        expect(i18n.exists(`lore:realms.${realm.id}.${field}`), `${realm.id}.${field}`).toBe(true)
      }
    }
  })

  it('opens the mascot’s and the logo’s lore', () => {
    render(
      <>
        <Ratatoskr state="idle" size={48} />
        <YggdrasilMark size={36} lore />
        <YggdrasilMark size={36} />
      </>,
    )
    fireEvent.click(screen.getByRole('button', { name: 'About Ratatoskr' }))
    expect(screen.getByRole('dialog', { name: 'About Ratatoskr' })).toHaveTextContent('carrying your requests')
    fireEvent.click(screen.getByRole('button', { name: 'About the Toskar mark' }))
    expect(screen.getByRole('dialog', { name: 'About Yggdrasil' })).toHaveTextContent('World Tree')
    // Only the mark asked for lore is a button.
    expect(screen.getAllByRole('button', { name: /^About/ })).toHaveLength(2)
  })
})
