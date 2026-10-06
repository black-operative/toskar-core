import { useTranslation } from 'react-i18next'
import { Sparkline } from '@/components/ui/Sparkline'
import { formatBytes } from '@/lib/format'
import { formatNumber, formatPercent } from '@/i18n/format'
import type { GPUSample, LiveFigures } from '@/types/api'

function Bar({ label, percent, value }: { label: string; percent: number; value: string }) {
  const clamped = Math.max(0, Math.min(100, percent))
  return (
    <div className="flex items-center gap-3">
      <span className="w-24 shrink-0 truncate text-xs text-ink-muted">{label}</span>
      <div
        className="h-2 flex-1 overflow-hidden rounded-full bg-raised"
        role="meter"
        aria-label={label}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={Math.round(clamped)}
      >
        <div className="h-full bg-primary/80 transition-all duration-300" style={{ width: `${clamped}%` }} />
      </div>
      <span className="w-24 shrink-0 text-end text-xs tabular-nums text-ink">{value}</span>
    </div>
  )
}

function GPUFigures({ gpu, history }: { gpu: GPUSample; history: number[] }) {
  const { t } = useTranslation(['performance', 'common'])
  const used = gpu.memory_used_bytes
  const total = gpu.memory_total_bytes
  return (
    <div className="space-y-2">
      <p className="truncate text-sm font-medium text-ink" title={gpu.name}>
        {gpu.name}
      </p>
      {gpu.busy_percent != null ? (
        <Bar label={t('live.gpuBusy')} percent={gpu.busy_percent} value={formatPercent(gpu.busy_percent / 100)} />
      ) : null}
      <Sparkline values={history} max={100} label={t('live.busyChart', { name: gpu.name })} />
      {used != null && total ? (
        <Bar label={t('live.gpuMemory')} percent={(used / total) * 100} value={`${formatBytes(used)} / ${formatBytes(total)}`} />
      ) : used != null ? (
        // Apple silicon shares system memory: what the GPU is using, with no total of its own.
        <div className="flex justify-between gap-3 text-xs">
          <span className="text-ink-muted">{t('live.sharedMemory')}</span>
          <span className="tabular-nums text-ink">{formatBytes(used)}</span>
        </div>
      ) : null}
      {gpu.temperature_c != null || gpu.power_watts != null ? (
        <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs">
          {gpu.temperature_c != null ? (
            <span>
              <span className="text-ink-muted">{t('live.temperature')} </span>
              <span className="tabular-nums text-ink">
                {t('common:units.celsius', { value: formatNumber(gpu.temperature_c, { maximumFractionDigits: 0 }) })}
              </span>
            </span>
          ) : null}
          {gpu.power_watts != null ? (
            <span>
              <span className="text-ink-muted">{t('live.power')} </span>
              <span className="tabular-nums text-ink">
                {t('common:units.watts', { value: formatNumber(gpu.power_watts, { maximumFractionDigits: 0 }) })}
              </span>
            </span>
          ) : null}
        </div>
      ) : null}
    </div>
  )
}

/**
 * A computer's live figures (#317): CPU and memory, and for each GPU its
 * busy %, video memory, temperature, and power where the computer gives
 * them, with the last hour of busy % as a line. A figure the computer can't
 * give isn't shown, rather than shown as 0.
 */
export function LiveFiguresPanel({ figures }: { figures: LiveFigures }) {
  const { t } = useTranslation('performance')
  const now = figures.current
  const recent = figures.recent ?? []
  const cpuHistory = recent.map((s) => s.cpu_percent).filter((v): v is number => v != null)
  const memUsed = now.memory_used_bytes
  const memTotal = now.memory_total_bytes
  const gpus = now.gpus ?? []
  if (now.cpu_percent == null && memUsed == null && gpus.length === 0) return null
  return (
    <div className="space-y-3">
      {now.cpu_percent != null ? (
        <Bar label={t('live.cpu')} percent={now.cpu_percent} value={formatPercent(now.cpu_percent / 100)} />
      ) : null}
      <Sparkline values={cpuHistory} max={100} label={t('live.cpuChart')} />
      {memUsed != null && memTotal ? (
        <Bar label={t('live.memory')} percent={(memUsed / memTotal) * 100} value={`${formatBytes(memUsed)} / ${formatBytes(memTotal)}`} />
      ) : null}
      {gpus.map((gpu, i) => (
        <GPUFigures
          key={`${gpu.name}-${i}`}
          gpu={gpu}
          history={recent.map((s) => s.gpus?.[i]?.busy_percent).filter((v): v is number => v != null)}
        />
      ))}
    </div>
  )
}
