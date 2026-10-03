import { fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { describe, expect, it } from 'vitest'
import { useDialog } from './useDialog'

function Example() {
  const [open, setOpen] = useState(false)
  const ref = useDialog(open, () => setOpen(false))
  return (
    <>
      <button onClick={() => setOpen(true)}>Delete chat</button>
      {open ? (
        <div ref={ref} role="dialog" aria-modal="true" aria-label="Delete this chat?">
          <button onClick={() => setOpen(false)}>Delete</button>
          <button data-autofocus onClick={() => setOpen(false)}>
            Cancel
          </button>
        </div>
      ) : null}
    </>
  )
}

describe('useDialog', () => {
  it('moves focus to the safe choice, keeps Tab inside, closes on Escape, and returns focus', () => {
    render(<Example />)
    const opener = screen.getByRole('button', { name: 'Delete chat' })
    opener.focus()
    fireEvent.click(opener)

    const cancel = screen.getByRole('button', { name: 'Cancel' })
    const confirm = screen.getByRole('button', { name: 'Delete' })
    expect(document.activeElement).toBe(cancel)

    // Tab from the last control wraps to the first, and Shift+Tab back.
    fireEvent.keyDown(document.activeElement!, { key: 'Tab' })
    expect(document.activeElement).toBe(confirm)
    fireEvent.keyDown(document.activeElement!, { key: 'Tab', shiftKey: true })
    expect(document.activeElement).toBe(cancel)

    fireEvent.keyDown(document.activeElement!, { key: 'Escape' })
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(document.activeElement).toBe(opener)
  })
})
