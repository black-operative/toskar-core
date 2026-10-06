import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { applyLanguage } from '@/i18n'
import { SystemReadAloudButton } from './ReadAloud'
import { hasSystemSpeech } from './speakableText'

class FakeUtterance {
  lang = ''
  onend: (() => void) | null = null
  onerror: (() => void) | null = null
  constructor(public text: string) {}
}

describe('SystemReadAloudButton', () => {
  const speak = vi.fn()
  const cancel = vi.fn()

  beforeEach(async () => {
    await applyLanguage('en')
    speak.mockReset()
    cancel.mockReset()
    vi.stubGlobal('speechSynthesis', { speak, cancel })
    vi.stubGlobal('SpeechSynthesisUtterance', FakeUtterance)
  })
  afterEach(() => vi.unstubAllGlobals())

  it('reads the answer with the device voices, and stops', () => {
    expect(hasSystemSpeech()).toBe(true)
    render(<SystemReadAloudButton text={'**Tires** wear out. See [the guide](https://example.com).'} />)
    fireEvent.click(screen.getByRole('button', { name: 'Read aloud' }))
    const utterance = speak.mock.calls[0][0] as FakeUtterance
    expect(utterance.text).toBe('Tires wear out. See the guide.')
    expect(utterance.lang).toBe('en')

    fireEvent.click(screen.getByRole('button', { name: 'Stop' }))
    expect(cancel).toHaveBeenCalled()
    expect(screen.getByRole('button', { name: 'Read aloud' })).toBeTruthy()
  })

  it('shows Read aloud again when the reading ends', () => {
    render(<SystemReadAloudButton text="Hello" />)
    fireEvent.click(screen.getByRole('button', { name: 'Read aloud' }))
    act(() => (speak.mock.calls[0][0] as FakeUtterance).onend?.())
    expect(screen.getByRole('button', { name: 'Read aloud' })).toBeTruthy()
  })
})

describe('SystemReadAloudButton with two answers', () => {
  it('leaves the newer reading alone when the older one ends or leaves', async () => {
    await applyLanguage('en')
    const speak = vi.fn()
    const cancel = vi.fn()
    vi.stubGlobal('speechSynthesis', { speak, cancel })
    vi.stubGlobal('SpeechSynthesisUtterance', FakeUtterance)
    const first = render(<SystemReadAloudButton text="First" />)
    fireEvent.click(first.getByRole('button', { name: 'Read aloud' }))
    const older = speak.mock.calls[0][0] as FakeUtterance
    const second = render(<SystemReadAloudButton text="Second" />)
    fireEvent.click(second.getByRole('button', { name: 'Read aloud' }))
    // Starting the second cancels the first, which then reports an error.
    act(() => older.onerror?.())
    cancel.mockReset()
    first.unmount()
    expect(cancel).not.toHaveBeenCalled()
    second.unmount()
    expect(cancel).toHaveBeenCalledTimes(1)
    vi.unstubAllGlobals()
  })
})
