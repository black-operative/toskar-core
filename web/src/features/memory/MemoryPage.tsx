import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { EmptyState } from '@/components/ui/EmptyState'
import { api } from '@/lib/api'
import { subscribeEvents } from '@/lib/events'
import { errorText } from '@/features/train/display'
import type { MemoryCategory, MemoryItem } from '@/types/api'
import { RealmKicker } from '@/components/ui/Realm'
import { formatDate } from '@/i18n/format'
import { nameInItself } from '@/i18n/answerLanguages'
import { LoadError } from '@/components/ui/LoadError'

// The categories, in order; their names are memory:categories.<category> in the catalog.
const CATEGORIES: MemoryCategory[] = ['identity', 'preferences', 'projects', 'technical', 'interests', 'people', 'other']

function useCategoryLabel(): (category: MemoryCategory) => string {
  const { t } = useTranslation('memory')
  return (category) => (CATEGORIES.includes(category) ? t(`categories.${category}`) : category)
}

export function MemoryPage() {
  const { t } = useTranslation('memory')
  const categoryLabel = useCategoryLabel()
  const queryClient = useQueryClient()
  const memory = useQuery({ queryKey: ['memory'], queryFn: () => api.listMemory() })
  const settings = useQuery({ queryKey: ['settings'], queryFn: () => api.getSettings() })
  const refresh = () => void queryClient.invalidateQueries({ queryKey: ['memory'] })

  useEffect(
    () =>
      subscribeEvents({
        onEvent: (e) => {
          if (e.type.startsWith('memory.')) refresh()
        },
      }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [],
  )

  const toggleAll = useMutation({
    mutationFn: (on: boolean) => api.updateSettings({ memory_enabled: on }),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['settings'] }),
  })

  const items = memory.data?.memories ?? []
  const categories = memory.data?.categories ?? CATEGORIES
  const on = settings.data?.memory_enabled !== false

  return (
    <div className="page-fill gap-4 overflow-y-auto p-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <RealmKicker />
          <h1 className="font-display text-2xl font-semibold text-ink">{t('page.title')}</h1>
          <p className="mt-1 max-w-2xl text-sm text-ink-muted">{t('page.description')}</p>
        </div>
        <label className="flex items-center gap-2 text-sm text-ink">
          <input type="checkbox" checked={on} disabled={toggleAll.isPending} onChange={(e) => toggleAll.mutate(e.target.checked)} />
          {t('page.useInChats')}
        </label>
      </div>
      {!on && (
        <p className="rounded-md bg-warning/10 p-3 text-sm text-warning">
          {t('page.off')}
        </p>
      )}
      <AddMemory categories={categories} onAdded={refresh} />
      {memory.isLoading && <p className="text-sm text-ink-muted">{t('page.loading')}</p>}
      {memory.isError && !memory.data && (
        <LoadError error={memory.error} onRetry={() => void memory.refetch()} retrying={memory.isFetching} />
      )}
      {!memory.isLoading && !memory.isError && items.length === 0 && (
        <EmptyState
          mascot="idle"
          title={t('page.emptyTitle')}
          description={t('page.emptyDescription')}
        />
      )}
      {categories.map((cat) => {
        const list = items.filter((m) => m.category === cat)
        if (list.length === 0) return null
        return (
          <section key={cat} className="space-y-2">
            <h2 className="label-caps">{categoryLabel(cat)}</h2>
            <ul className="space-y-2">
              {list.map((m) => (
                <MemoryRow key={m.id} memory={m} categories={categories} onChanged={refresh} />
              ))}
            </ul>
          </section>
        )
      })}
    </div>
  )
}

function AddMemory({ categories, onAdded }: { categories: MemoryCategory[]; onAdded: () => void }) {
  const { t } = useTranslation('memory')
  const categoryLabel = useCategoryLabel()
  const [content, setContent] = useState('')
  const [category, setCategory] = useState<MemoryCategory | ''>('')
  const add = useMutation({
    mutationFn: () => api.addMemory(content, category || undefined),
    onSuccess: () => {
      setContent('')
      setCategory('')
      onAdded()
    },
  })
  return (
    <form
      className="card flex flex-wrap items-end gap-2 !p-4"
      onSubmit={(e) => {
        e.preventDefault()
        if (content.trim()) add.mutate()
      }}
    >
      <label className="min-w-0 flex-1 space-y-1">
        <span className="text-sm text-ink">{t('add.label')}</span>
        <input className="field w-full" value={content} onChange={(e) => setContent(e.target.value)} placeholder={t('add.placeholder')} />
      </label>
      <select className="field" value={category} onChange={(e) => setCategory(e.target.value as MemoryCategory)} aria-label={t('add.category')}>
        <option value="">{t('add.chooseForMe')}</option>
        {categories.map((c) => (
          <option key={c} value={c}>
            {categoryLabel(c)}
          </option>
        ))}
      </select>
      <button type="submit" className="btn-primary px-3 py-1.5 text-sm" disabled={!content.trim() || add.isPending}>
        {t('add.button')}
      </button>
      {add.error && <p className="w-full text-sm text-danger">{errorText(add.error)}</p>}
    </form>
  )
}

function MemoryRow({ memory, categories, onChanged }: { memory: MemoryItem; categories: MemoryCategory[]; onChanged: () => void }) {
  const { t, i18n } = useTranslation('memory')
  // A memory in another language says which: it is still found in any language.
  const otherLanguage =
    memory.language && memory.language.split('-')[0] !== i18n.language.split('-')[0] ? nameInItself(memory.language) : ''
  const categoryLabel = useCategoryLabel()
  const [draft, setDraft] = useState<string | null>(null)
  const update = useMutation({
    mutationFn: (body: { content?: string; category?: MemoryCategory; enabled?: boolean; local_only?: boolean }) => api.updateMemory(memory.id, body),
    onSuccess: () => {
      setDraft(null)
      onChanged()
    },
  })
  const remove = useMutation({ mutationFn: () => api.deleteMemory(memory.id), onSuccess: onChanged })
  return (
    <li className={['card !p-3', memory.enabled ? '' : 'opacity-60'].join(' ')}>
      {draft != null ? (
        <form
          className="flex gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            update.mutate({ content: draft })
          }}
        >
          <input className="field flex-1" value={draft} onChange={(e) => setDraft(e.target.value)} aria-label={t('row.text')} autoFocus />
          <button type="submit" className="btn-primary px-3 py-1 text-xs" disabled={update.isPending}>
            {t('row.save')}
          </button>
          <button type="button" className="btn-secondary px-3 py-1 text-xs" onClick={() => setDraft(null)}>
            {t('row.cancel')}
          </button>
        </form>
      ) : (
        <div className="flex flex-wrap items-center gap-2">
          <p dir="auto" className="min-w-[12rem] flex-1 text-sm text-ink">{memory.content}</p>
          <span className="text-xs text-ink-faint">
            {memory.source_type === 'explicit' ? t('row.fromChat') : t('row.addedHere')} ·{' '}
            {formatDate(memory.updated_at)}
            {otherLanguage ? (
              <span lang={memory.language} title={t('row.languageHint')}>
                {' · '}
                {otherLanguage}
              </span>
            ) : null}
          </span>
          <select
            className="field py-0.5 text-xs"
            value={memory.category}
            aria-label={t('row.category')}
            onChange={(e) => update.mutate({ category: e.target.value as MemoryCategory })}
          >
            {categories.map((c) => (
              <option key={c} value={c}>
                {categoryLabel(c)}
              </option>
            ))}
          </select>
          <button type="button" className="btn-secondary px-2 py-0.5 text-xs" onClick={() => setDraft(memory.content)}>
            {t('row.edit')}
          </button>
          <button type="button" className="btn-secondary px-2 py-0.5 text-xs" disabled={update.isPending} onClick={() => update.mutate({ enabled: !memory.enabled })}>
            {memory.enabled ? t('row.pause') : t('row.useAgain')}
          </button>
          <button
            type="button"
            className="btn-secondary px-2 py-0.5 text-xs"
            disabled={update.isPending}
            aria-pressed={!!memory.local_only}
            title={t('row.localOnlyHint')}
            onClick={() => update.mutate({ local_only: !memory.local_only })}
          >
            {memory.local_only ? t('row.localOnlyOn') : t('row.localOnly')}
          </button>
          <button type="button" className="btn-secondary px-2 py-0.5 text-xs" disabled={remove.isPending} onClick={() => remove.mutate()} aria-label={t('row.deleteLabel', { content: memory.content })}>
            {t('row.delete')}
          </button>
        </div>
      )}
      {(update.error || remove.error) && <p className="mt-1 text-xs text-danger">{errorText(update.error ?? remove.error)}</p>}
    </li>
  )
}
