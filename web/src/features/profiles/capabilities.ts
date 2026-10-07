import i18n from '@/i18n'
import type { ToolPolicy } from '@/types/api'

export type CapabilityId = 'internet' | 'browser' | 'files' | 'code' | 'speech' | 'images' | 'video' | 'automations' | 'shell' | 'git'

// Each capability's name and description are profiles:capabilities.<id> in the catalog.
export const CAPABILITIES: {
  id: CapabilityId
  tools: { id: string; on: ToolPolicy['policy'] }[]
}[] = [
  {
    id: 'internet',
    tools: [
      { id: 'internet.search', on: 'allow' },
      { id: 'internet.open', on: 'allow' },
      { id: 'places.search', on: 'allow' },
      { id: 'places.details', on: 'allow' },
      { id: 'maps.route', on: 'allow' },
      { id: 'maps.distance', on: 'allow' },
    ],
  },
  {
    id: 'browser',
    tools: [
      { id: 'browser.open', on: 'allow' },
      { id: 'browser.extract', on: 'allow' },
      { id: 'browser.screenshot', on: 'allow' },
      { id: 'browser.close', on: 'allow' },
      { id: 'browser.click', on: 'ask' },
      { id: 'browser.type', on: 'ask' },
      { id: 'browser.download', on: 'ask' },
    ],
  },
  {
    id: 'files',
    tools: [
      { id: 'filesystem.search', on: 'allow' },
      { id: 'filesystem.read', on: 'allow' },
      { id: 'filesystem.write', on: 'allow' },
    ],
  },
  {
    id: 'code',
    tools: [{ id: 'code.execute', on: 'ask' }],
  },
  {
    id: 'speech',
    tools: [
      { id: 'speech.transcribe', on: 'allow' },
      { id: 'speech.synthesize', on: 'allow' },
    ],
  },
  {
    id: 'images',
    tools: [
      { id: 'image.generate', on: 'allow' },
      { id: 'image.edit', on: 'allow' },
    ],
  },
  {
    id: 'video',
    tools: [{ id: 'video.generate', on: 'allow' }],
  },
  {
    id: 'automations',
    tools: [{ id: 'automations.schedule', on: 'allow' }],
  },
  {
    id: 'shell',
    tools: [{ id: 'terminal', on: 'allow' }],
  },
  {
    id: 'git',
    tools: [
      { id: 'git.status', on: 'allow' },
      { id: 'git.diff', on: 'allow' },
      { id: 'git.log', on: 'allow' },
      { id: 'git.show', on: 'allow' },
      { id: 'git.add', on: 'allow' },
      { id: 'git.commit', on: 'allow' },
      { id: 'git.push', on: 'allow' },
    ],
  },
]

export function capabilityEnabled(tools: ToolPolicy[] | undefined, id: CapabilityId): boolean {
  const capability = CAPABILITIES.find((item) => item.id === id)
  if (!capability) return false
  return capability.tools.some((tool) => {
    const policy = tools?.find((row) => row.tool_id === tool.id)?.policy
    return policy != null && policy !== 'deny'
  })
}

export function setCapability(tools: ToolPolicy[], id: CapabilityId, enabled: boolean): ToolPolicy[] {
  const capability = CAPABILITIES.find((item) => item.id === id)
  if (!capability) return tools
  const next = tools.map((tool) => ({ ...tool }))
  for (const spec of capability.tools) {
    const policy = enabled ? spec.on : 'deny'
    const row = next.find((tool) => tool.tool_id === spec.id)
    if (row) row.policy = policy
    else next.push({ tool_id: spec.id, policy })
  }
  return next
}

export function capabilityLabel(id: CapabilityId): string {
  return i18n.t(`profiles:capabilities.${id}.label`)
}

export function capabilityDescription(id: CapabilityId): string {
  return i18n.t(`profiles:capabilities.${id}.description`)
}

/** The capabilities a profile has turned on, in order. */
export function activeCapabilities(tools: ToolPolicy[] | undefined): CapabilityId[] {
  return CAPABILITIES.filter((item) => capabilityEnabled(tools, item.id)).map((item) => item.id)
}

export function activeCapabilityLabels(tools: ToolPolicy[] | undefined): string[] {
  return activeCapabilities(tools).map(capabilityLabel)
}
