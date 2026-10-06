import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { Trans, useTranslation } from 'react-i18next'
import { api } from '@/lib/api'
import { KnowledgePicker } from '@/features/knowledge/KnowledgePicker'
import { formatBytes } from '@/lib/format'
import type { NodeTrainingFit, SpecializedAIView, TrainingHyper, TrainingPreset } from '@/types/api'
import { errorText, fitLabel, fitTone, formatDuration, PRESETS, presetDescription, presetLabel } from '../display'

export function PlanStep({ view, onStarted }: { view: SpecializedAIView; onStarted: () => void }) {
  const { t } = useTranslation('train')
  const queryClient = useQueryClient()
  const plan = useQuery({ queryKey: ['training', 'plan', view.id], queryFn: () => api.trainingPlan(view.id) })
  const refresh = () => void queryClient.invalidateQueries({ queryKey: ['training'] })

  const setKnowledge = useMutation({
    mutationFn: (ids: string[]) => api.updateAI(view.id, { knowledge_sources: ids }),
    onSuccess: refresh,
  })
  const setPreset = useMutation({ mutationFn: (preset: TrainingPreset) => api.updateAI(view.id, { preset }), onSuccess: refresh })
  const start = useMutation({
    mutationFn: (nodeId?: string) => api.startTraining(view.id, nodeId),
    onSuccess: () => {
      refresh()
      onStarted()
    },
  })

  const p = plan.data
  return (
    <div className="space-y-4">
      <div className="grid gap-3 md:grid-cols-2">
        <div className="card-outline space-y-2 border-s-4 !border-s-primary p-4">
          <p className="label-caps text-primary">{t('plan.willTrain')}</p>
          <p className="text-sm text-ink">
            <Trans
              t={t}
              i18nKey="plan.examples"
              count={p?.examples ?? view.dataset.usable}
              components={{ strong: <span className="font-semibold tabular-nums" /> }}
            />
          </p>
          <p className="text-xs text-ink-muted">{t('plan.base', { model: view.base_model?.display_name ?? t('plan.notChosen') })}</p>
        </div>
        <div className="card-outline space-y-2 border-s-4 !border-s-mimir p-4">
          <p className="label-caps text-mimir">{t('plan.connected')}</p>
          <KnowledgePicker selected={view.knowledge_sources} disabled={setKnowledge.isPending} onChange={(ids) => setKnowledge.mutate(ids)} />
          {setKnowledge.error && <p className="text-xs text-danger">{errorText(setKnowledge.error)}</p>}
          <p className="text-xs text-ink-muted">{t('plan.connectedHint')}</p>
        </div>
      </div>

      <div className="card space-y-3">
        <h3 className="section-title">{t('plan.effort')}</h3>
        <div className="grid gap-2 md:grid-cols-3">
          {PRESETS.map((key) => (
            <button
              key={key}
              type="button"
              className={['selectable text-start', view.preset === key ? 'shadow-[inset_0_0_0_1.5px_rgb(var(--rgb-primary))]' : ''].join(' ')}
              onClick={() => setPreset.mutate(key)}
            >
              <p className="font-medium text-ink">{presetLabel(key)}</p>
              <p className="mt-1 text-xs text-ink-muted">{presetDescription(key)}</p>
            </button>
          ))}
        </div>
        <AdvancedSettings view={view} planned={p?.hyper} />
      </div>

      <div className="card space-y-3">
        <h3 className="section-title">{t('plan.where')}</h3>
        {plan.isLoading && <p className="text-sm text-ink-muted">{t('plan.estimating')}</p>}
        <ul className="space-y-2">
          {(p?.fits ?? []).map((f) => (
            <FitRow
              key={f.node_id || f.node_name}
              fit={f}
              chosen={p?.chosen?.node_id === f.node_id}
              canStart={Boolean(p?.ready) && !start.isPending}
              onStart={() => start.mutate(f.node_id)}
            />
          ))}
        </ul>
      </div>

      {p && (p.blockers.length > 0 || p.warnings.length > 0) && (
        <div className="space-y-2">
          {p.blockers.map((b) => (
            <p key={b} className="rounded-md bg-danger/10 p-2 text-sm text-danger">
              {b}
            </p>
          ))}
          {p.warnings.map((w) => (
            <p key={w} className="rounded-md bg-warning/10 p-2 text-sm text-warning">
              {w}
            </p>
          ))}
        </div>
      )}
      {start.error && <p className="text-sm text-danger">{errorText(start.error)}</p>}
      <button type="button" className="btn-primary" disabled={!p?.ready || start.isPending} onClick={() => start.mutate(undefined)}>
        {start.isPending ? t('plan.starting') : t('plan.train', { revision: p?.next_revision ?? 1 })}
      </button>
    </div>
  )
}

function FitRow({
  fit,
  chosen,
  canStart,
  onStart,
}: {
  fit: NodeTrainingFit
  chosen: boolean
  canStart: boolean
  onStart: () => void
}) {
  const { t } = useTranslation('train')
  return (
    <li className={['rounded-lg bg-raised p-3 text-sm', chosen ? 'shadow-[inset_0_0_0_1.5px_rgb(var(--rgb-primary))]' : ''].join(' ')}>
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-medium text-ink">{fit.node_name}</span>
        {fit.local && <span className="text-xs text-ink-faint">{t('plan.thisComputer')}</span>}
        <span className={['status-chip', fitTone(fit)].join(' ')}>{fitLabel(fit.label)}</span>
        {chosen && <span className="status-chip bg-norn/15 text-norn">{t('plan.nornPicked')}</span>}
        {!chosen && fit.eligible && (
          <button type="button" className="btn-secondary btn-sm ms-auto" disabled={!canStart} onClick={onStart}>
            {t('plan.trainHere')}
          </button>
        )}
      </div>
      <p className="mt-1 text-ink-muted">{fit.reason}</p>
      {fit.eligible && (
        <dl className="mt-2 grid grid-cols-2 gap-x-4 gap-y-1 text-xs sm:grid-cols-4">
          <div>
            <dt className="text-ink-faint">{t('plan.memory')}</dt>
            <dd className="tabular-nums text-ink">
              {t('plan.memoryOf', { needed: formatBytes(fit.memory_needed_bytes), available: formatBytes(fit.memory_available_bytes) })}
            </dd>
          </div>
          <div>
            <dt className="text-ink-faint">{t('plan.download')}</dt>
            <dd className="tabular-nums text-ink">{fit.download_bytes ? formatBytes(fit.download_bytes) : t('plan.none')}</dd>
          </div>
          <div>
            <dt className="text-ink-faint">{t('plan.disk')}</dt>
            <dd className="tabular-nums text-ink">{formatBytes(fit.storage_needed_bytes)}</dd>
          </div>
          <div>
            <dt className="text-ink-faint">{t('plan.time')}</dt>
            <dd className="tabular-nums text-ink">{formatDuration(fit.duration_sec)}</dd>
          </div>
        </dl>
      )}
      {(fit.notes ?? []).map((n) => (
        <p key={n} className="mt-1 text-xs text-ink-muted">
          {n}
        </p>
      ))}
    </li>
  )
}

// Each field's label and help are train:plan.fields.<key> in the catalog.
const advancedFields: { key: keyof TrainingHyper; step?: string }[] = [
  { key: 'epochs' },
  { key: 'rank' },
  { key: 'layers' },
  { key: 'learning_rate', step: 'any' },
  { key: 'batch_size' },
  { key: 'max_seq_length' },
]

function AdvancedSettings({ view, planned }: { view: SpecializedAIView; planned?: TrainingHyper }) {
  const { t } = useTranslation('train')
  const queryClient = useQueryClient()
  const [draft, setDraft] = useState<TrainingHyper>(view.advanced ?? {})
  const save = useMutation({
    mutationFn: (clear: boolean) => api.updateAI(view.id, clear ? { clear_advanced: true } : { advanced: draft }),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['training'] }),
  })
  return (
    <details className="text-sm" open={Boolean(view.advanced)}>
      <summary className="cursor-pointer text-ink-muted">{t('plan.advanced')}</summary>
      <div className="mt-3 space-y-3">
        <p className="text-xs text-ink-muted">
          {t('plan.advancedHint', {
            preset: planned ? t('plan.presetSummary', { method: planned.method?.toUpperCase(), iters: planned.iters }) : '…',
          })}
        </p>
        <label className="flex items-center gap-2 text-xs">
          <span className="w-28 text-ink">{t('plan.method')}</span>
          <select
            className="field"
            value={draft.method ?? ''}
            onChange={(e) => setDraft({ ...draft, method: (e.target.value || undefined) as TrainingHyper['method'] })}
          >
            <option value="">{t('plan.presetMethod', { method: planned?.method ?? '—' })}</option>
            <option value="lora">{t('plan.lora')}</option>
            <option value="qlora">{t('plan.qlora')}</option>
          </select>
        </label>
        <div className="grid gap-2 sm:grid-cols-2">
          {advancedFields.map((f) => (
            <label key={f.key} className="flex flex-col gap-0.5 text-xs" title={t(`plan.fields.${f.key}.help`)}>
              <span className="text-ink">{t(`plan.fields.${f.key}.label`)}</span>
              <input
                type="number"
                step={f.step ?? '1'}
                className="field"
                placeholder={planned?.[f.key] != null ? String(planned[f.key]) : ''}
                value={draft[f.key] != null ? String(draft[f.key]) : ''}
                onChange={(e) => setDraft({ ...draft, [f.key]: e.target.value === '' ? undefined : Number(e.target.value) })}
              />
              <span className="text-ink-faint">{t(`plan.fields.${f.key}.help`)}</span>
            </label>
          ))}
        </div>
        {save.error && <p className="text-danger">{errorText(save.error)}</p>}
        <div className="flex gap-2">
          <button type="button" className="btn-secondary btn-sm" disabled={save.isPending} onClick={() => save.mutate(false)}>
            {t('plan.saveAdvanced')}
          </button>
          {view.advanced && (
            <button type="button" className="btn-secondary btn-sm" disabled={save.isPending} onClick={() => { setDraft({}); save.mutate(true) }}>
              {t('plan.usePreset')}
            </button>
          )}
        </div>
      </div>
    </details>
  )
}
