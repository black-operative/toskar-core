import i18n from '@/i18n'
import { formatDate, formatDecimal, formatMilliseconds, formatSequence, formatTokensPerSecond } from '@/i18n/format'
import type { GenerationRun, GenerationRoleStep, Model } from '@/types/api'

export function formatMs(ms: number): string {
  if (!ms || ms <= 0) return '—'
  return formatMilliseconds(ms)
}

export function formatRate(n: number): string {
  if (!n || n <= 0) return '—'
  return formatDecimal(n, n >= 100 ? 0 : 1)
}

export function formatWhen(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return '—'
  return formatDate(d, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

export function formatRoleLabel(role: string): string {
  if (!role) return i18n.t('performance:roles.role')
  // A plan's worker slots are "worker:1", "worker:2", …
  const worker = /^worker:(\d+)$/.exec(role)
  if (worker) return i18n.t('performance:roles.worker', { n: worker[1] })
  if (role === 'planner') return i18n.t('performance:roles.planner')
  if (role === 'reviewer') return i18n.t('performance:roles.reviewer')
  const slot = /^([a-z]+):(\d+)$/.exec(role)
  if (slot) return `${slot[1].charAt(0).toUpperCase()}${slot[1].slice(1)} ${slot[2]}`
  return role.charAt(0).toUpperCase() + role.slice(1)
}

export function modelDisplayName(models: Model[], id: string): string {
  return models.find((m) => m.id === id)?.display_name ?? id
}

/** Ordered unique computer names for a run (route). */
export function nodeRoute(run: GenerationRun): string[] {
  const names: string[] = []
  const seen = new Set<string>()
  for (const step of run.role_steps ?? []) {
    const label = step.node_name || step.node_id
    if (!label || seen.has(label)) continue
    seen.add(label)
    names.push(label)
  }
  return names
}

export function routeLabel(run: GenerationRun): string {
  const names = nodeRoute(run)
  if (names.length === 0) {
    return run.cross_machine ? i18n.t('performance:route.multiple') : i18n.t('performance:route.thisComputer')
  }
  if (names.length === 1) return names[0]
  return formatSequence(names)
}

export type MetricLabels = {
  speed: string
  prompt: string
  firstResponse: string
  total: string
  teamRun: string
}

export function metricLabels(advanced: boolean): MetricLabels {
  if (advanced) {
    return {
      speed: i18n.t('performance:metricsAdvanced.speed'),
      prompt: i18n.t('performance:metricsAdvanced.prompt'),
      firstResponse: i18n.t('performance:metricsAdvanced.firstResponse'),
      total: i18n.t('performance:metricsAdvanced.total'),
      teamRun: i18n.t('performance:metricsAdvanced.teamRun'),
    }
  }
  return {
    speed: i18n.t('performance:metrics.speed'),
    prompt: i18n.t('performance:metrics.prompt'),
    firstResponse: i18n.t('performance:metrics.firstResponse'),
    total: i18n.t('performance:metrics.total'),
    teamRun: i18n.t('performance:metrics.teamRun'),
  }
}

/** A speed in tokens per second, or — when there is none. */
export function formatTokPerSec(n: number): string {
  return n > 0 ? formatTokensPerSecond(n) : '—'
}

export function estimateBenchmarkMinutes(
  modelCount: number,
  promptCount: number,
  runsPerPrompt: number,
): number {
  if (modelCount < 1 || promptCount < 1) return 0
  const steps = modelCount * promptCount * (1 + Math.max(1, runsPerPrompt))
  // ~10–14s per measured step including load/unload overhead
  return Math.max(1, Math.round((steps * 12) / 60))
}

export function memoryUsePercent(total?: number, available?: number): number | null {
  if (!total || total <= 0 || available == null) return null
  const used = total - available
  if (used < 0) return null
  return Math.min(100, Math.round((used / total) * 100))
}

export function stepMetricRows(step: GenerationRoleStep, advanced: boolean) {
  const labels = metricLabels(advanced)
  return [
    { label: labels.firstResponse, value: formatMs(step.ttft_ms) },
    { label: labels.prompt, value: formatMs(step.prompt_ms) },
    { label: labels.total, value: formatMs(step.total_ms) },
    {
      label: labels.speed,
      value: formatTokPerSec(step.eval_tok_per_sec),
    },
  ]
}

/** "Vulkan · AMD Radeon RX 7900 XTX", or "CPU". */
export function ranOnDetail(backend: string, device: string | undefined, t: (key: string, options?: Record<string, unknown>) => string): string {
  const name = t(`common:acceleration.backend.${backend}`, { defaultValue: backend })
  return device ? t('ranOn.detail', { backend: name, device }) : name
}
