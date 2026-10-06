import { useEffect, useId, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { formatBytes } from '@/lib/format'
import type { Acceleration } from '@/types/api'

const tone: Record<Acceleration['state'], { pill: string; dot: string }> = {
  gpu: { pill: 'bg-success/15 text-success', dot: 'bg-success' },
  partial: { pill: 'bg-warning/15 text-warning', dot: 'bg-warning' },
  cpu: { pill: 'bg-danger/15 text-danger', dot: 'bg-danger' },
  cpu_expected: { pill: 'bg-raised text-ink-muted', dot: 'bg-ink-faint' },
}

/**
 * Where a running model runs, from the runtime's own report (#317): green on
 * the GPU, amber partly, red when a GPU sits unused, and grey on a computer
 * with no GPU, where the CPU is expected. Tapping it shows the device,
 * layers, GPU memory, and for anything short of green, why and what to do.
 */
export function AccelerationPill({
  acceleration,
  model,
  align = 'start',
}: {
  acceleration?: Acceleration
  model: string
  /** Which edge of the pill the details line up with: end where the pill sits at the right of a card. */
  align?: 'start' | 'end'
}) {
  const { t } = useTranslation('common')
  const [open, setOpen] = useState(false)
  const detailsId = useId()
  const rootRef = useRef<HTMLSpanElement>(null)
  useEffect(() => {
    if (!open) return
    const onDown = (event: PointerEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false)
    }
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false)
    }
    document.addEventListener('pointerdown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('pointerdown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])
  if (!acceleration) return null
  const { state, reason, backend, devices, layers_offloaded, layers_total, gpu_memory_bytes } = acceleration
  const style = tone[state] ?? tone.cpu_expected
  const device = devices && devices.length > 0 ? devices.join(', ') : ''
  const label = t(`acceleration.state.${state}`)
  return (
    <span ref={rootRef} className="relative inline-flex">
      <button
        type="button"
        className={['status-chip inline-flex items-center gap-1.5', style.pill].join(' ')}
        aria-expanded={open}
        aria-controls={detailsId}
        onClick={() => setOpen((v) => !v)}
        title={device || label}
      >
        <span aria-hidden className={['h-1.5 w-1.5 rounded-full', style.dot].join(' ')} />
        {label}
      </button>
      {open ? (
        <div
          id={detailsId}
          role="dialog"
          aria-label={t('acceleration.detailsLabel', { model })}
          className={[
            'absolute top-full z-20 mt-2 w-72 max-w-[calc(100vw-2rem)] space-y-2 rounded-xl border border-line/80 bg-surface p-3 text-start text-sm shadow-panel',
            align === 'end' ? 'end-0' : 'start-0',
          ].join(' ')}
        >
          <dl className="space-y-1.5">
            {device ? (
              <div className="flex justify-between gap-3">
                <dt className="text-ink-muted">{t('acceleration.device')}</dt>
                <dd className="min-w-0 text-end text-ink">{device}</dd>
              </div>
            ) : null}
            <div className="flex justify-between gap-3">
              <dt className="text-ink-muted">{t('acceleration.runsWith')}</dt>
              <dd className="text-ink">{t(`acceleration.backend.${backend}`, { defaultValue: backend })}</dd>
            </div>
            {layers_total > 0 ? (
              <div className="flex justify-between gap-3">
                <dt className="text-ink-muted">{t('acceleration.layers')}</dt>
                <dd className="tabular-nums text-ink">
                  {t('acceleration.layersValue', { offloaded: layers_offloaded, total: layers_total })}
                </dd>
              </div>
            ) : null}
            {gpu_memory_bytes ? (
              <div className="flex justify-between gap-3">
                <dt className="text-ink-muted">{t('acceleration.gpuMemory')}</dt>
                <dd className="tabular-nums text-ink">{formatBytes(gpu_memory_bytes)}</dd>
              </div>
            ) : null}
          </dl>
          {reason ? <p className="text-ink-muted">{t(`acceleration.reason.${reason}`)}</p> : null}
          {reason === 'gpu_unavailable' || reason === 'cpu_build' ? (
            <Link to="/diagnostics" className="inline-block text-primary hover:underline">
              {t('acceleration.openDiagnostics')}
            </Link>
          ) : null}
        </div>
      ) : null}
    </span>
  )
}
