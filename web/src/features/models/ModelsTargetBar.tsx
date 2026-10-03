import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import i18n from '@/i18n'
import { formatBytes } from '@/lib/format'
import type { HardwareInventory, Node, RunningModelView } from '@/types/api'

/** Page-level install / recommendation target. */
export type ModelsTarget = 'local' | 'all' | string

export function resolveInstallNodeId(target: ModelsTarget): string | undefined {
  if (target === 'local') return undefined
  return target
}

function hardwareLine(hw: HardwareInventory | null | undefined): string {
  if (!hw) return i18n.t('models:target.hardwareUnavailable')
  const parts: string[] = []
  const accel = hw.accelerators?.[0]
  if (accel?.model) parts.push(accel.model)
  else if (hw.cpu?.model) parts.push(hw.cpu.model)
  const mem =
    accel?.unified_memory_bytes ||
    accel?.dedicated_vram_bytes ||
    hw.memory?.total_bytes ||
    0
  if (mem > 0) {
    const unified = Boolean(accel?.unified_memory_bytes)
    parts.push(i18n.t(unified ? 'models:target.unifiedMemory' : 'models:target.memory', { size: formatBytes(mem) }))
  }
  return parts.join(' · ') || i18n.t('models:target.hardwareUnavailable')
}

function targetHardware(
  target: ModelsTarget,
  localHw: HardwareInventory | null,
  nodes: Node[],
): HardwareInventory | null {
  if (target === 'local' || target === 'all') return localHw
  return nodes.find((n) => n.id === target)?.hardware ?? null
}

function targetName(
  target: ModelsTarget,
  localHw: HardwareInventory | null,
  nodes: Node[],
): string {
  if (target === 'all') return i18n.t('models:target.all')
  if (target === 'local') return localHw?.hostname || i18n.t('models:target.thisComputer')
  return nodes.find((n) => n.id === target)?.name || i18n.t('models:target.computer')
}

export function ModelsTargetBar({
  target,
  onTargetChange,
  localHardware,
  nodes,
  running,
  checking = false,
}: {
  target: ModelsTarget
  onTargetChange: (next: ModelsTarget) => void
  localHardware: HardwareInventory | null
  nodes: Node[]
  running: RunningModelView[]
  /** This computer's hardware is still being read; don't call it unavailable yet. */
  checking?: boolean
}) {
  const { t } = useTranslation('models')
  const localNode = nodes.find((n) => n.is_local)
  const onlinePeers = nodes.filter(
    (n) => !n.is_local && n.paired && n.status === 'online',
  )
  const hasCluster = onlinePeers.length > 0 || nodes.filter((n) => n.paired).length > 1

  const hw = targetHardware(target, localHardware, nodes)
  const name = targetName(target, localHardware, nodes)

  const runningForTarget =
    target === 'all'
      ? running
      : target === 'local'
        ? running.filter(
            (r) =>
              !localNode ||
              r.node_id === localNode.id ||
              r.node_id === '' ||
              r.node_name === localHardware?.hostname,
          )
        : running.filter((r) => r.node_id === target)

  const line = checking && !hw ? t('target.hardwareChecking') : hardwareLine(hw)
  const runningLabel = t('target.running', { count: runningForTarget.length })

  return (
    <div className="min-w-0 rounded-xl border border-line/70 bg-surface px-4 py-3 shadow-panel">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="min-w-0">
          <label className="flex flex-wrap items-center gap-2">
            <span className="text-sm font-medium text-ink-muted">{t('target.label')}</span>
            <select
              className="field max-w-full py-1.5 text-sm font-semibold text-ink"
              value={target}
              onChange={(e) => onTargetChange(e.target.value as ModelsTarget)}
              aria-label={t('target.aria')}
            >
              <option value="local">
                {localHardware?.hostname || localNode?.name || t('target.thisComputer')}
              </option>
              {hasCluster && <option value="all">{t('target.all')}</option>}
              {onlinePeers.map((n) => (
                <option key={n.id} value={n.id}>
                  {n.name}
                </option>
              ))}
            </select>
          </label>
          <p className="mt-1.5 text-sm text-ink-muted">
            {target === 'all' ? (
              <>{t('target.clusterView', { running: runningLabel })}</>
            ) : (
              <>
                {line}
                <span className="text-ink-faint"> · </span>
                {runningLabel}
              </>
            )}
          </p>
          <p className="mt-1 text-xs text-ink-faint">
            {target === 'all' ? t('target.allHint') : t('target.basedOn', { name })}
          </p>
        </div>
        <Link
          to="/nodes"
          className="shrink-0 text-sm font-medium text-primary underline-offset-2 hover:underline"
        >
          {hasCluster ? t('target.manage') : t('target.useAnother')}
        </Link>
      </div>
    </div>
  )
}
