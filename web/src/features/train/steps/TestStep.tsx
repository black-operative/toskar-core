import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '@/lib/api'
import type { SpecializedAIView } from '@/types/api'
import { errorText } from '../display'
import { formatDateTime } from '@/i18n/format'

export function TestStep({ view, onNext }: { view: SpecializedAIView; onNext: () => void }) {
  const { t } = useTranslation('train')
  const queryClient = useQueryClient()
  const refresh = () => void queryClient.invalidateQueries({ queryKey: ['training'] })
  const [revision, setRevision] = useState(view.revisions[0]?.revision ?? 0)
  const [prompts, setPrompts] = useState(view.test_prompts.map((p) => p.prompt).join('\n'))
  const savedPrompts = view.test_prompts.map((p) => p.prompt).join('\n')

  const savePrompts = useMutation({
    mutationFn: () => api.setTestPrompts(view.id, prompts.split('\n')),
    onSuccess: refresh,
  })
  const run = useMutation({
    mutationFn: async () => {
      if (prompts !== savedPrompts) await api.setTestPrompts(view.id, prompts.split('\n'))
      return api.evaluateRevision(view.id, revision)
    },
    onSuccess: refresh,
  })

  if (view.revisions.length === 0) {
    return <div className="card text-sm text-ink-muted">{t('test.first')}</div>
  }
  const runs = view.eval_runs.filter((r) => r.revision === revision)
  const latest = runs[0]
  const evaluated = view.revisions.find((r) => r.revision === revision)?.evaluated

  return (
    <div className="space-y-4">
      <div className="card space-y-3">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h3 className="section-title">{t('test.title')}</h3>
          <label className="flex items-center gap-2 text-sm">
            <span className="text-ink-muted">{t('test.revision')}</span>
            <select className="field" value={revision} onChange={(e) => setRevision(Number(e.target.value))}>
              {view.revisions.map((r) => (
                <option key={r.revision} value={r.revision}>
                  {r.revision}
                  {r.revision === view.deployed_revision ? t('test.deployedTag') : ''}
                </option>
              ))}
            </select>
          </label>
        </div>
        <p className="text-sm text-ink-muted">{t('test.description')}</p>
        <label className="block space-y-1">
          <span className="text-sm font-medium text-ink">{t('test.prompts')}</span>
          <textarea className="field min-h-28 w-full text-sm" value={prompts} onChange={(e) => setPrompts(e.target.value)} />
        </label>
        {(savePrompts.error || run.error) && <p className="text-sm text-danger">{errorText(savePrompts.error ?? run.error)}</p>}
        <div className="flex flex-wrap gap-2">
          <button
            type="button"
            className="btn-primary"
            disabled={!prompts.trim() || run.isPending || latest?.status === 'running'}
            onClick={() => run.mutate()}
          >
            {latest?.status === 'running' ? t('test.comparing') : runs.length ? t('test.again') : t('test.run')}
          </button>
          {prompts !== savedPrompts && (
            <button type="button" className="btn-secondary" disabled={savePrompts.isPending} onClick={() => savePrompts.mutate()}>
              {t('test.save')}
            </button>
          )}
        </div>
      </div>

      {latest && (
        <div className="card space-y-3">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="section-title">{t('test.results')}</h3>
            <span className="text-xs text-ink-faint">{formatDateTime(latest.created_at)}</span>
            {latest.status === 'running' && <span className="status-chip bg-info/15 text-info">{t('test.running')}</span>}
            {latest.status === 'failed' && <span className="status-chip bg-danger/15 text-danger">{t('test.failed')}</span>}
          </div>
          {latest.error && <p className="text-sm text-danger">{latest.error}</p>}
          <ul className="space-y-3">
            {(latest.results ?? []).map((r, i) => (
              <li key={i} className="space-y-2">
                <p className="text-sm font-medium text-ink">{r.prompt}</p>
                {r.error ? (
                  <p className="text-sm text-danger">{r.error}</p>
                ) : (
                  <div className="grid gap-2 md:grid-cols-2">
                    <div className="rounded-lg bg-raised p-3">
                      <p className="label-caps mb-1">{t('test.base')}</p>
                      <p className="whitespace-pre-wrap text-sm text-ink-muted">{r.base}</p>
                    </div>
                    <div className="rounded-lg bg-primary-soft/60 p-3">
                      <p className="label-caps mb-1 text-primary">{t('test.specialized')}</p>
                      <p className="whitespace-pre-wrap text-sm text-ink">{r.specialized}</p>
                    </div>
                  </div>
                )}
              </li>
            ))}
          </ul>
        </div>
      )}
      {evaluated && (
        <button type="button" className="btn-primary" onClick={onNext}>
          {t('test.continue')}
        </button>
      )}
    </div>
  )
}
