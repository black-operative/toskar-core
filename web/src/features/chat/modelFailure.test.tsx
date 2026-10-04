import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { parseModelFailure } from './modelFailure'
import { ModelFailureNotice } from './ModelFailureNotice'

describe('parseModelFailure', () => {
  it('reads a structured model failure and ignores other errors', () => {
    const failure = parseModelFailure(
      JSON.stringify({
        kind: 'model_health',
        reason: 'oom',
        message: 'This model likely ran out of safe memory on this computer. Toskar stopped it to keep the system stable.',
        interrupted: true,
      }),
    )
    expect(failure?.reason).toBe('oom')
    expect(parseModelFailure('connection refused')).toBeNull()
  })
})

describe('ModelFailureNotice', () => {
  it('keeps the friendly message and offers retry', () => {
    const onRetry = vi.fn()
    render(
      <ModelFailureNotice
        failure={{
          kind: 'model_health',
          reason: 'generation_stalled',
          message: 'The model stopped responding, so Toskar stopped it and cleaned up the failed process.',
        }}
        advanced={false}
        onRetry={onRetry}
        onChooseModel={() => {}}
      />,
    )
    expect(screen.getByText(/stopped responding/)).toBeInTheDocument()
    expect(screen.queryByText('generation_stalled')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(onRetry).toHaveBeenCalledOnce()
  })
})
