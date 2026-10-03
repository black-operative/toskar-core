import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { EmptyState } from '@/components/ui/EmptyState'
import { api } from '@/lib/api'
import { isBinaryUpload, readUpload, UPLOAD_ACCEPT } from '@/lib/upload'
import type { KnowledgeSource } from '@/types/api'
import { errorText } from '@/features/train/display'
import { RealmKicker } from '@/components/ui/Realm'
import { crossLanguageNote, meaningNote, sourceLanguages } from './semantic'
import { refreshNote, sourceBadge, sourceWhere } from './remote'
import { RemoteSourceForm } from './RemoteSourceForm'
import { formatDateTime } from '@/i18n/format'
import { LoadError } from '@/components/ui/LoadError'
import { Skeleton } from '@/components/ui/Skeleton'


export function KnowledgePage() {
  const { t } = useTranslation('knowledge')
  const queryClient = useQueryClient()
  const sources = useQuery({ queryKey: ['knowledge'], queryFn: () => api.listKnowledge() })
  const refresh = () => void queryClient.invalidateQueries({ queryKey: ['knowledge'] })
  const list = sources.data ?? []

  return (
    <div className="page-fill gap-4 overflow-y-auto p-4">
      <div>
        <RealmKicker />
        <h1 className="font-display text-2xl font-semibold text-ink">{t('page.title')}</h1>
        <p className="mt-1 max-w-2xl text-sm text-ink-muted">{t('page.description')}</p>
      </div>
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(20rem,26rem)]">
        <section className="space-y-3">
          {sources.isLoading && <Skeleton label={t('page.loading')} />}
          {sources.isError && !sources.data && (
            <LoadError error={sources.error} onRetry={() => void sources.refetch()} retrying={sources.isFetching} />
          )}
          {!sources.isLoading && !sources.isError && list.length === 0 && (
            <EmptyState
              mascot="idle"
              title={t('page.emptyTitle')}
              description={t('page.emptyDescription')}
            />
          )}
          <ul className="space-y-2">
            {list.map((s) => (
              <SourceRow key={s.id} source={s} onChanged={refresh} />
            ))}
          </ul>
          {list.length > 0 && <SearchBox />}
        </section>
        <aside>
          <AddSource onAdded={refresh} />
        </aside>
      </div>
    </div>
  )
}

function SourceRow({ source, onChanged }: { source: KnowledgeSource; onChanged: () => void }) {
  const { t, i18n } = useTranslation('knowledge')
  const models = useQuery({ queryKey: ['models'], queryFn: () => api.getModels(), staleTime: 60_000 })
  const languages = sourceLanguages(source)
  const crossNote = crossLanguageNote(source, i18n.language, models.data ?? [])
  const [draft, setDraft] = useState<string | null>(null)
  const open = useMutation({
    mutationFn: () => api.knowledgeContent(source.id),
    onSuccess: (res) => setDraft(res?.text ?? ''),
  })
  const save = useMutation({
    mutationFn: (text: string) => api.updateKnowledge(source.id, { text }),
    onSuccess: () => {
      setDraft(null)
      onChanged()
    },
  })
  const reindex = useMutation({ mutationFn: () => api.refreshKnowledge(source.id), onSuccess: onChanged })
  const localOnly = useMutation({
    mutationFn: () => api.updateKnowledge(source.id, { local_only: !source.local_only }),
    onSuccess: onChanged,
  })
  const remove = useMutation({ mutationFn: () => api.deleteKnowledge(source.id), onSuccess: onChanged })
  return (
    <li className="card !p-4">
      <div className="flex flex-wrap items-start gap-3">
        <div className="min-w-[12rem] flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-medium text-ink">{source.name}</span>
            <span className="badge-mimir">{sourceBadge(source)}</span>
            {source.status === 'failed' && <span className="status-chip bg-danger/15 text-danger">{t('row.failed')}</span>}
          </div>
          <p className="mt-0.5 break-anywhere text-xs text-ink-muted">
            {sourceWhere(source)} · {t('row.passages', { count: source.chunk_count })}
            {source.refreshed_at && ` · ${t('row.indexed', { when: formatDateTime(source.refreshed_at) })}`}
            {languages && ` · ${languages}`}
          </p>
          {source.kind === 'path' && (
            <p className="mt-0.5 text-xs text-ink-faint">{t('row.watches')}</p>
          )}
          {refreshNote(source) && <p className="mt-0.5 text-xs text-ink-faint">{refreshNote(source)}</p>}
          {meaningNote(source) && <p className="mt-0.5 text-xs text-ink-faint">{meaningNote(source)}</p>}
          {crossNote && <p className="mt-0.5 text-xs text-ink-faint">{crossNote}</p>}
          {source.error && <p className="mt-1 text-xs text-danger">{source.error}</p>}
        </div>
        <div className="flex shrink-0 gap-1.5">
          {source.kind === 'text' && draft == null && !isBinaryUpload(source.filename ?? '') && (
            <button type="button" className="btn-secondary px-2 py-1 text-xs" disabled={open.isPending} onClick={() => open.mutate()}>
              {t('row.edit')}
            </button>
          )}
          <button type="button" className="btn-secondary px-2 py-1 text-xs" disabled={reindex.isPending} onClick={() => reindex.mutate()}>
            {reindex.isPending ? t('row.indexing') : t('row.reindex')}
          </button>
          <button
            type="button"
            className="btn-secondary px-2 py-1 text-xs"
            disabled={localOnly.isPending}
            aria-pressed={!!source.local_only}
            title={t('row.localOnlyHint')}
            onClick={() => localOnly.mutate()}
          >
            {source.local_only ? t('row.localOnlyOn') : t('row.localOnly')}
          </button>
          <button
            type="button"
            className="btn-secondary px-2 py-1 text-xs"
            disabled={remove.isPending}
            onClick={() => {
              if (window.confirm(t('row.confirmDisconnect', { name: source.name }))) remove.mutate()
            }}
          >
            {t('row.disconnect')}
          </button>
        </div>
      </div>
      {draft != null && (
        <div className="mt-3 space-y-2">
          <textarea className="field min-h-48 w-full font-mono text-xs" value={draft} onChange={(e) => setDraft(e.target.value)} aria-label={t('row.editLabel', { name: source.name })} />
          <p className="text-xs text-ink-faint">{t('row.saveHint')}</p>
          <div className="flex gap-1.5">
            <button type="button" className="btn-primary px-3 py-1 text-xs" disabled={save.isPending} onClick={() => save.mutate(draft)}>
              {save.isPending ? t('row.saving') : t('row.save')}
            </button>
            <button type="button" className="btn-secondary px-3 py-1 text-xs" onClick={() => setDraft(null)}>
              {t('row.cancel')}
            </button>
          </div>
        </div>
      )}
      {(reindex.error || remove.error || open.error || save.error) && (
        <p className="mt-2 text-xs text-danger">{errorText(reindex.error ?? remove.error ?? open.error ?? save.error)}</p>
      )}
    </li>
  )
}

function AddSource({ onAdded }: { onAdded: () => void }) {
  const { t } = useTranslation('knowledge')
  const [mode, setMode] = useState<'path' | 'text' | 'database' | 'api'>('path')
  const [path, setPath] = useState('')
  const [name, setName] = useState('')
  const [filename, setFilename] = useState('')
  const [text, setText] = useState('')
  const [binary, setBinary] = useState<string | null>(null)
  const add = useMutation({
    mutationFn: () =>
      mode === 'path'
        ? api.createKnowledge({ kind: 'path', path, name: name || undefined })
        : api.createKnowledge({
            kind: 'text',
            filename: filename || 'pasted.txt',
            text,
            content_base64: binary ?? undefined,
            name: name || undefined,
          }),
    onSuccess: () => {
      setPath('')
      setName('')
      setFilename('')
      setText('')
      setBinary(null)
      onAdded()
    },
  })
  return (
    <div className="card space-y-3">
      <h2 className="section-title">{t('add.title')}</h2>
      <div className="flex flex-wrap gap-1.5">
        {(['path', 'text', 'database', 'api'] as const).map((m) => (
          <button key={m} type="button" className={mode === m ? 'btn-primary px-3 py-1 text-xs' : 'btn-secondary px-3 py-1 text-xs'} onClick={() => setMode(m)}>
            {t(`add.modes.${m}`)}
          </button>
        ))}
      </div>
      {mode === 'database' || mode === 'api' ? (
        <RemoteSourceForm key={mode} kind={mode} onAdded={onAdded} />
      ) : (
        <>
          {mode === 'path' ? (
            <label className="block space-y-1">
              <span className="text-sm text-ink">{t('add.path')}</span>
              <input className="field w-full font-mono text-xs" value={path} onChange={(e) => setPath(e.target.value)} placeholder="~/Documents/inventory.csv" />
              <span className="block text-xs text-ink-faint">{t('add.pathHint')}</span>
            </label>
          ) : (
            <>
              <label className="btn-secondary inline-block cursor-pointer px-3 py-1.5 text-xs">
                {t('add.chooseFile')}
                <input
                  type="file"
                  accept={UPLOAD_ACCEPT}
                  className="sr-only"
                  onChange={async (e) => {
                    const f = e.target.files?.[0]
                    if (!f) return
                    const upload = await readUpload(f)
                    setFilename(upload.filename)
                    setText(upload.text ?? '')
                    setBinary(upload.contentBase64 ?? null)
                  }}
                />
              </label>
              {filename && <span className="ms-2 text-xs text-ink-muted">{filename}</span>}
              <textarea className="field min-h-32 w-full font-mono text-xs" value={text} onChange={(e) => setText(e.target.value)} placeholder={t('add.pastePlaceholder')} aria-label={t('add.content')} />
            </>
          )}
          <label className="block space-y-1">
            <span className="text-sm text-ink">{t('add.name')}</span>
            <input className="field w-full" value={name} onChange={(e) => setName(e.target.value)} />
          </label>
          {add.error && <p className="text-sm text-danger">{errorText(add.error)}</p>}
          {add.data?.status === 'failed' && <p className="text-sm text-danger">{add.data.error}</p>}
          <button
            type="button"
            className="btn-primary px-3 py-1.5 text-sm"
            disabled={add.isPending || (mode === 'path' ? !path.trim() : !text.trim() && !binary)}
            onClick={() => add.mutate()}
          >
            {add.isPending ? t('add.indexing') : t('add.connect')}
          </button>
        </>
      )}
    </div>
  )
}

function SearchBox() {
  const { t } = useTranslation('knowledge')
  const [query, setQuery] = useState('')
  const search = useMutation({ mutationFn: () => api.searchKnowledge(query) })
  return (
    <div className="card space-y-3">
      <h2 className="section-title">{t('search.title')}</h2>
      <p className="text-sm text-ink-muted">{t('search.description')}</p>
      <form
        className="flex gap-2"
        onSubmit={(e) => {
          e.preventDefault()
          if (query.trim()) search.mutate()
        }}
      >
        <input className="field flex-1" value={query} onChange={(e) => setQuery(e.target.value)} placeholder={t('search.placeholder')} aria-label={t('search.question')} />
        <button type="submit" className="btn-secondary px-3 py-1.5 text-sm" disabled={search.isPending}>
          {t('search.search')}
        </button>
      </form>
      {search.data && search.data.length === 0 && <p className="text-sm text-ink-muted">{t('search.nothing')}</p>}
      <ul className="space-y-2">
        {(search.data ?? []).map((h, i) => (
          <li key={i} className="rounded-lg bg-raised p-3 text-sm">
            <p className="text-xs text-mimir">{h.title}</p>
            <p className="mt-1 whitespace-pre-wrap text-ink-muted">{h.body}</p>
          </li>
        ))}
      </ul>
    </div>
  )
}
