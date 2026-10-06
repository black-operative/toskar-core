import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import type { LiveFigures } from '@/types/api'
import { LiveFiguresPanel } from './LiveFiguresPanel'

const base = { node_id: 'n', node_name: 'Desktop', recent: [], day: [] }

describe('LiveFiguresPanel', () => {
  it('shows CPU, memory, and a GPU with every figure', () => {
    render(
      <LiveFiguresPanel
        figures={{
          ...base,
          current: {
            at: '2026-10-05T20:00:00Z',
            cpu_percent: 12,
            memory_used_bytes: 16 * 1024 ** 3,
            memory_total_bytes: 64 * 1024 ** 3,
            gpus: [
              {
                name: 'AMD Radeon RX 7900 XTX',
                busy_percent: 97,
                memory_used_bytes: 5 * 1024 ** 3,
                memory_total_bytes: 24 * 1024 ** 3,
                temperature_c: 61,
                power_watts: 287,
              },
            ],
          },
        }}
      />,
    )
    expect(screen.getByText('AMD Radeon RX 7900 XTX')).toBeInTheDocument()
    expect(screen.getByRole('meter', { name: 'Busy' })).toHaveAttribute('aria-valuenow', '97')
    expect(screen.getByText('61 °C')).toBeInTheDocument()
    expect(screen.getByText('287 W')).toBeInTheDocument()
    expect(screen.getByRole('meter', { name: 'CPU' })).toHaveAttribute('aria-valuenow', '12')
  })

  // A Mac gives the GPU's busy % and the shared memory it uses, but no
  // temperature or power: those are left out, not shown as 0.
  it('leaves out what a computer does not give', () => {
    const figures: LiveFigures = {
      ...base,
      current: { at: '2026-10-05T20:00:00Z', cpu_percent: 16, gpus: [{ name: 'Apple M5 Pro', busy_percent: 31, memory_used_bytes: 898449408 }] },
    }
    render(<LiveFiguresPanel figures={figures} />)
    expect(screen.getByText('Memory in use')).toBeInTheDocument()
    expect(screen.queryByText('Temperature')).not.toBeInTheDocument()
    expect(screen.queryByText('Power')).not.toBeInTheDocument()
    expect(screen.queryByText(/0 °C|0 W/)).not.toBeInTheDocument()
  })

  it('renders nothing without any figures', () => {
    const { container } = render(<LiveFiguresPanel figures={{ ...base, current: { at: '2026-10-05T20:00:00Z' } }} />)
    expect(container).toBeEmptyDOMElement()
  })
})
