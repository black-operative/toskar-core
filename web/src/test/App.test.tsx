import { render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { App } from '@/app/App'
import { useUIStore } from '@/stores/uiStore'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      getHealth: vi.fn(async () => ({
        status: 'ok',
        product: 'yggdrasil',
        version: 'test',
      })),
      getSettings: vi.fn(async () => ({
        data_dir: '/tmp',
        models_dir: '/tmp/models',
        runtimes_dir: '/tmp/runtimes',
        logs_dir: '/tmp/logs',
        api_host: '127.0.0.1',
        api_port: 7331,
        lan_api_enabled: false,
        web_ui_enabled: true,
        discovery_enabled: true,
        node_name: 'test',
        node_id: 'local',
        advanced_mode: false,
        model_lifecycle: 'automatic',
        idle_unload_minutes: 15,
        keep_running_in_background: false,
      })),
    },
  }
})

describe('App', () => {
  beforeEach(() => {
    useUIStore.getState().resetToDefaults()
  })

  it('renders the onboarding welcome screen', async () => {
    render(<App />)
    await waitFor(() => {
      expect(screen.getByRole('heading', { name: 'Toskar' })).toBeInTheDocument()
    })
    expect(
      screen.getByText(/run AI privately on your own computers/i),
    ).toBeInTheDocument()
  })
})
