import type { AIProfile, ToolPolicy } from '@/types/api'

// The built-in tools the editor lists one by one; each is profiles:tools.<id> in the catalog.
export const TOOL_CATALOG: string[] = [
  'internet.search',
  'internet.open',
  'filesystem.search',
  'filesystem.read',
  'filesystem.write',
  'terminal',
  'git.status',
  'git.diff',
  'git.log',
  'git.show',
  'git.add',
  'git.commit',
  'git.push',
]

/**
 * The profile's tools for the editor. A tool the profile doesn't list is
 * denied, as the daemon treats it (tools.PolicyForProfile), so opening the
 * editor and saving never turns on tools nobody chose.
 */
export function defaultToolsFrom(profile: AIProfile): ToolPolicy[] {
  const existing = new Map((profile.tools ?? []).map((t) => [t.tool_id, t.policy]))
  const listed = TOOL_CATALOG.map((id) => ({ tool_id: id, policy: existing.get(id) ?? ('deny' as const) }))
  // Keep tools outside the editor's list, such as connected services, as they are.
  const others = (profile.tools ?? []).filter((t) => !TOOL_CATALOG.includes(t.tool_id))
  return [...listed, ...others]
}
