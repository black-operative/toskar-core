import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { AutomationDigestSetting } from './AutomationDigest'

describe('AutomationDigestSetting', () => {
  it('turns on at 8:00 in this time zone, and sets its time', () => {
    const onChange = vi.fn()
    const { rerender } = render(<AutomationDigestSetting value="" onChange={onChange} />)
    expect(screen.queryByLabelText('Time')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('switch', { name: 'Daily automation digest' }))
    expect(onChange).toHaveBeenLastCalledWith({ automation_digest: '08:00', automation_digest_zone: expect.any(String) })
    rerender(<AutomationDigestSetting value="08:00" onChange={onChange} />)
    fireEvent.change(screen.getByLabelText('Time'), { target: { value: '18:30' } })
    expect(onChange).toHaveBeenLastCalledWith({ automation_digest: '18:30', automation_digest_zone: expect.any(String) })
    fireEvent.click(screen.getByRole('switch', { name: 'Daily automation digest' }))
    expect(onChange).toHaveBeenLastCalledWith({ automation_digest: '' })
  })
})
