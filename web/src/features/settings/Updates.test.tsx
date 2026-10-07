import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import type { UpdatesStatus } from '@/types/api'
import { UpdateCheckSetting, UpdateNotice } from './Updates'

const latest = { version: '1.7.0', notes_url: 'https://toskar.ai/whats-new', download_url: 'https://toskar.ai/download' }
const status = (over: Partial<UpdatesStatus>): UpdatesStatus => ({ supported: true, enabled: true, current: '1.6.1', available: false, ...over })

describe('UpdateNotice', () => {
  it('names the newer version, with what is new and where to get it', () => {
    render(<UpdateNotice updates={status({ available: true, latest })} />)
    expect(screen.getByText(/Toskar 1\.7\.0 is available\./)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: "What's new" })).toHaveAttribute('href', 'https://toskar.ai/whats-new')
    expect(screen.getByRole('link', { name: 'Download' })).toHaveAttribute('href', 'https://toskar.ai/download')
  })

  it('says up to date after a check found nothing newer, and nothing before one', () => {
    const { rerender } = render(<UpdateNotice updates={status({ latest: { version: '1.6.1' } })} />)
    expect(screen.getByText('Up to date')).toBeInTheDocument()
    rerender(<UpdateNotice updates={status({})} />)
    expect(screen.queryByText('Up to date')).not.toBeInTheDocument()
  })

  it('shows nothing when the check is off or the build does not check', () => {
    const { container, rerender } = render(<UpdateNotice updates={status({ enabled: false, latest: { version: '1.6.1' } })} />)
    expect(container).toBeEmptyDOMElement()
    rerender(<UpdateNotice updates={status({ supported: false, enabled: false })} />)
    expect(container).toBeEmptyDOMElement()
  })
})

describe('UpdateCheckSetting', () => {
  it('turns the daily check on and off', () => {
    const onToggle = vi.fn()
    render(<UpdateCheckSetting updates={status({})} checked onToggle={onToggle} />)
    expect(screen.getByText(/Nothing else is sent\./)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('switch', { name: 'Check for updates' }))
    expect(onToggle).toHaveBeenCalled()
  })

  it('is hidden in builds that update themselves', () => {
    const { container } = render(<UpdateCheckSetting updates={status({ supported: false, enabled: false })} checked onToggle={() => {}} />)
    expect(container).toBeEmptyDOMElement()
  })
})
