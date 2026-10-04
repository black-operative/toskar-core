import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import { AppLayout } from './AppLayout'

vi.mock('@/lib/api', () => ({
  api: {
    getHealth: vi.fn(async () => ({ status: 'ok', product: 'yggdrasil', version: 'test' })),
    getNodes: vi.fn(async () => []),
    getModels: vi.fn(async () => []),
    listNotifications: vi.fn(async () => []),
    markNotificationsRead: vi.fn(),
    dismissNotification: vi.fn(),
  },
}))

function renderAt(path: string) {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route element={<AppLayout />}>
            <Route path="/models" element={<h1>Models page</h1>} />
            <Route path="/memory" element={<h1>Memory page</h1>} />
          </Route>
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('AppLayout', () => {
  it('names the page in the window title', async () => {
    renderAt('/models')
    await waitFor(() => expect(document.title).toBe('Models · Toskar'))
  })

  it('opens the sidebar as a drawer from the menu button, and closes it after a page is chosen or on Escape', async () => {
    renderAt('/models')
    const menu = screen.getByRole('button', { name: 'Open menu' })
    const sidebar = document.getElementById('app-sidebar')!
    expect(menu).toHaveAttribute('aria-expanded', 'false')
    expect(sidebar.dataset.open).toBe('false')

    // As from the keyboard: the button has focus when it is pressed.
    menu.focus()
    fireEvent.click(menu)
    expect(menu).toHaveAttribute('aria-expanded', 'true')
    expect(sidebar.dataset.open).toBe('true')
    expect(document.activeElement).toHaveAttribute('href', '/models')

    fireEvent.keyDown(document.activeElement!, { key: 'Escape' })
    expect(sidebar.dataset.open).toBe('false')
    expect(document.activeElement).toBe(menu)

    fireEvent.click(menu)
    fireEvent.click(screen.getByRole('link', { name: /Memory/ }))
    expect(await screen.findByText('Memory page')).toBeInTheDocument()
    expect(sidebar.dataset.open).toBe('false')
  })
})
