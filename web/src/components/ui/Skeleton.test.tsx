import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { Skeleton } from './Skeleton'

describe('Skeleton', () => {
  it('announces what is loading once, and hides its shapes from screen readers', () => {
    const { container } = render(<Skeleton label="Loading memories…" count={4} />)
    const status = screen.getByRole('status')
    expect(status).toHaveAttribute('aria-busy', 'true')
    expect(status).toHaveTextContent('Loading memories…')
    expect(container.querySelector('[aria-hidden]')?.querySelectorAll('li')).toHaveLength(4)
  })

  it('draws a conversation for chats', () => {
    const { container } = render(<Skeleton label="Loading this chat…" shape="chat" count={2} />)
    // A question bubble, then a reply drawn as plain lines.
    const turns = container.querySelectorAll('[aria-hidden] > div')
    expect(turns).toHaveLength(2)
    expect(turns[0].querySelector('.rounded-2xl')).not.toBeNull()
    expect(turns[1].querySelector('.rounded-2xl')).toBeNull()
  })
})
