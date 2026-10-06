import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { formatBytes } from '@/lib/format'
import type { AIProfile, HardwareInventory, RunningModelView } from '@/types/api'
import { Ratatoskr } from '@/components/ui/Ratatoskr'
import { AccelerationPill } from '@/components/AccelerationPill'

export function RunningTab({
  running,
  profiles,
  localHardware,
  tightModelIds,
  onStop,
}: {
  running: RunningModelView[]
  profiles: AIProfile[]
  localHardware: HardwareInventory | null
  tightModelIds?: Set<string>
  onStop: (modelId: string, instanceId: string, nodeId: string) => void
}) {
  const { t } = useTranslation('models')
  if (running.length === 0) {
    return (
      <div className="flex items-center gap-4">
        <Ratatoskr state="sleep" size={96} />
        <p className="text-sm text-ink-muted">{t('runningTab.empty')}</p>
      </div>
    )
  }

  const totalMem = running.reduce((s, r) => s + (r.memory_bytes ?? 0), 0)
  const hostMem =
    localHardware?.accelerators?.[0]?.unified_memory_bytes ||
    localHardware?.accelerators?.[0]?.dedicated_vram_bytes ||
    localHardware?.memory?.total_bytes ||
    0

  return (
    <div className="space-y-4">
      <p className="text-sm text-ink-muted">
        {t('runningTab.active')}
        {totalMem > 0 ? t('runningTab.inUse', { size: formatBytes(totalMem) }) : ''}
        {hostMem > 0 ? t('runningTab.ofHost', { size: formatBytes(hostMem) }) : ''}
      </p>
      <ul className="grid gap-4 lg:grid-cols-2">
        {running.map((item) => {
          const usedBy =
            item.used_by_profiles && item.used_by_profiles.length > 0
              ? item.used_by_profiles
              : profiles
                  .filter((p) => p.roles?.some((r) => r.model_id === item.model_id))
                  .map((p) => p.name)

          return (
            <li key={item.instance_id} className="card min-w-0 space-y-3">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <h3 className="font-display text-lg font-semibold text-ink">
                    {item.display_name}
                  </h3>
                  {tightModelIds?.has(item.model_id) ? (
                    <p
                      className="mt-1 text-xs text-ink-muted"
                      title={t('tightFit.hint')}
                    >
                      {t('tightFit.label')}
                    </p>
                  ) : null}
                  <div className="mt-1 flex flex-wrap items-center gap-2">
                    <p className="text-sm text-success">
                      {t('runningTab.runningOn', { computer: item.node_name })}
                      {/* A daemon that reports acceleration names the device in the pill. */}
                      {!item.acceleration && item.accelerator ? ` · ${item.accelerator}` : ''}
                    </p>
                    <AccelerationPill acceleration={item.acceleration} model={item.display_name} />
                  </div>
                </div>
                <button
                  type="button"
                  className="btn-secondary shrink-0 px-3 py-1.5 text-xs"
                  onClick={() => onStop(item.model_id, item.instance_id, item.node_id)}
                >
                  {t('runningTab.stop')}
                </button>
              </div>

              <dl className="grid grid-cols-2 gap-3 text-sm">
                <div>
                  <dt className="label-caps">{t('runningTab.memory')}</dt>
                  <dd className="mt-0.5 tabular-nums text-ink">
                    {item.memory_bytes
                      ? hostMem > 0
                        ? `${formatBytes(item.memory_bytes)} / ${formatBytes(hostMem)}`
                        : formatBytes(item.memory_bytes)
                      : t('runningTab.memoryUnavailable')}
                  </dd>
                </div>
                <div>
                  <dt className="label-caps">{t('runningTab.speed')}</dt>
                  <dd className="mt-0.5 tabular-nums text-ink">
                    {item.speed_tok_per_sec
                      ? t('speed.tokPerSec', { value: Math.round(item.speed_tok_per_sec) })
                      : t('runningTab.speedUnavailable')}
                  </dd>
                </div>
              </dl>

              {usedBy.length > 0 && (
                <p className="text-xs text-ink-faint">{t('runningTab.usedBy', { profiles: usedBy.join(', ') })}</p>
              )}

              <div className="flex flex-wrap gap-2">
                <Link to="/chat" className="btn-primary px-3 py-1.5 text-xs">
                  {t('runningTab.openChat')}
                </Link>
              </div>
            </li>
          )
        })}
      </ul>
    </div>
  )
}
