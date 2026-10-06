import { describe, expect, it } from 'vitest'
import type { AIProfile } from '@/types/api'
import { defaultToolsFrom } from './editorTools'

const profile = (tools: AIProfile['tools']): AIProfile => ({
  id: 'p',
  name: 'P',
  purpose: 'general',
  orchestrator_id: 'simple',
  roles: [],
  node_policy: { mode: 'automatic' },
  tools,
})

describe('defaultToolsFrom', () => {
  it('denies the tools a profile does not list, as the daemon does', () => {
    const tools = defaultToolsFrom(profile([]))
    expect(tools.length).toBeGreaterThan(0)
    expect(tools.every((t) => t.policy === 'deny')).toBe(true)
  })

  it('keeps listed policies, and tools outside the editor such as connected services', () => {
    const tools = defaultToolsFrom(profile([
      { tool_id: 'internet.search', policy: 'allow' },
      { tool_id: 'github.search', policy: 'ask' },
    ]))
    expect(tools.find((t) => t.tool_id === 'internet.search')?.policy).toBe('allow')
    expect(tools.find((t) => t.tool_id === 'terminal')?.policy).toBe('deny')
    expect(tools.find((t) => t.tool_id === 'github.search')?.policy).toBe('ask')
  })
})
