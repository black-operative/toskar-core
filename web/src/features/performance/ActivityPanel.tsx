import { useQuery } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { EmptyState } from '@/components/ui/EmptyState'
import { api } from '@/lib/api'
import { useUIStore } from '@/stores/uiStore'
import type { GenerationRun, Model } from '@/types/api'
import {
  formatMs,
  formatRate,
  formatRoleLabel,
  formatTokPerSec,
  formatWhen,
  metricLabels,
  modelDisplayName,
  nodeRoute,
  ranOnDetail,
  routeLabel,
  stepMetricRows,
} from './performanceFormat'
import { LoadError } from '@/components/ui/LoadError'
import { Skeleton } from '@/components/ui/Skeleton'
import { RanOnTag } from './RanOnTag'

function SummaryCard({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-xl bg-raised/40 px-4 py-3">
      <p className="label-caps text-[10px] text-ink-faint">{label}</p>
      <p className="mt-1 font-display text-xl font-semibold tabular-nums text-ink">{value}</p>
    </div>
  )
}

function ActivityRow({
  run,
  models,
  open,
  onToggle,
}: {
  run: GenerationRun
  models: Model[]
  open: boolean
  onToggle: () => void
}) {
  const { t } = useTranslation('performance')
  const advanced = useUIStore((s) => s.advancedMode)
  const labels = metricLabels(advanced)
  const title =
    run.conversation_title ||
    run.profile_name ||
    modelDisplayName(models, run.model_id) ||
    t('activity.generation')
  const route = nodeRoute(run)
  const modelName = modelDisplayName(models, run.model_id)
  const helpedPeer = run.profile_name === 'Cluster request'

  return (
    <li className="border-b border-line/60 last:border-0">
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={open}
        className="flex w-full items-start gap-3 px-4 py-3.5 text-start transition hover:bg-raised/40"
      >
        <div className="min-w-0 flex-1 space-y-1.5">
          <div className="flex flex-wrap items-center gap-2">
            <p className="font-medium text-ink">{title}</p>
            {run.cross_machine && (
              <span className="rounded-full border border-info/40 bg-info/10 px-2 py-0.5 text-[10px] font-medium uppercase tracking-wide text-info">
                {helpedPeer ? t('activity.helpedPeer') : labels.teamRun}
              </span>
            )}
          </div>
          <p className="text-xs text-ink-faint">{formatWhen(run.created_at)}</p>
          <p className="flex flex-wrap items-center gap-2 text-sm text-ink">
            {modelName}
            <RanOnTag backend={run.backend} device={run.device} />
          </p>
          <p className="text-sm text-ink-muted">
            {route.length > 1 ? (
              <span className="inline-flex flex-wrap items-center gap-1">
                {route.map((name, i) => (
                  <span key={`${name}-${i}`} className="inline-flex items-center gap-1">
                    {i > 0 && <span className="inline-block text-ink-faint rtl:-scale-x-100" aria-hidden>→</span>}
                    <span className="rounded-md bg-raised px-1.5 py-0.5 text-xs text-ink">
                      {name}
                    </span>
                  </span>
                ))}
              </span>
            ) : (
              routeLabel(run)
            )}
          </p>
          <p className="text-sm tabular-nums text-ink-muted">
            {formatMs(run.total_ms)}
            {run.eval_tok_per_sec > 0 ? ` · ${formatTokPerSec(run.eval_tok_per_sec)}` : ''}
          </p>
        </div>
        <span className={['shrink-0 pt-1 text-ink-faint', open ? '' : 'inline-block rtl:-scale-x-100'].join(' ')} aria-hidden>
          {open ? '▾' : '›'}
        </span>
      </button>

      {open && (
        <div className="space-y-3 border-t border-line/40 bg-raised/20 px-4 py-3 animate-fade">
          <dl className="grid gap-2 sm:grid-cols-2">
            <div className="flex justify-between gap-2 text-sm">
              <dt className="text-ink-muted">{labels.firstResponse}</dt>
              <dd className="tabular-nums text-ink">{formatMs(run.ttft_ms)}</dd>
            </div>
            <div className="flex justify-between gap-2 text-sm">
              <dt className="text-ink-muted">{labels.prompt}</dt>
              <dd className="tabular-nums text-ink">{formatMs(run.prompt_ms)}</dd>
            </div>
            <div className="flex justify-between gap-2 text-sm">
              <dt className="text-ink-muted">{labels.speed}</dt>
              <dd className="tabular-nums text-ink">{formatTokPerSec(run.eval_tok_per_sec)}</dd>
            </div>
            <div className="flex justify-between gap-2 text-sm">
              <dt className="text-ink-muted">{labels.total}</dt>
              <dd className="tabular-nums text-ink">{formatMs(run.total_ms)}</dd>
            </div>
            {run.backend ? (
              <div className="flex justify-between gap-2 text-sm">
                <dt className="text-ink-muted">{t('ranOn.label')}</dt>
                <dd className="min-w-0 text-end text-ink">{ranOnDetail(run.backend, run.device, t)}</dd>
              </div>
            ) : null}
            {advanced && (
              <>
                <div className="flex justify-between gap-2 text-sm">
                  <dt className="text-ink-muted">{t('activity.promptTokPerSec')}</dt>
                  <dd className="tabular-nums text-ink">
                    {formatRate(run.prompt_tok_per_sec)}
                  </dd>
                </div>
                <div className="flex justify-between gap-2 text-sm">
                  <dt className="text-ink-muted">{t('activity.tokensOut')}</dt>
                  <dd className="tabular-nums text-ink">
                    {run.completion_tokens > 0 ? run.completion_tokens : '—'}
                  </dd>
                </div>
              </>
            )}
          </dl>

          {(run.role_steps?.length ?? 0) > 0 && (
            <div className="space-y-2">
              <p className="text-xs font-medium text-ink-muted">
                {run.cross_machine ? t('activity.perRoleAcross') : t('activity.perRoleTurn')}
              </p>
              <ul className="space-y-2">
                {run.role_steps!.map((step) => (
                  <li
                    key={`${step.role}-${step.node_id ?? ''}-${step.model_id ?? ''}`}
                    className="rounded-lg bg-surface px-3 py-2 text-xs"
                  >
                    <p className="font-medium text-ink">
                      {formatRoleLabel(step.role)}
                      {step.node_name ? ` · ${step.node_name}` : ''}
                    </p>
                    {step.model_id ? (
                      <p className="mt-0.5 text-ink-muted">
                        {modelDisplayName(models, step.model_id)}
                      </p>
                    ) : null}
                    <dl className="mt-2 grid grid-cols-2 gap-1">
                      {stepMetricRows(step, advanced).map((row) => (
                        <div key={row.label} className="flex justify-between gap-2">
                          <dt className="text-ink-faint">{row.label}</dt>
                          <dd className="tabular-nums text-ink">{row.value}</dd>
                        </div>
                      ))}
                    </dl>
                  </li>
                ))}
              </ul>
            </div>
          )}
        </div>
      )}
    </li>
  )
}

export function ActivityPanel() {
  const { t } = useTranslation('performance')
  const advanced = useUIStore((s) => s.advancedMode)
  const labels = metricLabels(advanced)
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [filterComputer, setFilterComputer] = useState('')
  const [filterModel, setFilterModel] = useState('')
  const [filterProfile, setFilterProfile] = useState('')
  const [filterDate, setFilterDate] = useState('')
  const [filterHardware, setFilterHardware] = useState<'' | 'gpu' | 'cpu'>('')

  const performanceQuery = useQuery({
    queryKey: ['performance', 'activity'],
    queryFn: () => api.getPerformance({ sort: 'created_at', order: 'desc', limit: 200 }),
    retry: false,
    refetchInterval: 10_000,
  })

  const modelsQuery = useQuery({
    queryKey: ['models'],
    queryFn: () => api.getModels(),
    retry: false,
  })

  const runs = performanceQuery.data ?? []
  const models = modelsQuery.data ?? []

  const computers = useMemo(() => {
    const set = new Set<string>()
    for (const r of runs) {
      for (const n of nodeRoute(r)) set.add(n)
    }
    return [...set].sort()
  }, [runs])

  const modelOptions = useMemo(() => {
    const set = new Set<string>()
    for (const r of runs) {
      if (r.model_id) set.add(r.model_id)
    }
    return [...set]
  }, [runs])

  const profiles = useMemo(() => {
    const set = new Set<string>()
    for (const r of runs) {
      if (r.profile_name) set.add(r.profile_name)
    }
    return [...set].sort()
  }, [runs])

  const filtered = useMemo(() => {
    return runs.filter((r) => {
      if (filterComputer) {
        const names = nodeRoute(r)
        if (!names.includes(filterComputer)) return false
      }
      if (filterModel && r.model_id !== filterModel) return false
      if (filterProfile && r.profile_name !== filterProfile) return false
      if (filterDate) {
        const day = r.created_at?.slice(0, 10)
        if (day !== filterDate) return false
      }
      if (filterHardware) {
        if (!r.backend) return false
        if ((r.backend !== 'cpu') !== (filterHardware === 'gpu')) return false
      }
      return true
    })
  }, [runs, filterComputer, filterModel, filterProfile, filterDate, filterHardware])

  // Only replies recorded since Toskar noted the device have one to filter by.
  const anyRanOn = runs.some((r) => r.backend)

  const summary = useMemo(() => {
    if (filtered.length === 0) return null
    const avg = (pick: (r: GenerationRun) => number) => {
      const vals = filtered.map(pick).filter((n) => n > 0)
      if (vals.length === 0) return 0
      return vals.reduce((a, b) => a + b, 0) / vals.length
    }
    return {
      count: filtered.length,
      teamRuns: filtered.filter((r) => r.cross_machine).length,
      avgSpeed: avg((r) => r.eval_tok_per_sec),
      avgTtft: avg((r) => r.ttft_ms),
      avgTotal: avg((r) => r.total_ms),
    }
  }, [filtered])

  return (
    <div className="space-y-6 animate-fade">
      {summary && (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
          <SummaryCard label={t('activity.runs')} value={String(summary.count)} />
          <SummaryCard label={t('activity.averageOf', { metric: labels.speed })} value={formatTokPerSec(summary.avgSpeed)} />
          <SummaryCard
            label={labels.firstResponse}
            value={formatMs(summary.avgTtft)}
          />
          <SummaryCard
            label={t('activity.averageResponse')}
            value={formatMs(summary.avgTotal)}
          />
          <SummaryCard
            label={advanced ? t('activity.crossMachine') : t('activity.teamRuns')}
            value={String(summary.teamRuns)}
          />
        </div>
      )}

      {runs.length > 0 && (
        <div className="flex flex-wrap gap-2">
          <select
            className="field py-1.5 text-xs"
            value={filterComputer}
            onChange={(e) => setFilterComputer(e.target.value)}
            aria-label={t('activity.filterComputer')}
          >
            <option value="">{t('activity.computer')}</option>
            {computers.map((c) => (
              <option key={c} value={c}>
                {c}
              </option>
            ))}
          </select>
          <select
            className="field py-1.5 text-xs"
            value={filterModel}
            onChange={(e) => setFilterModel(e.target.value)}
            aria-label={t('activity.filterModel')}
          >
            <option value="">{t('activity.model')}</option>
            {modelOptions.map((id) => (
              <option key={id} value={id}>
                {modelDisplayName(models, id)}
              </option>
            ))}
          </select>
          <select
            className="field py-1.5 text-xs"
            value={filterProfile}
            onChange={(e) => setFilterProfile(e.target.value)}
            aria-label={t('activity.filterProfile')}
          >
            <option value="">{t('activity.profile')}</option>
            {profiles.map((p) => (
              <option key={p} value={p}>
                {p}
              </option>
            ))}
          </select>
          {anyRanOn ? (
            <select
              className="field py-1.5 text-xs"
              value={filterHardware}
              onChange={(e) => setFilterHardware(e.target.value as '' | 'gpu' | 'cpu')}
              aria-label={t('activity.filterHardware')}
            >
              <option value="">{t('activity.hardware')}</option>
              <option value="gpu">{t('activity.onGPU')}</option>
              <option value="cpu">{t('activity.onCPU')}</option>
            </select>
          ) : null}
          <input
            type="date"
            className="field py-1.5 text-xs"
            value={filterDate}
            onChange={(e) => setFilterDate(e.target.value)}
            aria-label={t('activity.filterDate')}
          />
        </div>
      )}

      {performanceQuery.isLoading && <Skeleton label={t('activity.loading')} />}

      {performanceQuery.isError && !performanceQuery.data && (
        <LoadError error={performanceQuery.error} onRetry={() => void performanceQuery.refetch()} retrying={performanceQuery.isFetching} />
      )}
      {!performanceQuery.isLoading && !performanceQuery.isError && runs.length === 0 && (
        <EmptyState
          title={t('activity.emptyTitle')}
          description={t('activity.emptyDescription')}
        />
      )}

      {filtered.length > 0 && (
        <ul className="card divide-y divide-line overflow-hidden p-0">
          {filtered.map((run) => (
            <ActivityRow
              key={run.id}
              run={run}
              models={models}
              open={expandedId === run.id}
              onToggle={() =>
                setExpandedId((current) => (current === run.id ? null : run.id))
              }
            />
          ))}
        </ul>
      )}

      {runs.length > 0 && filtered.length === 0 && (
        <p className="text-sm text-ink-muted">{t('activity.noMatches')}</p>
      )}
    </div>
  )
}
