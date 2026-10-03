import { fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { describe, expect, it } from 'vitest'
import { rovingKeyDown, useMenu } from './roving'

function Tabs() {
  const [tab, setTab] = useState('a')
  return (
    <div role="tablist" aria-label="Views" onKeyDown={rovingKeyDown}>
      {['a', 'b', 'c'].map((id) => (
        <button key={id} role="tab" aria-selected={tab === id} tabIndex={tab === id ? 0 : -1} onClick={() => setTab(id)}>
          {id.toUpperCase()}
        </button>
      ))}
    </div>
  )
}

function Menu() {
  const [open, setOpen] = useState(false)
  const ref = useMenu(open, () => setOpen(false))
  return (
    <>
      <button aria-expanded={open} onClick={() => setOpen(true)}>
        More
      </button>
      {open ? (
        <div ref={ref} role="menu" onKeyDown={rovingKeyDown}>
          <button role="menuitem">Rename</button>
          <button role="menuitem">Delete</button>
        </div>
      ) : null}
    </>
  )
}

describe('rovingKeyDown', () => {
  it('moves and chooses tabs with the arrow keys, wrapping, and Home and End', () => {
    render(<Tabs />)
    const [a, b, c] = screen.getAllByRole('tab')
    a.focus()
    fireEvent.keyDown(a, { key: 'ArrowRight' })
    expect(document.activeElement).toBe(b)
    expect(b).toHaveAttribute('aria-selected', 'true')
    fireEvent.keyDown(b, { key: 'End' })
    expect(c).toHaveAttribute('aria-selected', 'true')
    fireEvent.keyDown(c, { key: 'ArrowRight' })
    expect(a).toHaveAttribute('aria-selected', 'true')
    expect(screen.getAllByRole('tab').map((t) => t.tabIndex)).toEqual([0, -1, -1])
  })
})

describe('useMenu', () => {
  it('focuses the first item, moves with arrows, and closes on Escape back to its button', () => {
    render(<Menu />)
    const more = screen.getByRole('button', { name: 'More' })
    more.focus()
    fireEvent.click(more)
    expect(document.activeElement).toBe(screen.getByRole('menuitem', { name: 'Rename' }))
    fireEvent.keyDown(document.activeElement!, { key: 'ArrowDown' })
    expect(document.activeElement).toBe(screen.getByRole('menuitem', { name: 'Delete' }))
    fireEvent.keyDown(document.activeElement!, { key: 'Escape' })
    expect(screen.queryByRole('menu')).toBeNull()
    expect(document.activeElement).toBe(more)
  })
})
