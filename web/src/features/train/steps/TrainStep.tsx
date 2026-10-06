import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '@/lib/api'
import { formatBytes } from '@/lib/format'
import type { SpecializedAIView, TrainingJob } from '@/types/api'
import { elapsedSec, formatDuration, isTerminal, jobPercent, jobStages, stateLabel } from '../display'
import { formatDecimal, formatGigabytes } from '@/i18n/format'

export function TrainStep({ view, onNext }: { view: SpecializedAIView; onNext: () => void }) {
  const { t } = useTranslation('train')
  const [now, setNow] = useState(() => Date.now())
  const job = view.jobs[0]
  const running = job && !isTerminal(job.state)
  useEffect(() => {
    if (!running) return
    const t = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(t)
  }, [running])

  if (!job) {
    return <div className="card text-sm text-ink-muted">{t('train.none')}</div>
  }
  return (
    <div className="space-y-4">
      <JobCard job={job} now={now} />
      {job.state === 'complete' && (
        <button type="button" className="btn-primary" onClick={onNext}>
          {t('train.compare')}
        </button>
      )}
      {view.jobs.length > 1 && (
        <div className="card space-y-2">
          <h3 className="section-title">{t('train.earlier')}</h3>
          <ul className="divide-y divide-line/60 text-sm">
            {view.jobs.slice(1).map((j) => (
              <li key={j.id} className="flex flex-wrap items-center justify-between gap-2 py-1.5">
                <span className="text-ink">{t('train.revision', { revision: j.revision })}</span>
                <span className="text-ink-muted">
                  {stateLabel(j.state)} · {formatDuration(elapsedSec(j))}
                  {j.error && <span className="text-danger"> · {j.error}</span>}
                </span>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  )
}

function JobCard({ job, now }: { job: TrainingJob; now: number }) {
  const { t } = useTranslation('train')
  const queryClient = useQueryClient()
  const cancel = useMutation({
    mutationFn: () => api.cancelTraining(job.id),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['training'] }),
  })
  const p = job.progress
  const terminal = isTerminal(job.state)
  const currentIndex = jobStages.indexOf(job.state)
  const downloading = job.state === 'loading_model' && (p.download_total ?? 0) > 0 && (p.download_bytes ?? 0) < (p.download_total ?? 0)

  return (
    <div className="card space-y-4">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div>
          <h3 className="section-title">{t('train.revision', { revision: job.revision })}</h3>
          <p className="text-xs text-ink-muted">
            {t('train.on', {
              computer: job.node_name || job.node_id,
              backend: job.backend.toUpperCase(),
              method: job.hyper.method?.toUpperCase(),
              epochs: job.hyper.epochs,
            })}
          </p>
        </div>
        {!terminal && (
          <button type="button" className="btn-secondary btn-sm" disabled={cancel.isPending} onClick={() => cancel.mutate()}>
            {cancel.isPending ? t('train.cancelling') : t('train.cancel')}
          </button>
        )}
      </div>

      <ol className="flex flex-wrap gap-1 text-xs" aria-label={t('train.stages')}>
        {jobStages.map((s, i) => {
          const state =
            job.state === 'failed' || job.state === 'cancelled'
              ? i < Math.max(currentIndex, 0)
                ? 'done'
                : 'todo'
              : i < currentIndex || job.state === 'complete'
                ? 'done'
                : i === currentIndex
                  ? 'now'
                  : 'todo'
          return (
            <li
              key={s}
              className={[
                'rounded-md px-2 py-1',
                state === 'done' ? 'bg-primary-soft text-primary-active' : state === 'now' ? 'bg-primary text-primary-fg' : 'bg-raised text-ink-faint',
              ].join(' ')}
            >
              {stateLabel(s)}
            </li>
          )
        })}
      </ol>

      {job.state === 'failed' && (
        <p className="rounded-md bg-danger/10 p-3 text-sm text-danger">
          {t('train.failed', { error: job.error || t('train.failedDefault') })}
        </p>
      )}
      {job.state === 'cancelled' && (
        <p className="rounded-md bg-raised p-3 text-sm text-ink-muted">{t('train.cancelled')}</p>
      )}

      {!terminal && (
        <div className="space-y-1">
          <div className="flex justify-between text-xs text-ink-muted">
            <span>{p.detail || stateLabel(job.state)}</span>
            {p.iters ? (
              <span className="tabular-nums">{t('train.step', { iter: p.iter ?? 0, iters: p.iters })}</span>
            ) : null}
          </div>
          <div
            className="h-2 overflow-hidden rounded-full bg-raised"
            role="progressbar"
            aria-label={p.detail || stateLabel(job.state)}
            aria-valuenow={jobPercent(job)}
            aria-valuemin={0}
            aria-valuemax={100}
          >
            <div
              className="h-full bg-primary transition-[width] duration-500"
              style={{ width: `${downloading ? Math.round(((p.download_bytes ?? 0) / (p.download_total ?? 1)) * 100) : jobPercent(job)}%` }}
            />
          </div>
          {downloading && (
            <p className="text-xs text-ink-faint">
              {t('train.downloaded', { done: formatBytes(p.download_bytes), total: formatBytes(p.download_total) })}
            </p>
          )}
        </div>
      )}

      <dl className="grid grid-cols-2 gap-x-4 gap-y-2 text-sm sm:grid-cols-4">
        <Metric label={t('train.elapsed')} value={formatDuration(elapsedSec(job, now))} />
        <Metric
          label={t('train.remaining')}
          value={p.remaining_sec != null && !terminal ? t('train.about', { time: formatDuration(p.remaining_sec) }) : '—'}
        />
        <Metric
          label={t('train.epoch')}
          value={p.epoch != null ? t('train.epochOf', { epoch: formatDecimal(p.epoch, 1), epochs: p.epochs ?? job.hyper.epochs }) : '—'}
        />
        <Metric label={t('train.speed')} value={p.tokens_per_sec ? t('train.tokensPerSec', { count: Math.round(p.tokens_per_sec) }) : '—'} />
        <Metric label={t('train.trainingLoss')} value={p.train_loss != null ? formatDecimal(p.train_loss, 3) : '—'} />
        <Metric label={t('train.validationLoss')} value={p.val_loss != null ? formatDecimal(p.val_loss, 3) : '—'} />
        <Metric label={t('train.peakMemory')} value={p.peak_memory_gb ? formatGigabytes(p.peak_memory_gb) : '—'} />
      </dl>
      <p className="text-xs text-ink-faint">{t('train.lossNote')}</p>
    </div>
  )
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-xs text-ink-faint">{label}</dt>
      <dd className="tabular-nums text-ink">{value}</dd>
    </div>
  )
}

