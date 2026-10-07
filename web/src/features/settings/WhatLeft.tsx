import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { api } from '@/lib/api'
import type { EgressKind, EgressRecord } from '@/types/api'
import { formatDate } from '@/i18n/format'

// What left, in order; the names are settings:whatLeft.kinds.<kind> in the catalog.
const KINDS: EgressKind[] = ['web_search', 'web_page', 'places', 'paired_computer', 'external_server', 'connector', 'notification', 'community_ratings', 'update_check']
// What sent it; the names are settings:whatLeft.sources.<source>.
const SOURCES = ['chat', 'api', 'automation', 'training']

const RETENTION_DAYS = [7, 30, 90, 365, 0]

function when(iso: string): string {
  return formatDate(iso, { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' })
}

/**
 * What left this computer (spec §63): every web search, page read, paired
 * computer, external server, and connected service runs sent data to, and
 * how long run records are kept.
 */
export function WhatLeft() {
  const { t } = useTranslation('settings')
  const kindLabel = (kind: EgressKind) => (KINDS.includes(kind) ? t(`whatLeft.kinds.${kind}`) : kind)
  const sourceLabel = (source: string) => (SOURCES.includes(source) ? t(`whatLeft.sources.${source}`) : source)
  const retentionLabel = (days: number) =>
    days === 0 ? t('whatLeft.retention.forever') : days === 365 ? t('whatLeft.retention.year') : t('whatLeft.retention.days', { count: days })
  const queryClient = useQueryClient()
  const overview = useQuery({ queryKey: ['privacy'], queryFn: () => api.getPrivacy(), retry: false })
  const records = useQuery({ queryKey: ['egress'], queryFn: () => api.listEgress(), retry: false })
  const [message, setMessage] = useState('')

  const retention = useMutation({
    mutationFn: (days: number) => api.setRunRetention(days),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['privacy'] })
      void queryClient.invalidateQueries({ queryKey: ['egress'] })
    },
  })
  const deleteRuns = useMutation({
    mutationFn: () => api.deleteRunRecords(),
    onSuccess: (c) => {
      setMessage(c ? t('whatLeft.deleted', { tasks: c.tasks, automations: c.automation_runs, entries: c.egress }) : '')
      void queryClient.invalidateQueries({ queryKey: ['privacy'] })
      void queryClient.invalidateQueries({ queryKey: ['egress'] })
    },
  })

  const counts = overview.data?.last_30_days ?? {}
  const total = Object.values(counts).reduce((a, b) => a + (b ?? 0), 0)
  const list: EgressRecord[] = records.data ?? []

  return (
    <section className="card space-y-4">
      <div>
        <h2 className="section-title">{t('whatLeft.title')}</h2>
        <p className="mt-1 text-sm text-ink-muted">{t('whatLeft.description')}</p>
      </div>

      <p className="text-sm text-ink">
        {total === 0
          ? t('whatLeft.nothing')
          : t('whatLeft.summary', {
              list: KINDS.filter((k) => counts[k])
                .map((k) => t('whatLeft.summaryItem', { kind: t(`whatLeft.kindsLower.${k}`), count: counts[k] }))
                .join(', '),
            })}
      </p>

      {list.length > 0 && (
        <ul className="max-h-72 divide-y divide-line/50 overflow-y-auto rounded-lg border border-line/60">
          {list.map((r) => (
            <li key={r.id} className="flex flex-wrap items-baseline gap-x-2 px-3 py-2 text-xs">
              <span className="font-medium text-ink">{kindLabel(r.kind)}</span>
              <span className="text-ink">{r.destination}</span>
              {r.detail && <span className="min-w-0 flex-1 truncate text-ink-muted" title={r.detail}>{r.detail}</span>}
              <span className="ms-auto shrink-0 text-ink-faint">
                {r.source ? `${sourceLabel(r.source)} · ` : ''}
                {when(r.at)}
              </span>
            </li>
          ))}
        </ul>
      )}

      <div className="flex flex-wrap items-end gap-3">
        <label className="block text-sm">
          <span className="text-ink-muted">{t('whatLeft.keepFor')}</span>
          <select
            className="field mt-1"
            value={overview.data?.retention_days ?? 30}
            disabled={retention.isPending}
            onChange={(e) => retention.mutate(Number(e.target.value))}
          >
            {RETENTION_DAYS.map((days) => (
              <option key={days} value={days}>
                {retentionLabel(days)}
              </option>
            ))}
          </select>
        </label>
        <button
          type="button"
          className="btn-secondary px-3 py-1.5 text-xs"
          disabled={deleteRuns.isPending}
          onClick={() => {
            if (window.confirm(t('whatLeft.confirmDelete'))) {
              deleteRuns.mutate()
            }
          }}
        >
          {t('whatLeft.delete')}
        </button>
      </div>
      <p className="text-xs text-ink-faint">{t('whatLeft.footnote')}</p>
      {message && <p className="text-xs text-ink-muted">{message}</p>}
      {(retention.error || deleteRuns.error) && (
        <p className="text-xs text-danger">{String((retention.error ?? deleteRuns.error) as Error)}</p>
      )}
    </section>
  )
}
