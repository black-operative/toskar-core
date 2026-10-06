import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { Trans, useTranslation } from 'react-i18next'
import { api } from '@/lib/api'
import type { ExampleFlag, SpecializedAIView, TrainingExample, TrainingMessage } from '@/types/api'
import { errorText, flagCount, flagInfo, lastAnswer, lastUserTurn } from '../display'

const PAGE = 50

export function ExamplesStep({ view, onNext }: { view: SpecializedAIView; onNext: () => void }) {
  const { t } = useTranslation('train')
  const queryClient = useQueryClient()
  const [filter, setFilter] = useState<'all' | 'flagged' | 'excluded'>('all')
  const [shown, setShown] = useState(PAGE)
  const examples = useQuery({ queryKey: ['training', 'examples', view.id], queryFn: () => api.listExamples(view.id) })
  const refresh = () => void queryClient.invalidateQueries({ queryKey: ['training'] })

  const update = useMutation({
    mutationFn: (args: { id: string; messages?: TrainingMessage[]; excluded?: boolean }) =>
      api.updateExample(view.id, args.id, { messages: args.messages, excluded: args.excluded }),
    onSuccess: refresh,
  })
  const remove = useMutation({ mutationFn: (id: string) => api.deleteExample(view.id, id), onSuccess: refresh })

  const stats = examples.data?.stats ?? view.dataset
  const list = (examples.data?.examples ?? []).filter((ex) =>
    filter === 'flagged' ? (ex.flags?.length ?? 0) > 0 : filter === 'excluded' ? ex.excluded : true,
  )

  return (
    <div className="space-y-4">
      <div className="card space-y-3">
        <div className="flex flex-wrap items-baseline justify-between gap-2">
          <h3 className="section-title">{t('examples.title')}</h3>
          <p className="text-sm text-ink-muted">
            <Trans
              t={t}
              i18nKey="examples.willTrain"
              values={{ usable: stats.usable, total: stats.total }}
              components={{ strong: <span className="font-semibold tabular-nums text-ink" /> }}
            />
          </p>
        </div>
        {(stats.warnings ?? []).map((w) => (
          <p key={w} className="rounded-md bg-warning/10 p-2 text-sm text-warning">
            {w}
          </p>
        ))}
        {Object.keys(stats.flagged).length > 0 && (
          <div className="flex flex-wrap gap-1.5">
            {(Object.entries(stats.flagged) as [ExampleFlag, number][]).map(([flag, n]) => (
              <span key={flag} className={['status-chip', flagInfo(flag)?.blocking ? 'bg-danger/15 text-danger' : 'bg-warning/15 text-warning'].join(' ')} title={flagInfo(flag)?.help}>
                {flagCount(flag, n)}
              </span>
            ))}
          </div>
        )}
        <div className="flex gap-1.5">
          {(['all', 'flagged', 'excluded'] as const).map((f) => (
            <button key={f} type="button" className={filter === f ? 'btn-primary btn-sm' : 'btn-secondary btn-sm'} onClick={() => setFilter(f)}>
              {t(`examples.filters.${f}`)}
            </button>
          ))}
        </div>
        {examples.isLoading && <p className="text-sm text-ink-muted">{t('examples.loading')}</p>}
        <ul className="space-y-2">
          {list.slice(0, shown).map((ex) => (
            <ExampleRow
              key={ex.id}
              example={ex}
              busy={update.isPending || remove.isPending}
              onToggle={() => update.mutate({ id: ex.id, excluded: !ex.excluded })}
              onSave={(messages) => update.mutate({ id: ex.id, messages })}
              onDelete={() => remove.mutate(ex.id)}
            />
          ))}
        </ul>
        {list.length > shown && (
          <button type="button" className="btn-secondary btn-sm" onClick={() => setShown(shown + PAGE)}>
            {t('examples.showMore', { count: list.length - shown })}
          </button>
        )}
        {(update.error || remove.error) && <p className="text-sm text-danger">{errorText(update.error ?? remove.error)}</p>}
      </div>
      <AddExample aiID={view.id} onAdded={refresh} />
      <button type="button" className="btn-primary" disabled={stats.usable < 10} onClick={onNext}>
        {t('examples.continue')}
      </button>
    </div>
  )
}

function ExampleRow({
  example,
  busy,
  onToggle,
  onSave,
  onDelete,
}: {
  example: TrainingExample
  busy: boolean
  onToggle: () => void
  onSave: (messages: TrainingMessage[]) => void
  onDelete: () => void
}) {
  const { t } = useTranslation('train')
  const [editing, setEditing] = useState(false)
  const [answer, setAnswer] = useState(lastAnswer(example.messages))
  const turns = example.messages.filter((m) => m.role !== 'system').length
  return (
    <li className={['rounded-lg bg-raised p-3 text-sm', example.excluded ? 'opacity-60' : ''].join(' ')}>
      <div className="flex flex-wrap items-center gap-1.5">
        {(example.flags ?? []).map((f) => (
          <span key={f} className={['status-chip', flagInfo(f)?.blocking ? 'bg-danger/15 text-danger' : 'bg-warning/15 text-warning'].join(' ')} title={flagInfo(f)?.help}>
            {flagInfo(f)?.label ?? f}
          </span>
        ))}
        {example.excluded && <span className="status-chip bg-raised text-ink-muted">{t('examples.excluded')}</span>}
        {turns > 2 && <span className="text-xs text-ink-faint">{t('examples.turns', { count: turns })}</span>}
      </div>
      <p className="mt-1 text-ink">
        <span className="label-caps me-1">{t('examples.q')}</span>
        {lastUserTurn(example.messages) || <em className="text-ink-faint">{t('examples.empty')}</em>}
      </p>
      {editing ? (
        <textarea className="field mt-1 min-h-20 w-full text-sm" value={answer} onChange={(e) => setAnswer(e.target.value)} aria-label={t('examples.answer')} />
      ) : (
        <p className="mt-1 whitespace-pre-wrap text-ink-muted">
          <span className="label-caps me-1">{t('examples.a')}</span>
          {lastAnswer(example.messages) || <em className="text-ink-faint">{t('examples.noAnswer')}</em>}
        </p>
      )}
      <div className="mt-2 flex gap-1.5">
        {editing ? (
          <>
            <button
              type="button"
              className="btn-primary btn-sm"
              disabled={busy}
              onClick={() => {
                const msgs = [...example.messages]
                const i = msgs.map((m) => m.role).lastIndexOf('assistant')
                if (i >= 0) msgs[i] = { ...msgs[i], content: answer }
                else msgs.push({ role: 'assistant', content: answer })
                onSave(msgs)
                setEditing(false)
              }}
            >
              {t('examples.save')}
            </button>
            <button type="button" className="btn-secondary btn-sm" onClick={() => setEditing(false)}>
              {t('examples.cancel')}
            </button>
          </>
        ) : (
          <button type="button" className="btn-secondary btn-sm" onClick={() => setEditing(true)}>
            {t('examples.editAnswer')}
          </button>
        )}
        <button type="button" className="btn-secondary btn-sm" disabled={busy} onClick={onToggle}>
          {example.excluded ? t('examples.include') : t('examples.exclude')}
        </button>
        <button type="button" className="btn-secondary btn-sm" disabled={busy} onClick={onDelete}>
          {t('examples.delete')}
        </button>
      </div>
    </li>
  )
}

function AddExample({ aiID, onAdded }: { aiID: string; onAdded: () => void }) {
  const { t } = useTranslation('train')
  const [question, setQuestion] = useState('')
  const [answer, setAnswer] = useState('')
  const add = useMutation({
    mutationFn: () =>
      api.addExample(aiID, [
        { role: 'user', content: question },
        { role: 'assistant', content: answer },
      ]),
    onSuccess: () => {
      setQuestion('')
      setAnswer('')
      onAdded()
    },
  })
  return (
    <div className="card space-y-2">
      <h3 className="section-title">{t('examples.writeTitle')}</h3>
      <p className="text-sm text-ink-muted">{t('examples.writeDescription')}</p>
      <input className="field w-full" value={question} onChange={(e) => setQuestion(e.target.value)} placeholder={t('examples.questionPlaceholder')} aria-label={t('examples.question')} />
      <textarea className="field min-h-20 w-full" value={answer} onChange={(e) => setAnswer(e.target.value)} placeholder={t('examples.answerPlaceholder')} aria-label={t('examples.answer')} />
      {add.error && <p className="text-sm text-danger">{errorText(add.error)}</p>}
      <button type="button" className="btn-secondary" disabled={!question.trim() || !answer.trim() || add.isPending} onClick={() => add.mutate()}>
        {t('examples.add')}
      </button>
    </div>
  )
}
