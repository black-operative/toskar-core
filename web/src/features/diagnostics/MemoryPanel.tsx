import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { api } from '@/lib/api'
import { formatBytes } from '@/lib/format'
import { formatDate, formatNumber } from '@/i18n/format'
import type { RuntimeSample } from '@/types/api'

/** Steady growth: three hours or more of samples, memory up by half, and background tasks not falling. */
export function growingFor(samples: RuntimeSample[], intervalSeconds: number): number {
  if (samples.length < 2 || intervalSeconds <= 0) return 0
  const hours = ((samples.length - 1) * intervalSeconds) / 3600
  if (hours < 3) return 0
  const first = samples[0]
  const last = samples[samples.length - 1]
  if (last.heap_bytes < first.heap_bytes * 1.5 || last.goroutines < first.goroutines) return 0
  // Most steps go up, not one spike.
  let rises = 0
  for (let i = 1; i < samples.length; i++) if (samples[i].heap_bytes >= samples[i - 1].heap_bytes) rises++
  return rises / (samples.length - 1) >= 0.7 ? Math.floor(hours) : 0
}

function Sparkline({ samples, label }: { samples: RuntimeSample[]; label: string }) {
  if (samples.length < 2) return null
  const width = 240
  const height = 40
  const values = samples.map((s) => s.heap_bytes)
  const max = Math.max(...values)
  const min = Math.min(...values)
  const span = max - min || 1
  const points = values
    .map((v, i) => `${((i / (values.length - 1)) * width).toFixed(1)},${(height - 2 - ((v - min) / span) * (height - 4)).toFixed(1)}`)
    .join(' ')
  return (
    <svg viewBox={`0 0 ${width} ${height}`} className="h-10 w-full max-w-xs" role="img" aria-label={label}>
      <polyline points={points} fill="none" className="stroke-primary" strokeWidth={1.5} strokeLinejoin="round" />
    </svg>
  )
}

/**
 * Yggdrasil's own memory and background tasks, now and over the last day
 * (#231), so a slow leak is visible and easy to report.
 */
export function MemoryPanel() {
  const { t } = useTranslation('diagnostics')
  const query = useQuery({ queryKey: ['runtime-history'], queryFn: () => api.getRuntimeHistory(), refetchInterval: 60_000 })
  const history = query.data
  // An older daemon, or one that answered something else, has no counts.
  if (!history?.now) return null
  const samples = Array.isArray(history.samples) ? history.samples : []
  const hours = Math.max(1, Math.round(((samples.length - 1) * history.interval_seconds) / 3600))
  const growing = growingFor(samples, history.interval_seconds)
  return (
    <section className="card space-y-3" aria-labelledby="memory-title">
      <div>
        <h2 id="memory-title" className="section-title">
          {t('memory.title')}
        </h2>
        <p className="mt-1 text-sm text-ink-muted">{t('memory.description')}</p>
      </div>
      <dl className="grid gap-3 text-sm sm:grid-cols-3">
        <div>
          <dt className="text-ink-muted">{t('memory.heap')}</dt>
          <dd className="tabular-nums text-ink">{formatBytes(history.now.heap_bytes)}</dd>
        </div>
        <div>
          <dt className="text-ink-muted">{t('memory.tasks')}</dt>
          <dd className="tabular-nums text-ink">{formatNumber(history.now.goroutines)}</dd>
        </div>
        {history.started_at && !history.started_at.startsWith('0001') ? (
          <div>
            <dt className="text-ink-muted">{t('memory.since')}</dt>
            <dd className="text-ink">{formatDate(history.started_at, { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' })}</dd>
          </div>
        ) : null}
      </dl>
      <Sparkline samples={samples} label={t('memory.chartLabel', { count: hours })} />
      {growing > 0 ? (
        <p role="status" className="rounded-lg bg-warning/10 px-3 py-2 text-sm text-warning">
          {t('memory.growing', { count: growing })}
        </p>
      ) : null}
    </section>
  )
}
