import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it } from 'vitest'
import type { Acceleration } from '@/types/api'
import { AccelerationPill } from './AccelerationPill'

function renderPill(acceleration?: Acceleration) {
  return render(
    <MemoryRouter>
      <AccelerationPill acceleration={acceleration} model="Qwen 2.5 7B" />
    </MemoryRouter>,
  )
}

describe('AccelerationPill', () => {
  it('shows a model on the GPU in green, with its device and layers on tap', () => {
    renderPill({
      state: 'gpu',
      backend: 'vulkan',
      devices: ['AMD Radeon RX 7900 XTX'],
      layers_offloaded: 29,
      layers_total: 29,
      gpu_memory_bytes: 5 * 1024 ** 3,
    })
    const pill = screen.getByRole('button', { name: 'GPU' })
    expect(pill.className).toContain('text-success')
    expect(pill).toHaveAttribute('aria-expanded', 'false')
    fireEvent.click(pill)
    expect(screen.getByRole('dialog', { name: 'Where Qwen 2.5 7B runs' })).toBeInTheDocument()
    expect(screen.getByText('AMD Radeon RX 7900 XTX')).toBeInTheDocument()
    expect(screen.getByText('Vulkan')).toBeInTheDocument()
    expect(screen.getByText('29 of 29')).toBeInTheDocument()
  })

  it('says why a model is only partly on the GPU', () => {
    renderPill({ state: 'partial', reason: 'gpu_memory', backend: 'vulkan', layers_offloaded: 40, layers_total: 65 })
    fireEvent.click(screen.getByRole('button', { name: 'Partly on GPU' }))
    expect(screen.getByText(/doesn't fit in the GPU's memory/)).toBeInTheDocument()
  })

  it('shows an unused GPU in red, with the way to Diagnostics', () => {
    renderPill({ state: 'cpu', reason: 'cpu_build', backend: 'cpu', layers_offloaded: 0, layers_total: 0 })
    const pill = screen.getByRole('button', { name: 'CPU only' })
    expect(pill.className).toContain('text-danger')
    fireEvent.click(pill)
    expect(screen.getByRole('link', { name: 'Open Diagnostics' })).toHaveAttribute('href', '/diagnostics')
  })

  // A computer without a GPU is expected to use the CPU: grey, never red.
  it('shows the CPU on a computer without a GPU in grey', () => {
    renderPill({ state: 'cpu_expected', reason: 'no_gpu', backend: 'cpu', layers_offloaded: 0, layers_total: 0 })
    const pill = screen.getByRole('button', { name: 'CPU' })
    expect(pill.className).not.toContain('text-danger')
    expect(pill.className).toContain('text-ink-muted')
  })

  it('shows nothing when where the model runs is not known', () => {
    const { container } = renderPill(undefined)
    expect(container).toBeEmptyDOMElement()
  })

  it('closes on Escape', () => {
    renderPill({ state: 'gpu', backend: 'metal', devices: ['Apple M2 Pro'], layers_offloaded: 0, layers_total: 0 })
    fireEvent.click(screen.getByRole('button', { name: 'GPU' }))
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })
})
