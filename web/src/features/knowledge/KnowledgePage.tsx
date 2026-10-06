import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { HowItWorks, stepIcons } from '@/components/ui/HowItWorks'
import { api } from '@/lib/api'
import { isBinaryUpload, readUpload, UPLOAD_ACCEPT } from '@/lib/upload'
import type { AIProfile, KnowledgeSource } from '@/types/api'
import { errorText } from '@/features/train/display'
import { RealmKicker } from '@/components/ui/Realm'
import { crossLanguageNote, meaningNote, sourceLanguages } from './semantic'
import { refreshNote, sourceBadge, sourceWhere } from './remote'
import { RemoteSourceForm } from './RemoteSourceForm'
import { formatDateTime } from '@/i18n/format'
import { LoadError } from '@/components/ui/LoadError'
import { Skeleton } from '@/components/ui/Skeleton'


type Mode = 'path' | 'text' | 'database' | 'api'

// What people connect, and the form each one uses.
const EXAMPLES: { id: 'catalog' | 'policies' | 'notes' | 'database' | 'api'; mode: Mode }[] = [
  { id: 'catalog', mode: 'path' },
  { id: 'policies', mode: 'path' },
  { id: 'notes', mode: 'path' },
  { id: 'database', mode: 'database' },
  { id: 'api', mode: 'api' },
]

export function KnowledgePage() {
  const { t } = useTranslation('knowledge')
  const queryClient = useQueryClient()
  const sources = useQuery({ queryKey: ['knowledge'], queryFn: () => api.listKnowledge() })
  const profilesQuery = useQuery({ queryKey: ['profiles'], queryFn: () => api.getProfiles() })
  const settingsQuery = useQuery({ queryKey: ['settings'], queryFn: () => api.getSettings(), retry: false })
  const refresh = () => void queryClient.invalidateQueries({ queryKey: ['knowledge'] })
  const list = sources.data ?? []
  const profiles = profilesQuery.data ?? []
  // The profile chats start with: the one Settings names, else General.
  const chatProfile =
    profiles.find((p) => p.id === settingsQuery.data?.default_profile_id) ??
    profiles.find((p) => p.id === 'general-assistant') ??
    profiles[0]
  const [mode, setMode] = useState<Mode>('path')
  const [showIntro, setShowIntro] = useState(false)
  const empty = !sources.isLoading && !sources.isError && list.length === 0

  // Knowledge reaches a chat only through a profile, so connecting a source
  // offers to add it to profiles, and a source no profile uses says so.
  const useWith = useMutation({
    mutationFn: async ({ sourceID, profileIDs }: { sourceID: string; profileIDs: string[] }) => {
      // Read the profiles fresh: saving replaces the whole profile, and a
      // copy from before an earlier save would undo it.
      const current = (await api.getProfiles()) ?? []
      for (const id of profileIDs) {
        const profile = current.find((p) => p.id === id)
        if (!profile || profile.knowledge_sources?.includes(sourceID)) continue
        await api.updateProfile(id, { ...profile, knowledge_sources: [...(profile.knowledge_sources ?? []), sourceID] })
      }
    },
    onSettled: () => void queryClient.invalidateQueries({ queryKey: ['profiles'] }),
  })

  function chooseExample(next: Mode) {
    setMode(next)
    // After the form switches, start at its first field; on a phone this
    // also brings the form into view.
    requestAnimationFrame(() => {
      document.querySelector<HTMLElement>('#knowledge-connect :is(input:not([type=file]), textarea, select)')?.focus()
    })
  }

  const steps = [
    { icon: stepIcons.plug, title: t('intro.steps.connect.title'), body: t('intro.steps.connect.body') },
    {
      icon: stepIcons.person,
      title: t('intro.steps.use.title'),
      body: t('intro.steps.use.body', { profile: chatProfile?.name ?? 'General' }),
    },
    { icon: stepIcons.chat, title: t('intro.steps.ask.title'), body: t('intro.steps.ask.body') },
  ]

  return (
    <div className="page-fill gap-5 overflow-y-auto p-4 sm:p-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="min-w-0 max-w-2xl">
          <RealmKicker />
          <h1 className="page-title">{t('page.title')}</h1>
          <p className="page-subtitle mt-1">{t('page.description')}</p>
        </div>
        {list.length > 0 ? (
          <button
            type="button"
            className="btn-secondary"
            aria-expanded={showIntro}
            aria-controls="knowledge-intro"
            onClick={() => setShowIntro((open) => !open)}
          >
            {showIntro ? t('page.hideHowItWorks') : t('page.howItWorks')}
          </button>
        ) : null}
      </div>
      {(list.length > 0 && showIntro) || empty ? (
        <HowItWorks id="knowledge-intro" title={t('intro.title')} tone="bg-mimir/15 text-mimir" steps={steps} mascot={empty} />
      ) : null}
      <div className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_minmax(20rem,26rem)]">
        <section className="min-w-0 space-y-3">
          {sources.isLoading && <Skeleton label={t('page.loading')} />}
          {sources.isError && !sources.data && (
            <LoadError error={sources.error} onRetry={() => void sources.refetch()} retrying={sources.isFetching} />
          )}
          {empty || showIntro ? <Examples onChoose={chooseExample} /> : null}
          <ul className="space-y-2">
            {list.map((s) => (
              <SourceRow
                key={s.id}
                source={s}
                usedBy={profiles.filter((p) => p.knowledge_sources?.includes(s.id))}
                chatProfile={profilesQuery.isSuccess ? chatProfile : undefined}
                attaching={useWith.isPending}
                onUseWith={(profile) => useWith.mutate({ sourceID: s.id, profileIDs: [profile.id] })}
                onChanged={refresh}
              />
            ))}
          </ul>
          {list.length > 0 && <SearchBox />}
        </section>
        <aside>
          <AddSource
            mode={mode}
            onModeChange={setMode}
            profiles={profiles}
            chatProfile={chatProfile}
            onAdded={(source, profileIDs) => {
              refresh()
              if (source?.id && profileIDs.length > 0) useWith.mutate({ sourceID: source.id, profileIDs })
            }}
          />
        </aside>
      </div>
    </div>
  )
}

/** What people connect; choosing one opens that kind of source in the form. */
function Examples({ onChoose }: { onChoose: (mode: Mode) => void }) {
  const { t } = useTranslation('knowledge')
  return (
    <section className="space-y-3" aria-labelledby="knowledge-examples-title">
      <h2 id="knowledge-examples-title" className="section-title">
        {t('examples.title')}
      </h2>
      <ul className="grid gap-3 sm:grid-cols-2 2xl:grid-cols-3">
        {EXAMPLES.map(({ id, mode }) => (
          <li key={id}>
            <button type="button" className="selectable flex h-full w-full flex-col items-start gap-1" onClick={() => onChoose(mode)}>
              <span className="selectable-title text-sm font-semibold text-ink">{t(`examples.${id}.title`)}</span>
              <span className="text-sm text-ink-muted">{t(`examples.${id}.body`)}</span>
              <span className="mt-auto pt-2 text-xs font-semibold text-primary-active">
                {t('examples.connect')}{' '}
                <span className="inline-block rtl:-scale-x-100" aria-hidden>
                  →
                </span>
              </span>
            </button>
          </li>
        ))}
      </ul>
    </section>
  )
}

function SourceRow({
  source,
  usedBy,
  chatProfile,
  attaching,
  onUseWith,
  onChanged,
}: {
  source: KnowledgeSource
  usedBy: AIProfile[]
  /** The profile to offer when none uses this source; unknown until profiles load. */
  chatProfile?: AIProfile
  attaching: boolean
  onUseWith: (profile: AIProfile) => void
  onChanged: () => void
}) {
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
    <li className={['card !p-4', usedBy.length === 0 && chatProfile ? 'ring-1 ring-warning/40' : ''].join(' ')}>
      <div className="flex flex-wrap items-start gap-3">
        <div className="min-w-[12rem] flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="font-semibold text-ink">{source.name}</h2>
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
          {usedBy.length > 0 ? (
            <p className="mt-2 flex items-center gap-1.5 text-xs text-ink-muted">
              <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-success" aria-hidden />
              {t('row.usedBy', { profiles: usedBy.map((p) => p.name).join(', ') })}
            </p>
          ) : chatProfile ? (
            <div className="mt-2 flex flex-wrap items-center gap-2">
              <p className="text-xs text-warning">{t('row.unused')}</p>
              <button type="button" className="btn-secondary btn-sm" disabled={attaching} onClick={() => onUseWith(chatProfile)}>
                {t('row.useWith', { profile: chatProfile.name })}
              </button>
            </div>
          ) : null}
        </div>
        <div className="flex shrink-0 gap-1.5">
          {source.kind === 'text' && draft == null && !isBinaryUpload(source.filename ?? '') && (
            <button type="button" className="btn-secondary btn-sm" disabled={open.isPending} onClick={() => open.mutate()}>
              {t('row.edit')}
            </button>
          )}
          <button type="button" className="btn-secondary btn-sm" disabled={reindex.isPending} onClick={() => reindex.mutate()}>
            {reindex.isPending ? t('row.indexing') : t('row.reindex')}
          </button>
          <button
            type="button"
            className="btn-secondary btn-sm"
            disabled={localOnly.isPending}
            aria-pressed={!!source.local_only}
            title={t('row.localOnlyHint')}
            onClick={() => localOnly.mutate()}
          >
            {source.local_only ? t('row.localOnlyOn') : t('row.localOnly')}
          </button>
          <button
            type="button"
            className="btn-secondary btn-sm"
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
            <button type="button" className="btn-primary btn-sm" disabled={save.isPending} onClick={() => save.mutate(draft)}>
              {save.isPending ? t('row.saving') : t('row.save')}
            </button>
            <button type="button" className="btn-secondary btn-sm" onClick={() => setDraft(null)}>
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

function AddSource({
  mode,
  onModeChange,
  profiles,
  chatProfile,
  onAdded,
}: {
  mode: Mode
  onModeChange: (mode: Mode) => void
  profiles: AIProfile[]
  chatProfile?: AIProfile
  onAdded: (source: KnowledgeSource | null | undefined, profileIDs: string[]) => void
}) {
  const { t } = useTranslation('knowledge')
  // Profiles that will use the new source; the chat profile until changed.
  const [useWith, setUseWith] = useState<string[] | null>(null)
  const chosen = useWith ?? (chatProfile ? [chatProfile.id] : [])
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
    onSuccess: (source) => {
      setPath('')
      setName('')
      setFilename('')
      setText('')
      setBinary(null)
      onAdded(source, chosen)
    },
  })
  const profilePicker = (
    <UseWithPicker
      profiles={profiles}
      chosen={chosen}
      onChange={(ids) => setUseWith(ids)}
    />
  )
  return (
    <div id="knowledge-connect" className="card space-y-3">
      <h2 className="section-title">{t('add.title')}</h2>
      <div className="segmented grid grid-cols-2" role="group" aria-label={t('add.title')}>
        {(['path', 'text', 'database', 'api'] as const).map((m) => (
          <button key={m} type="button" className="segmented-item px-2 text-center" aria-pressed={mode === m} onClick={() => onModeChange(m)}>
            {t(`add.modes.${m}`)}
          </button>
        ))}
      </div>
      {mode === 'database' || mode === 'api' ? (
        <RemoteSourceForm key={mode} kind={mode} beforeConnect={profilePicker} onAdded={(source) => onAdded(source, chosen)} />
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
              <label className="btn-secondary btn-sm cursor-pointer">
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
          {profilePicker}
          {add.error && <p className="text-sm text-danger">{errorText(add.error)}</p>}
          {add.data?.status === 'failed' && <p className="text-sm text-danger">{add.data.error}</p>}
          <button
            type="button"
            className="btn-primary"
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

/** Which profiles a new source is added to, so their chats use it. */
function UseWithPicker({
  profiles,
  chosen,
  onChange,
}: {
  profiles: AIProfile[]
  chosen: string[]
  onChange: (ids: string[]) => void
}): ReactNode {
  const { t } = useTranslation('knowledge')
  if (profiles.length === 0) return null
  return (
    <fieldset className="space-y-1.5">
      <legend className="text-sm text-ink">{t('add.useWith')}</legend>
      <div className="flex flex-wrap gap-x-4 gap-y-1.5">
        {profiles.map((profile) => (
          <label key={profile.id} className="flex items-center gap-2 text-sm text-ink">
            <input
              type="checkbox"
              checked={chosen.includes(profile.id)}
              onChange={(e) => onChange(e.target.checked ? [...chosen, profile.id] : chosen.filter((id) => id !== profile.id))}
            />
            {profile.name}
          </label>
        ))}
      </div>
      <p className="text-xs text-ink-faint">{t('add.useWithHint')}</p>
    </fieldset>
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
        <button type="submit" className="btn-secondary" disabled={search.isPending}>
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
