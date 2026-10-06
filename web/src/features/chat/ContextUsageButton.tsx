import { useTranslation } from 'react-i18next'
import { useEffect, useId, useRef, useState } from 'react'
import {
  contextRows,
  fillPercent,
  formatTokens,
  type ContextUsage,
} from './contextUsage'
import { formatPercent } from '@/i18n/format'
import { formatBytes } from '@/lib/format'

const rowColor: Record<string, string> = {
  instructions: 'bg-ink-faint',
  tools: 'bg-norn',
  conversation: 'bg-primary',
  toolResults: 'bg-mimir',
}

export function ContextUsageButton({
  usage,
  windowLimit,
}: {
  usage: ContextUsage | null
  windowLimit: number
}) {
  const { t } = useTranslation('chat')
  const [open, setOpen] = useState(false)
  const rootRef = useRef<HTMLDivElement>(null)
  const titleId = useId()
  const limit = usage?.limit || windowLimit
  const used = usage?.promptTokens ?? 0
  const percent = fillPercent(used, limit)
  const rows = usage ? contextRows(usage) : []
  const full = percent >= 100

  useEffect(() => {
    if (!open) return
    const onPointer = (event: PointerEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false)
    }
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false)
    }
    document.addEventListener('pointerdown', onPointer)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('pointerdown', onPointer)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  return (
    <div ref={rootRef} className="relative shrink-0">
      <button
        type="button"
        className="inline-flex h-8 w-8 items-center justify-center rounded-full text-ink-muted transition hover:bg-raised hover:text-ink"
        aria-label={usage ? t('context.full', { percent: formatPercent(percent / 100) }) : t('context.usage')}
        aria-expanded={open}
        aria-controls={titleId}
        title={t('context.usage')}
        onClick={() => setOpen((current) => !current)}
      >
        <ContextRing percent={percent} full={full} />
      </button>
      {open ? (
        <div
          id={titleId}
          role="dialog"
          aria-label={t('context.usage')}
          className="absolute bottom-11 end-0 z-20 w-72 rounded-xl border border-line/80 bg-surface p-3 shadow-panel"
        >
          <div className="flex items-baseline justify-between gap-3">
            <p className="text-sm font-medium text-ink">
              {usage ? t('context.percentFull', { percent: formatPercent(percent / 100) }) : t('context.title')}
            </p>
            <p className="text-xs text-ink-faint">
              {usage
                ? t(usage.estimated ? 'context.usedEstimated' : 'context.used', { used: formatTokens(used), limit: formatTokens(limit) })
                : t('context.limit', { limit: formatTokens(limit) })}
            </p>
          </div>
          {usage && rows.length > 0 ? (
            <>
              <div className="mt-2 flex h-1.5 overflow-hidden rounded-full bg-raised">
                {rows.map((row) => (
                  <span
                    key={row.id}
                    className={rowColor[row.id]}
                    style={{ width: `${(row.tokens / Math.max(used, limit, 1)) * 100}%` }}
                  />
                ))}
              </div>
              <ul className="mt-3 space-y-1.5">
                {rows.map((row) => (
                  <li key={row.id} className="flex items-center gap-2 text-xs text-ink-muted">
                    <span className={`h-2 w-2 shrink-0 rounded-sm ${rowColor[row.id]}`} />
                    <span className="min-w-0 flex-1">{row.label}</span>
                    <span className="tabular-nums text-ink-faint">{formatTokens(row.tokens)}</span>
                  </li>
                ))}
              </ul>
              {percent >= 85 ? (
                <p className="mt-2 text-xs leading-relaxed text-warning">{t('context.nearFull')}</p>
              ) : null}
              {(usage.memoryBytes ?? 0) > 0 ? (
                <p className="mt-2 text-xs leading-relaxed text-ink-muted">
                  {t('context.memory', { size: formatBytes(usage.memoryBytes ?? 0) })}{' '}
                  <span className="text-ink-faint">{t('context.memoryHint')}</span>
                </p>
              ) : null}
              {(usage.summarizedMessages ?? 0) > 0 ? (
                <p className="mt-2 text-xs leading-relaxed text-ink-muted">
                  {t('context.summarized', { count: usage.summarizedMessages })}
                </p>
              ) : null}
            </>
          ) : (
            <p className="mt-2 text-xs leading-relaxed text-ink-muted">{t('context.empty')}</p>
          )}
        </div>
      ) : null}
    </div>
  )
}

/**
 * A meter, open at the bottom so it never reads as a radio button: the track
 * sweeps 270°, and the fill follows it as the chat uses the model's window.
 */
function ContextRing({ percent, full }: { percent: number; full: boolean }) {
  const size = 18
  const stroke = 2
  const radius = (size - stroke) / 2
  const circ = 2 * Math.PI * radius
  const sweep = circ * 0.75
  const shown = Math.max(0, Math.min(percent, 100))
  const dash = (shown / 100) * sweep
  const c = size / 2
  // Start at the lower left (135°) and run clockwise to the lower right.
  const turn = `rotate(135 ${c} ${c})`
  return (
    <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} aria-hidden>
      <circle
        cx={c}
        cy={c}
        r={radius}
        fill="none"
        className="stroke-ink-faint/45"
        strokeWidth={stroke}
        strokeLinecap="round"
        strokeDasharray={`${sweep} ${circ}`}
        transform={turn}
      />
      {shown > 0 ? (
        <circle
          cx={c}
          cy={c}
          r={radius}
          fill="none"
          className={full ? 'stroke-danger' : shown >= 85 ? 'stroke-warning' : 'stroke-primary'}
          strokeWidth={stroke}
          strokeLinecap="round"
          strokeDasharray={`${dash} ${circ}`}
          transform={turn}
        />
      ) : null}
    </svg>
  )
}
