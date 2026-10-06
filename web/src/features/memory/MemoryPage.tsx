import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { Trans, useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { HowItWorks, stepIcons } from '@/components/ui/HowItWorks'
import { Toggle } from '@/components/ui/Toggle'
import { api } from '@/lib/api'
import { rovingKeyDown, useMenu } from '@/lib/roving'
import { subscribeEvents } from '@/lib/events'
import { errorText } from '@/features/train/display'
import type { MemoryCategory, MemoryItem } from '@/types/api'
import { RealmKicker } from '@/components/ui/Realm'
import { formatDate } from '@/i18n/format'
import { nameInItself } from '@/i18n/answerLanguages'
import { LoadError } from '@/components/ui/LoadError'
import { Skeleton } from '@/components/ui/Skeleton'

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
  const empty = !memory.isLoading && !memory.isError && items.length === 0
  const [showIntro, setShowIntro] = useState(false)
  // Text an example puts in the Add box, for the person to adjust and add.
  const [example, setExample] = useState<{ text: string } | null>(null)

  return (
    <div className="page-fill gap-5 overflow-y-auto p-4 sm:p-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="min-w-0 max-w-2xl">
          <RealmKicker />
          <h1 className="page-title">{t('page.title')}</h1>
          <p className="page-subtitle mt-1">{t('page.description')}</p>
        </div>
        <div className="flex flex-wrap items-center gap-3">
          {items.length > 0 ? (
            <button
              type="button"
              className="btn-secondary"
              aria-expanded={showIntro}
              aria-controls="memory-intro"
              onClick={() => setShowIntro((open) => !open)}
            >
              {showIntro ? t('page.hideHowItWorks') : t('page.howItWorks')}
            </button>
          ) : null}
          <div className="flex items-center gap-2.5" title={t('page.useInChatsHint')}>
            <Toggle checked={on} disabled={toggleAll.isPending || !settings.data} label={t('page.useInChats')} onChange={() => toggleAll.mutate(!on)} />
            <span className="text-sm font-medium text-ink" aria-hidden>
              {t('page.useInChats')}
            </span>
          </div>
        </div>
      </div>
      {!on && (
        <p role="status" className="rounded-lg bg-warning/10 p-3 text-sm text-warning">
          {t('page.off')}
        </p>
      )}
      {empty || showIntro ? (
        <div className="space-y-2">
          <HowItWorks
            id="memory-intro"
            title={t('intro.title')}
            tone="bg-muninn/15 text-muninn"
            mascot={empty}
            steps={[
              { icon: stepIcons.chat, title: t('intro.steps.tell.title'), body: t('intro.steps.tell.body') },
              { icon: stepIcons.spark, title: t('intro.steps.fit.title'), body: t('intro.steps.fit.body') },
              { icon: stepIcons.person, title: t('intro.steps.control.title'), body: t('intro.steps.control.body') },
            ]}
          />
          <p className="px-1 text-sm text-ink-muted">
            <Trans
              t={t}
              i18nKey="intro.knowledge"
              components={{ go: <Link to="/knowledge" className="font-medium text-primary-active underline-offset-2 hover:underline" /> }}
            />
          </p>
        </div>
      ) : null}
      <AddMemory
        categories={categories}
        example={example}
        showExamples={empty || showIntro}
        onExample={(text) => setExample({ text })}
        onAdded={refresh}
      />
      {memory.isLoading && <Skeleton label={t('page.loading')} />}
      {memory.isError && !memory.data && (
        <LoadError error={memory.error} onRetry={() => void memory.refetch()} retrying={memory.isFetching} />
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

const EXAMPLES = ['metric', 'short', 'language', 'shop'] as const

function AddMemory({
  categories,
  example,
  showExamples,
  onExample,
  onAdded,
}: {
  categories: MemoryCategory[]
  /** Text from an example; a new object each time, so choosing one again refills the box. */
  example: { text: string } | null
  showExamples: boolean
  onExample: (text: string) => void
  onAdded: () => void
}) {
  const { t } = useTranslation('memory')
  const categoryLabel = useCategoryLabel()
  const [content, setContent] = useState('')
  const [category, setCategory] = useState<MemoryCategory | ''>('')
  const inputRef = useRef<HTMLInputElement>(null)
  useEffect(() => {
    if (!example) return
    setContent(example.text)
    inputRef.current?.focus()
  }, [example])
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
      className="card space-y-3 !p-4"
      onSubmit={(e) => {
        e.preventDefault()
        if (content.trim()) add.mutate()
      }}
    >
      <div className="flex flex-wrap items-end gap-2">
        <label className="min-w-[14rem] flex-1 space-y-1">
          <span className="text-sm text-ink">{t('add.label')}</span>
          <input ref={inputRef} className="field w-full" value={content} onChange={(e) => setContent(e.target.value)} placeholder={t('add.placeholder')} />
        </label>
        <select className="field" value={category} onChange={(e) => setCategory(e.target.value as MemoryCategory)} aria-label={t('add.category')}>
          <option value="">{t('add.chooseForMe')}</option>
          {categories.map((c) => (
            <option key={c} value={c}>
              {categoryLabel(c)}
            </option>
          ))}
        </select>
        <button type="submit" className="btn-primary" disabled={!content.trim() || add.isPending}>
          {t('add.button')}
        </button>
      </div>
      {showExamples ? (
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="text-xs text-ink-faint">{t('examples.title')}</span>
          {EXAMPLES.map((id) => (
            <button key={id} type="button" className="chat-suggestion" onClick={() => onExample(t(`examples.${id}`))}>
              {t(`examples.${id}`)}
            </button>
          ))}
        </div>
      ) : null}
      {add.error && <p className="text-sm text-danger">{errorText(add.error)}</p>}
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
  const [menuOpen, setMenuOpen] = useState(false)
  const menuRef = useMenu(menuOpen, () => setMenuOpen(false))
  const [draftCategory, setDraftCategory] = useState<MemoryCategory>(memory.category)
  useEffect(() => {
    if (!menuOpen) return
    const close = () => setMenuOpen(false)
    window.addEventListener('click', close)
    return () => window.removeEventListener('click', close)
  }, [menuOpen])
  return (
    <li className="card !p-3.5">
      {draft != null ? (
        <form
          className="flex flex-wrap items-center gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            update.mutate({ content: draft, category: draftCategory })
          }}
        >
          <input className="field min-w-[12rem] flex-1" value={draft} onChange={(e) => setDraft(e.target.value)} aria-label={t('row.text')} autoFocus />
          <select
            className="field"
            value={draftCategory}
            aria-label={t('row.category')}
            onChange={(e) => setDraftCategory(e.target.value as MemoryCategory)}
          >
            {categories.map((c) => (
              <option key={c} value={c}>
                {categoryLabel(c)}
              </option>
            ))}
          </select>
          <button type="submit" className="btn-primary btn-sm" disabled={update.isPending}>
            {t('row.save')}
          </button>
          <button type="button" className="btn-secondary btn-sm" onClick={() => setDraft(null)}>
            {t('row.cancel')}
          </button>
        </form>
      ) : (
        <div className="flex items-start gap-3">
          <div className={['min-w-0 flex-1', memory.enabled ? '' : 'opacity-60'].join(' ')}>
            <p dir="auto" className="text-[15px] text-ink">
              {memory.content}
            </p>
            <p className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-ink-faint">
              <span>
                {memory.source_type === 'explicit' ? t('row.fromChat') : t('row.addedHere')} · {formatDate(memory.updated_at)}
                {otherLanguage ? (
                  <span lang={memory.language} title={t('row.languageHint')}>
                    {' · '}
                    {otherLanguage}
                  </span>
                ) : null}
              </span>
              {!memory.enabled ? <span className="status-chip bg-raised text-ink-muted">{t('row.paused')}</span> : null}
              {memory.local_only ? (
                <span className="status-chip bg-raised text-ink-muted" title={t('row.localOnlyHint')}>
                  {t('row.localOnly')}
                </span>
              ) : null}
            </p>
          </div>
          <div className="flex shrink-0 items-center gap-1">
            <button
              type="button"
              className="btn-secondary btn-sm"
              onClick={() => {
                setDraftCategory(memory.category)
                setDraft(memory.content)
              }}
            >
              {t('row.edit')}
            </button>
            <div className="relative">
              <button
                type="button"
                className="icon-button"
                aria-label={t('row.more')}
                aria-haspopup="menu"
                aria-expanded={menuOpen}
                onClick={(e) => {
                  e.stopPropagation()
                  setMenuOpen((open) => !open)
                }}
              >
                <svg viewBox="0 0 16 16" className="h-4 w-4" fill="currentColor" aria-hidden>
                  <circle cx="3.5" cy="8" r="1.2" />
                  <circle cx="8" cy="8" r="1.2" />
                  <circle cx="12.5" cy="8" r="1.2" />
                </svg>
              </button>
              {menuOpen ? (
                <div className="menu min-w-[12rem]" ref={menuRef} role="menu" onKeyDown={rovingKeyDown} onClick={(e) => e.stopPropagation()}>
                  <button
                    type="button"
                    role="menuitem"
                    className="menu-item"
                    disabled={update.isPending}
                    onClick={() => {
                      setMenuOpen(false)
                      update.mutate({ enabled: !memory.enabled })
                    }}
                  >
                    {memory.enabled ? t('row.pause') : t('row.useAgain')}
                  </button>
                  <button
                    type="button"
                    role="menuitemcheckbox"
                    aria-checked={!!memory.local_only}
                    className="menu-item"
                    title={t('row.localOnlyHint')}
                    disabled={update.isPending}
                    onClick={() => {
                      setMenuOpen(false)
                      update.mutate({ local_only: !memory.local_only })
                    }}
                  >
                    {memory.local_only ? t('row.localOnlyOn') : t('row.localOnly')}
                  </button>
                  <button
                    type="button"
                    role="menuitem"
                    className="menu-item menu-item-danger"
                    disabled={remove.isPending}
                    aria-label={t('row.deleteLabel', { content: memory.content })}
                    onClick={() => {
                      setMenuOpen(false)
                      remove.mutate()
                    }}
                  >
                    {t('row.delete')}
                  </button>
                </div>
              ) : null}
            </div>
          </div>
        </div>
      )}
      {(update.error || remove.error) && <p className="mt-1 text-xs text-danger">{errorText(update.error ?? remove.error)}</p>}
    </li>
  )
}
