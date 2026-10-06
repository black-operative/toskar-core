import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import i18n from '@/i18n'
import { RanOnTag } from './RanOnTag'
import { LoadingSpinner } from '@/components/ui/LoadingSpinner'
import { api } from '@/lib/api'
import { useUIStore } from '@/stores/uiStore'
import type {
  BenchmarkJob,
  BenchmarkModelSummary,
  BenchmarkWorkload,
  Model,
} from '@/types/api'
import {
  estimateBenchmarkMinutes,
  formatMs,
  formatRate,
  formatWhen,
  metricLabels,
  modelDisplayName,
} from './performanceFormat'
import { formatPercent } from '@/i18n/format'

const JOB_STATUSES = ['pending', 'running', 'completed', 'failed', 'cancelled', 'canceled']

/** A benchmark job's status in the App language, or as the computer sent it. */
function jobStatus(status: string): string {
  return JOB_STATUSES.includes(status) ? i18n.t(`performance:benchmark.statuses.${status}`) : status
}

function SelectCard({
  selected,
  title,
  subtitle,
  meta,
  onClick,
}: {
  selected: boolean
  title: string
  subtitle?: string
  meta?: string
  onClick: () => void
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={selected}
      className={[
        'min-w-0 rounded-xl p-4 text-start transition duration-150',
        selected
          ? 'bg-primary-soft shadow-[inset_0_0_0_1.5px_rgb(var(--rgb-primary)/0.55)]'
          : 'bg-surface shadow-[inset_0_0_0_1px_rgb(var(--rgb-line)/0.55)] hover:bg-raised/70',
      ].join(' ')}
    >
      <p
        className={[
          'font-semibold',
          selected ? 'text-primary-active' : 'text-ink',
        ].join(' ')}
      >
        {title}
      </p>
      {subtitle ? <p className="mt-1 text-sm text-ink-muted">{subtitle}</p> : null}
      {meta ? <p className="mt-2 text-xs text-ink-faint">{meta}</p> : null}
    </button>
  )
}

function VisualBars({
  rows,
  unit,
  higherIsBetter,
}: {
  rows: { id: string; label: string; value: number; winner?: boolean }[]
  unit: string
  higherIsBetter: boolean
}) {
  const { t } = useTranslation('performance')
  const max = Math.max(...rows.map((r) => r.value), 0.001)
  const sorted = [...rows].sort((a, b) =>
    higherIsBetter ? b.value - a.value : a.value - b.value,
  )
  return (
    <ul className="space-y-2">
      {sorted.map((r) => {
        const pct = Math.max(4, Math.round((r.value / max) * 100))
        return (
          <li key={r.id} className="space-y-1">
            <div className="flex items-baseline justify-between gap-2 text-sm">
              <span className={r.winner ? 'font-semibold text-ink' : 'text-ink'}>
                {r.label}
                {r.winner ? (
                  <span className="badge-preferred ms-2">{t('benchmark.fastest')}</span>
                ) : null}
              </span>
              <span className="shrink-0 tabular-nums text-ink-muted">
                {higherIsBetter ? formatRate(r.value) : formatMs(r.value)}
                {higherIsBetter ? ` ${unit}` : ''}
              </span>
            </div>
            <div className="h-2 overflow-hidden rounded-full bg-raised">
              <div
                className={[
                  'h-full rounded-full transition-all',
                  r.winner ? 'bg-accent' : 'bg-primary/70',
                ].join(' ')}
                style={{ width: `${pct}%` }}
              />
            </div>
          </li>
        )
      })}
    </ul>
  )
}

/** Where a model's benchmark ran, from its first sample that says (#317). */
function ranOnFor(job: BenchmarkJob, modelId: string): { backend?: string; device?: string } {
  const sample = (job.samples ?? []).find((s) => s.model_id === modelId && s.backend)
  return { backend: sample?.backend, device: sample?.device }
}

function BenchmarkResults({
  job,
  workloads,
  models,
}: {
  job: BenchmarkJob
  workloads: BenchmarkWorkload[]
  models: Model[]
}) {
  const { t } = useTranslation('performance')
  const advanced = useUIStore((s) => s.advancedMode)
  const labels = metricLabels(advanced)
  const [showDetail, setShowDetail] = useState(false)

  const byWorkload = useMemo(() => {
    const map = new Map<string, BenchmarkModelSummary[]>()
    for (const row of job.summaries ?? []) {
      const list = map.get(row.workload_id) ?? []
      list.push(row)
      map.set(row.workload_id, list)
    }
    return [...map.entries()]
  }, [job.summaries])

  const workloadName = (id: string) => workloads.find((w) => w.id === id)?.name ?? id

  return (
    <section className="card space-y-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="section-title">{t('benchmark.results')}</h2>
          <p className="mt-1 text-sm text-ink-muted">{t('benchmark.status', { status: jobStatus(job.status) })}</p>
        </div>
        {(job.status === 'running' || job.status === 'pending') && (
          <div className="min-w-[220px] flex-1">
            <div className="mb-1 flex justify-between text-xs text-ink-muted">
              <span>{job.progress.message || job.progress.phase}</span>
              <span className="tabular-nums">{formatPercent(job.progress.percent / 100)}</span>
            </div>
            <div className="h-2 overflow-hidden rounded-full bg-raised">
              <div
                className="h-full bg-primary transition-all"
                style={{ width: `${Math.min(job.progress.percent, 100)}%` }}
              />
            </div>
          </div>
        )}
      </div>

      {job.error && (
        <div className="rounded-lg border border-danger/30 bg-danger/10 px-4 py-3 text-sm text-danger">
          {job.error}
        </div>
      )}

      {Object.keys(job.winners ?? {}).length > 0 && (
        <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
          {Object.entries(job.winners).map(([workloadId, modelId]) => (
            <div
              key={workloadId}
              className="rounded-lg border border-accent/30 bg-accent-soft px-4 py-3"
            >
              <p className="label-caps text-accent">
                {t(job.status === 'completed' ? 'benchmark.winner' : 'benchmark.leading', {
                  workload: workloadName(workloadId),
                })}
              </p>
              <p className="mt-1 font-medium text-ink">
                {modelDisplayName(models, modelId)}
              </p>
            </div>
          ))}
        </div>
      )}

      {byWorkload.length > 0 && (
        <div className="space-y-6">
          {byWorkload.map(([workloadId, rows]) => (
            <div key={workloadId} className="space-y-4">
              <h3 className="font-display text-base font-semibold text-ink">
                {workloadName(workloadId)}
              </h3>
              <div>
                <p className="mb-2 text-xs font-medium text-ink-muted">{labels.speed}</p>
                <VisualBars
                  higherIsBetter
                  unit="tok/s"
                  rows={rows.map((r) => ({
                    id: r.model_id,
                    label: modelDisplayName(models, r.model_id),
                    value: r.avg_eval_tok_per_sec,
                    winner: job.winners?.[workloadId] === r.model_id,
                  }))}
                />
              </div>
              <div>
                <p className="mb-2 text-xs font-medium text-ink-muted">
                  {labels.firstResponse}
                </p>
                <VisualBars
                  higherIsBetter={false}
                  unit=""
                  rows={rows.map((r) => ({
                    id: r.model_id,
                    label: modelDisplayName(models, r.model_id),
                    value: r.avg_ttft_ms,
                    winner:
                      Math.min(...rows.map((x) => x.avg_ttft_ms || Infinity)) ===
                      r.avg_ttft_ms,
                  }))}
                />
              </div>
            </div>
          ))}
        </div>
      )}

      {byWorkload.length > 0 && (
        <div>
          <button
            type="button"
            className="text-sm text-primary hover:underline"
            onClick={() => setShowDetail((o) => !o)}
          >
            {showDetail ? t('benchmark.hideDetail') : t('benchmark.detail')}
          </button>
          {showDetail && (
            <div className="mt-3 space-y-4">
              {byWorkload.map(([workloadId, rows]) => (
                <div
                  key={workloadId}
                  className="min-w-0 overflow-hidden rounded-xl border border-line"
                >
                  <div className="border-b border-line bg-raised/50 px-4 py-2">
                    <p className="text-sm font-medium text-ink">
                      {workloadName(workloadId)}
                    </p>
                  </div>
                  <div className="table-scroll">
                    <table className="min-w-full text-start text-sm">
                      <thead>
                        <tr className="border-b border-line text-ink-muted">
                          <th className="px-3 py-2 font-medium">{t('benchmark.model')}</th>
                          <th className="px-3 py-2 font-medium">{t('ranOn.label')}</th>
                          <th className="px-3 py-2 text-end font-medium">
                            {labels.speed}
                          </th>
                          <th className="px-3 py-2 text-end font-medium">
                            {labels.prompt}
                          </th>
                          <th className="px-3 py-2 text-end font-medium">
                            {labels.firstResponse}
                          </th>
                          <th className="px-3 py-2 text-end font-medium">
                            {labels.total}
                          </th>
                          {advanced && (
                            <th className="px-3 py-2 text-end font-medium">{t('benchmark.load')}</th>
                          )}
                        </tr>
                      </thead>
                      <tbody className="divide-y divide-line">
                        {rows.map((row) => (
                          <tr key={`${row.model_id}-${row.workload_id}`}>
                            <td className="px-3 py-2.5 text-ink">
                              {modelDisplayName(models, row.model_id)}
                            </td>
                            <td className="px-3 py-2.5">
                              <RanOnTag {...ranOnFor(job, row.model_id)} />
                            </td>
                            <td className="px-3 py-2.5 text-end tabular-nums">
                              {formatRate(row.avg_eval_tok_per_sec)}
                            </td>
                            <td className="px-3 py-2.5 text-end tabular-nums">
                              {formatRate(row.avg_prompt_tok_per_sec)}
                            </td>
                            <td className="px-3 py-2.5 text-end tabular-nums">
                              {formatMs(row.avg_ttft_ms)}
                            </td>
                            <td className="px-3 py-2.5 text-end tabular-nums">
                              {formatMs(row.avg_total_ms)}
                            </td>
                            {advanced && (
                              <td className="px-3 py-2.5 text-end tabular-nums">
                                {formatMs(row.load_ms ?? 0)}
                              </td>
                            )}
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {(job.status === 'running' || job.status === 'pending') && byWorkload.length === 0 && (
        <LoadingSpinner label={job.progress.message || t('benchmark.running')} />
      )}
    </section>
  )
}

export function BenchmarkPanel() {
  const { t } = useTranslation('performance')
  const queryClient = useQueryClient()
  const advanced = useUIStore((s) => s.advancedMode)
  const [selectedModels, setSelectedModels] = useState<string[]>([])
  const [selectedWorkloads, setSelectedWorkloads] = useState<string[]>([])
  const [runsPerPrompt, setRunsPerPrompt] = useState(2)
  const [activeJobId, setActiveJobId] = useState<string | null>(null)
  const [benchError, setBenchError] = useState<string | null>(null)

  const modelsQuery = useQuery({
    queryKey: ['models'],
    queryFn: () => api.getModels(),
    retry: false,
  })

  const workloadsQuery = useQuery({
    queryKey: ['benchmark-workloads'],
    queryFn: () => api.listBenchmarkWorkloads(),
    retry: false,
    staleTime: 60_000,
  })

  const jobQuery = useQuery({
    queryKey: ['benchmark', activeJobId],
    queryFn: () => (activeJobId ? api.getBenchmark(activeJobId) : null),
    enabled: Boolean(activeJobId),
    refetchInterval: (query) => {
      const status = query.state.data?.status
      if (status === 'running' || status === 'pending') return 1200
      return false
    },
    retry: false,
  })

  const jobsQuery = useQuery({
    queryKey: ['benchmarks'],
    queryFn: () => api.listBenchmarks(),
    retry: false,
    refetchInterval: (query) => {
      const items = query.state.data ?? []
      return items.some((j) => j.status === 'running' || j.status === 'pending')
        ? 2000
        : false
    },
  })

  const models = modelsQuery.data ?? []
  const installedModels = useMemo(
    () =>
      models.filter(
        (m) => m.installed || (m.installed_on?.length ?? 0) > 0,
      ),
    [models],
  )
  const workloads = workloadsQuery.data ?? []
  const activeJob = jobQuery.data ?? null
  const recentJobs = jobsQuery.data ?? []

  const promptCount = useMemo(() => {
    return workloads
      .filter((w) => selectedWorkloads.includes(w.id))
      .reduce((n, w) => n + w.prompts.length, 0)
  }, [workloads, selectedWorkloads])

  const estMinutes = estimateBenchmarkMinutes(
    selectedModels.length,
    promptCount,
    runsPerPrompt,
  )

  const startMutation = useMutation({
    mutationFn: () =>
      api.startBenchmark({
        model_ids: selectedModels,
        workload_ids: selectedWorkloads,
        runs: runsPerPrompt,
      }),
    onSuccess: (job) => {
      if (job?.id) {
        setActiveJobId(job.id)
        setBenchError(null)
        queryClient.invalidateQueries({ queryKey: ['benchmark', job.id] })
        queryClient.invalidateQueries({ queryKey: ['benchmarks'] })
      }
    },
    onError: (error) => {
      setBenchError(error instanceof Error ? error.message : t('benchmark.startFailed'))
    },
  })

  const cancelMutation = useMutation({
    mutationFn: () =>
      activeJobId ? api.cancelBenchmark(activeJobId) : Promise.resolve(null),
    onSuccess: () => {
      if (activeJobId) {
        queryClient.invalidateQueries({ queryKey: ['benchmark', activeJobId] })
        queryClient.invalidateQueries({ queryKey: ['benchmarks'] })
      }
    },
  })

  const canStart =
    selectedModels.length >= 1 &&
    selectedWorkloads.length >= 1 &&
    !startMutation.isPending &&
    activeJob?.status !== 'running' &&
    activeJob?.status !== 'pending'

  const toggleModel = (id: string) => {
    setSelectedModels((current) =>
      current.includes(id) ? current.filter((x) => x !== id) : [...current, id].slice(0, 6),
    )
  }

  const toggleWorkload = (id: string) => {
    setSelectedWorkloads((current) =>
      current.includes(id) ? current.filter((x) => x !== id) : [...current, id],
    )
  }

  return (
    <div className="space-y-6 animate-fade">
      <section className="card space-y-6">
        <div>
          <h2 className="section-title">{t('benchmark.title')}</h2>
          <p className="mt-1 text-sm text-ink-muted">{t('benchmark.description')}</p>
          <ol className="mt-4 flex flex-wrap gap-3 text-sm">
            {[
              { n: 1, label: t('benchmark.chooseModels'), done: selectedModels.length > 0 },
              { n: 2, label: t('benchmark.chooseWorkloads'), done: selectedWorkloads.length > 0 },
              { n: 3, label: t('benchmark.run'), done: false },
            ].map((step) => (
              <li
                key={step.n}
                className={[
                  'inline-flex items-center gap-2 rounded-full px-3 py-1',
                  step.done ? 'bg-success/15 text-success' : 'bg-raised text-ink-muted',
                ].join(' ')}
              >
                <span className="font-semibold tabular-nums">{step.n}</span>
                {step.label}
              </li>
            ))}
          </ol>
        </div>

        <div>
          <p className="label-caps mb-2">{t('benchmark.step', { n: 1, label: t('benchmark.chooseModels') })}</p>
          {modelsQuery.isLoading && <LoadingSpinner label={t('benchmark.loadingModels')} />}
          {!modelsQuery.isLoading && installedModels.length === 0 && (
            <p className="text-sm text-ink-muted">{t('benchmark.installFirst')}</p>
          )}
          <div className="grid gap-2 sm:grid-cols-2">
            {installedModels.map((model) => (
              <SelectCard
                key={model.id}
                selected={selectedModels.includes(model.id)}
                title={model.display_name}
                subtitle={advanced ? model.id : undefined}
                onClick={() => toggleModel(model.id)}
              />
            ))}
          </div>
          <p className="mt-2 text-xs text-ink-muted">{t('benchmark.selected', { count: selectedModels.length })}</p>
        </div>

        <div>
          <p className="label-caps mb-2">{t('benchmark.step', { n: 2, label: t('benchmark.chooseWorkloads') })}</p>
          <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
            {workloads.map((workload) => (
              <SelectCard
                key={workload.id}
                selected={selectedWorkloads.includes(workload.id)}
                title={workload.name}
                subtitle={workload.description}
                meta={t('benchmark.prompts', { count: workload.prompts.length })}
                onClick={() => toggleWorkload(workload.id)}
              />
            ))}
          </div>
        </div>

        <div className="space-y-3 border-t border-line/60 pt-5">
          <p className="label-caps">{t('benchmark.step', { n: 3, label: t('benchmark.run') })}</p>
          <div className="rounded-lg border border-warning/30 bg-warning/10 px-4 py-3 text-sm text-ink">
            {t('benchmark.warning')}
          </div>
          <div className="flex flex-wrap items-end gap-4">
            {advanced && (
              <label className="text-sm text-ink-muted">
                {t('benchmark.runsPerPrompt')}
                <select
                  className="field mt-1 block"
                  value={runsPerPrompt}
                  onChange={(e) => setRunsPerPrompt(Number(e.target.value))}
                >
                  {[1, 2, 3].map((n) => (
                    <option key={n} value={n}>
                      {n}
                    </option>
                  ))}
                </select>
              </label>
            )}
            <button
              type="button"
              className="btn-primary"
              disabled={!canStart}
              onClick={() => startMutation.mutate()}
            >
              {startMutation.isPending ? t('benchmark.starting') : t('benchmark.run')}
            </button>
            {activeJob &&
              (activeJob.status === 'running' || activeJob.status === 'pending') && (
                <button
                  type="button"
                  className="btn-secondary"
                  disabled={cancelMutation.isPending}
                  onClick={() => cancelMutation.mutate()}
                >
                  {t('benchmark.cancel')}
                </button>
              )}
          </div>
          {selectedModels.length > 0 && selectedWorkloads.length > 0 && (
            <p className="text-xs text-ink-faint">
              {t('benchmark.estimate', {
                models: t('benchmark.models', { count: selectedModels.length }),
                workloads: t('benchmark.workloads', { count: selectedWorkloads.length }),
                minutes: t('benchmark.minutes', { count: estMinutes }),
              })}
            </p>
          )}
        </div>

        {benchError && (
          <div className="rounded-lg border border-danger/30 bg-danger/10 px-4 py-3 text-sm text-danger">
            {benchError}
          </div>
        )}
      </section>

      {activeJob && (
        <BenchmarkResults job={activeJob} workloads={workloads} models={models} />
      )}

      {recentJobs.length > 0 && (
        <section className="card space-y-3">
          <div>
            <h2 className="section-title">{t('benchmark.recent')}</h2>
            <p className="mt-1 text-sm text-ink-muted">{t('benchmark.recentHint')}</p>
          </div>
          <ul className="divide-y divide-line rounded-xl border border-line">
            {recentJobs.slice(0, 8).map((job) => {
              const winnerCount = Object.keys(job.winners ?? {}).length
              const active = job.id === activeJobId
              return (
                <li key={job.id}>
                  <button
                    type="button"
                    onClick={() => setActiveJobId(job.id)}
                    className={[
                      'flex w-full items-center justify-between gap-3 px-4 py-3 text-start transition hover:bg-raised/50',
                      active ? 'bg-primary-soft/40' : '',
                    ].join(' ')}
                  >
                    <div>
                      <p className="font-medium text-ink">{jobStatus(job.status)}</p>
                      <p className="mt-0.5 text-xs text-ink-muted">
                        {t('benchmark.jobSummary', {
                          models: t('benchmark.models', { count: job.request.model_ids.length }),
                          workloads: t('benchmark.workloads', { count: job.request.workload_ids.length }),
                          when: formatWhen(job.created_at),
                        })}
                      </p>
                    </div>
                    <div className="text-end text-xs text-ink-muted">
                      {winnerCount > 0 ? (
                        <span className="text-accent">{t('benchmark.winners', { count: winnerCount })}</span>
                      ) : (
                        <span className="tabular-nums">{formatPercent(job.progress.percent / 100)}</span>
                      )}
                    </div>
                  </button>
                </li>
              )
            })}
          </ul>
        </section>
      )}
    </div>
  )
}
