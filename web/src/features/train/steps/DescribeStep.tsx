import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '@/lib/api'
import type { SpecializedAIView } from '@/types/api'
import { errorText } from '../display'

export function DescribeStep({ view, onNext, onDeleted }: { view: SpecializedAIView; onNext: () => void; onDeleted: () => void }) {
  const { t } = useTranslation('train')
  const queryClient = useQueryClient()
  const [name, setName] = useState(view.name)
  const [goal, setGoal] = useState(view.goal)
  const [instructions, setInstructions] = useState(view.instructions)
  const dirty = name !== view.name || goal !== view.goal || instructions !== view.instructions

  const save = useMutation({
    mutationFn: () => api.updateAI(view.id, { name, goal, instructions }),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['training'] }),
  })
  const remove = useMutation({
    mutationFn: () => api.deleteAI(view.id),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['training'] })
      onDeleted()
    },
  })

  return (
    <div className="card space-y-4">
      <div>
        <h3 className="section-title">{t('describe.title')}</h3>
        <p className="mt-1 text-sm text-ink-muted">{t('describe.description')}</p>
      </div>
      <label className="block space-y-1">
        <span className="text-sm font-medium text-ink">{t('describe.name')}</span>
        <input className="field w-full" value={name} onChange={(e) => setName(e.target.value)} />
      </label>
      <label className="block space-y-1">
        <span className="text-sm font-medium text-ink">{t('describe.job')}</span>
        <textarea className="field min-h-20 w-full" value={goal} onChange={(e) => setGoal(e.target.value)} />
      </label>
      <label className="block space-y-1">
        <span className="text-sm font-medium text-ink">{t('describe.instructions')}</span>
        <textarea className="field min-h-32 w-full font-mono text-xs" value={instructions} onChange={(e) => setInstructions(e.target.value)} />
        <span className="block text-xs text-ink-faint">{t('describe.instructionsHint')}</span>
      </label>
      {(save.error || remove.error) && <p className="text-sm text-danger">{errorText(save.error ?? remove.error)}</p>}
      <div className="flex flex-wrap items-center gap-2">
        <button
          type="button"
          className="btn-primary"
          disabled={!name.trim() || save.isPending}
          onClick={() => (dirty ? save.mutate(undefined, { onSuccess: onNext }) : onNext())}
        >
          {dirty ? t('describe.saveContinue') : t('describe.continue')}
        </button>
        <button
          type="button"
          className="btn-danger ms-auto"
          disabled={remove.isPending}
          onClick={() => {
            if (window.confirm(t('describe.confirmDelete', { name: view.name }))) {
              remove.mutate()
            }
          }}
        >
          {t('describe.delete')}
        </button>
      </div>
    </div>
  )
}
