import { act, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { applyLanguage } from '@/i18n'
import { pseudoLocalize } from '@/i18n/pseudo'
import { LifecycleHost } from './LifecycleHost'

type Handler = (...data: unknown[]) => void
const w = window as unknown as { go?: unknown; runtime?: unknown }

afterEach(async () => {
  delete w.go
  delete w.runtime
  await applyLanguage('en')
  localStorage.clear()
})

describe('LifecycleHost', () => {
  it('shows the closing overlay in the UI language, whatever the shell said', async () => {
    let send: Handler = () => {}
    w.runtime = { EventsOn: (_: string, h: Handler) => ((send = h), () => {}) }
    await applyLanguage('en-XA')
    render(<LifecycleHost>page</LifecycleHost>)
    act(() => send({ state: 'stopping', message: 'Stopping local AI service…' }))
    expect(screen.getByText(pseudoLocalize('Closing Toskar'))).toBeInTheDocument()
    expect(screen.getByText(pseudoLocalize('Stopping local AI service…'))).toBeInTheDocument()
  })
})

describe('the desktop shell', () => {
  it('is told the UI language when it changes', async () => {
    const SetLanguage = vi.fn().mockResolvedValue(undefined)
    w.go = { main: { App: { SetLanguage } } }
    await applyLanguage('en-XA')
    expect(SetLanguage).toHaveBeenLastCalledWith('en-XA')
    await applyLanguage('en')
    expect(SetLanguage).toHaveBeenLastCalledWith('en')
  })
})
