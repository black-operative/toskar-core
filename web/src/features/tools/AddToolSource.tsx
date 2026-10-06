import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState, type ReactNode } from 'react'
import { Trans, useTranslation } from 'react-i18next'
import { api } from '@/lib/api'
import type {
  MCPAddRequest,
  MCPAdded,
  MCPField,
  MCPGalleryEntry,
  MCPImportCandidate,
  MCPNeed,
  MCPParsed,
  MCPSpec,
} from '@/types/api'
import { preopenSignInWindow } from '@/lib/desktopBridge'
import { errorText, linesToMap, openSignIn, SOURCES_KEY, specSummary, splitArgs } from './mcpShared'
import { rovingKeyDown } from '@/lib/roving'

// The tabs, in order; each is tools:add.tabs.<id> in the catalog.
const TABS = ['gallery', 'paste', 'apps', 'custom'] as const

type TabId = (typeof TABS)[number]

/**
 * Adding a tool source, four ways: pick one from the gallery and answer a
 * question or two, paste whatever a server's instructions say, bring the
 * servers set up in other apps on this computer, or fill in the form.
 */
export function AddToolSource({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation('tools')
  const [tab, setTab] = useState<TabId>('gallery')
  const [done, setDone] = useState<MCPAdded | null>(null)
  return (
    <section className="card space-y-4" aria-label={t('add.title')}>
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div>
          <h2 className="section-title">{t('add.title')}</h2>
          <p className="mt-1 max-w-2xl text-sm text-ink-muted">{t('add.description')}</p>
        </div>
        <button type="button" className="btn-secondary px-3 py-1.5 text-xs" onClick={onClose}>
          {t('add.close')}
        </button>
      </div>
      {done ? (
        <AddedNotice added={done} onAnother={() => setDone(null)} onClose={onClose} />
      ) : (
        <>
          <div role="tablist" className="flex flex-wrap gap-1.5" onKeyDown={rovingKeyDown}>
            {TABS.map((id) => (
              <button
                key={id}
                type="button"
                role="tab"
                aria-selected={tab === id}
                tabIndex={tab === id ? 0 : -1}
                className={tab === id ? 'btn-primary px-3 py-1.5 text-xs' : 'btn-secondary px-3 py-1.5 text-xs'}
                onClick={() => setTab(id)}
              >
                {t(`add.tabs.${id}`)}
              </button>
            ))}
          </div>
          {tab === 'gallery' && <GalleryTab onAdded={setDone} />}
          {tab === 'paste' && <PasteTab onAdded={setDone} />}
          {tab === 'apps' && <AppsTab onAdded={setDone} />}
          {tab === 'custom' && <CustomTab onAdded={setDone} />}
        </>
      )}
    </section>
  )
}

/** useAdd adds a source and, when the service needs a sign-in, opens it. */
function useAdd(onAdded: (added: MCPAdded) => void) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async ({ body, popup }: { body: MCPAddRequest; popup?: Window | null }) => {
      try {
        const added = await api.addMCPServer(body)
        if (added.sign_in_url) openSignIn(added.sign_in_url, popup)
        else popup?.close()
        return added
      } catch (err) {
        popup?.close()
        throw err
      }
    },
    onSuccess: (added) => {
      void queryClient.invalidateQueries({ queryKey: SOURCES_KEY })
      void queryClient.invalidateQueries({ queryKey: ['tools'] })
      void queryClient.invalidateQueries({ queryKey: ['mcp-gallery'] })
      void queryClient.invalidateQueries({ queryKey: ['mcp-import'] })
      onAdded(added)
    },
  })
}

/** Work is the message shown while a source is being added. */
function Working({ local }: { local: boolean }) {
  const { t } = useTranslation('tools')
  return (
    <p className="rounded-md bg-info/10 px-2.5 py-2 text-xs leading-relaxed text-ink" role="status">
      {local ? t('add.startingLocal') : t('add.connecting')}
    </p>
  )
}

function AddedNotice({ added, onAnother, onClose }: { added: MCPAdded; onAnother: () => void; onClose: () => void }) {
  const { t } = useTranslation('tools')
  const s = added.server
  const reads = s.tools.filter((tool) => tool.risk === 'read').length
  const changes = s.tools.length - reads
  const name = { name: <span className="font-semibold" /> }
  return (
    <div className="space-y-3" role="status">
      {added.sign_in_url ? (
        <>
          <p className="text-sm text-ink">
            <Trans t={t} i18nKey="add.needsSignIn" values={{ name: s.name }} components={name} />
          </p>
          <a className="btn-primary px-3 py-1.5 text-xs" href={added.sign_in_url} target="_blank" rel="noreferrer">
            {t('add.openSignIn')}
          </a>
        </>
      ) : (
        <p className="text-sm text-ink">
          <Trans
            t={t}
            i18nKey={s.tools.length === 0 ? 'add.readyNoTools' : changes > 0 ? 'add.readyChanges' : 'add.readyReads'}
            values={{ name: s.name, count: s.tools.length, reads, changes }}
            components={name}
          />
        </p>
      )}
      <div className="flex gap-2">
        <button type="button" className="btn-secondary px-3 py-1.5 text-xs" onClick={onAnother}>
          {t('add.another')}
        </button>
        <button type="button" className="btn-secondary px-3 py-1.5 text-xs" onClick={onClose}>
          {t('add.done')}
        </button>
      </div>
    </div>
  )
}

function GalleryTab({ onAdded }: { onAdded: (a: MCPAdded) => void }) {
  const { t } = useTranslation('tools')
  const gallery = useQuery({ queryKey: ['mcp-gallery'], queryFn: () => api.mcpGallery(), retry: false })
  const [chosen, setChosen] = useState<MCPGalleryEntry | null>(null)
  const groups = useMemo(() => {
    const out = new Map<string, MCPGalleryEntry[]>()
    for (const e of gallery.data ?? []) {
      out.set(e.category, [...(out.get(e.category) ?? []), e])
    }
    return [...out.entries()]
  }, [gallery.data])

  if (chosen) return <GallerySetup entry={chosen} onBack={() => setChosen(null)} onAdded={onAdded} />
  if (gallery.isError) return <p className="text-sm text-danger">{errorText(gallery.error)}</p>
  return (
    <div className="space-y-4">
      {gallery.isLoading && <p className="text-sm text-ink-muted">{t('add.loadingGallery')}</p>}
      {groups.map(([category, entries]) => (
        <div key={category} className="space-y-2">
          <p className="label-caps">{category}</p>
          <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
            {entries.map((e) => (
              <button key={e.id} type="button" className="selectable space-y-1.5" onClick={() => setChosen(e)}>
                <span className="flex flex-wrap items-center gap-1.5">
                  <span className="selectable-title text-sm font-semibold text-ink">{e.name}</span>
                  {e.added && <span className="status-chip bg-success/15 text-success">{t('add.added')}</span>}
                  {e.sign_in && <span className="status-chip bg-raised text-ink-muted">{t('add.signIn')}</span>}
                  {e.missing && <span className="status-chip bg-warning/15 text-warning">{t('add.needs', { runtime: e.missing })}</span>}
                </span>
                <span className="block text-xs text-ink-muted">{e.description}</span>
                <span className="block text-[11px] text-ink-faint">{e.remote ? t('add.runsWeb') : t('add.runsLocal')}</span>
              </button>
            ))}
          </div>
        </div>
      ))}
    </div>
  )
}

function GallerySetup({ entry, onBack, onAdded }: { entry: MCPGalleryEntry; onBack: () => void; onAdded: (a: MCPAdded) => void }) {
  const { t } = useTranslation('tools')
  const [values, setValues] = useState<Record<string, string>>(() =>
    Object.fromEntries(entry.fields.map((f) => [f.key, f.default ?? ''])),
  )
  const add = useAdd(onAdded)
  return (
    <form
      className="space-y-3"
      onSubmit={(ev) => {
        ev.preventDefault()
        // Open the sign-in window now, while the click still counts, so the
        // browser does not block it.
        const popup = entry.sign_in ? preopenSignInWindow() : null
        add.mutate({ body: { preset: entry.id, values }, popup })
      }}
    >
      <div>
        <button type="button" className="text-xs text-ink-muted hover:text-ink" onClick={onBack}>
          <span className="inline-block rtl:-scale-x-100" aria-hidden>
            ←
          </span>{' '}
          {t('add.backToGallery')}
        </button>
        <h3 className="mt-1 font-display text-lg font-semibold text-ink">{entry.name}</h3>
        <p className="text-sm text-ink-muted">{entry.description}</p>
        {entry.homepage && (
          <a className="text-xs text-primary hover:underline" href={entry.homepage} target="_blank" rel="noreferrer">
            {t('add.about')}
          </a>
        )}
      </div>
      {entry.missing && <MissingRuntime name={entry.missing} />}
      {entry.setup && <p className="rounded-md bg-info/10 px-2.5 py-2 text-xs leading-relaxed text-ink">{entry.setup}</p>}
      {entry.sign_in && (
        <p className="text-xs text-ink-muted">{t('add.signInNote', { name: entry.name })}</p>
      )}
      {entry.fields.map((f) => (
        <FieldInput key={f.key} field={f} value={values[f.key] ?? ''} onChange={(v) => setValues((cur) => ({ ...cur, [f.key]: v }))} />
      ))}
      {add.isPending && <Working local={!entry.remote} />}
      {add.isError && <p className="text-xs text-danger">{errorText(add.error)}</p>}
      <button type="submit" className="btn-primary px-3 py-1.5 text-xs" disabled={add.isPending}>
        {add.isPending
          ? t('add.adding')
          : t(entry.sign_in ? 'add.signInAndAdd' : 'add.addName', { name: entry.name })}
      </button>
    </form>
  )
}

function FieldInput({ field, value, onChange }: { field: MCPField; value: string; onChange: (v: string) => void }) {
  const { t } = useTranslation('tools')
  const label = (
    <span className="text-ink-muted">
      {field.label}
      {field.optional && <span className="text-ink-faint">{t('add.optional')}</span>}
    </span>
  )
  return (
    <label className="block text-sm">
      {label}
      {field.kind === 'folders' ? (
        <textarea
          className="field mt-1 min-h-20 w-full font-mono text-xs"
          value={value}
          spellCheck={false}
          placeholder={field.placeholder}
          onChange={(e) => onChange(e.target.value)}
        />
      ) : (
        <input
          className="field mt-1 w-full"
          type={field.secret ? 'password' : 'text'}
          autoComplete="off"
          spellCheck={false}
          value={value}
          placeholder={field.placeholder}
          onChange={(e) => onChange(e.target.value)}
        />
      )}
      {field.help && <span className="mt-1 block text-xs text-ink-faint">{field.help}</span>}
    </label>
  )
}

const RUNTIME_HELP: Record<string, { key: string; href: string }> = {
  'Node.js': { key: 'add.installNode', href: 'https://nodejs.org' },
  uv: { key: 'add.installUv', href: 'https://docs.astral.sh/uv/' },
  Docker: { key: 'add.installDocker', href: 'https://www.docker.com/products/docker-desktop/' },
}

function MissingRuntime({ name }: { name: string }) {
  const { t } = useTranslation('tools')
  const help = RUNTIME_HELP[name]
  const how: ReactNode = help ? (
    <Trans
      t={t}
      i18nKey={help.key}
      components={{ go: <a className="underline" href={help.href} target="_blank" rel="noreferrer" />, code: <code /> }}
    />
  ) : (
    t('add.installGeneric', { runtime: name })
  )
  return (
    <p className="rounded-md bg-warning/10 px-2.5 py-2 text-xs leading-relaxed text-ink">
      {t('add.missing', { runtime: name })} {how}
    </p>
  )
}

function PasteTab({ onAdded }: { onAdded: (a: MCPAdded) => void }) {
  const { t } = useTranslation('tools')
  const [text, setText] = useState('')
  const parse = useMutation({ mutationFn: () => api.parseMCP(text) })
  return (
    <div className="space-y-3">
      <label className="block text-sm">
        <span className="text-ink-muted">{t('add.pasteLabel')}</span>
        <textarea
          className="field mt-1 min-h-32 w-full font-mono text-xs"
          value={text}
          spellCheck={false}
          // eslint-disable-next-line i18next/no-literal-string -- an example of what to type, not prose
          placeholder={'{\n  "mcpServers": {\n    "example": { "command": "npx", "args": ["-y", "example-mcp"] }\n  }\n}\n\nor  https://mcp.example.com/mcp\nor  npx -y example-mcp'}
          onChange={(e) => {
            setText(e.target.value)
            parse.reset()
          }}
        />
      </label>
      <button
        type="button"
        className="btn-primary px-3 py-1.5 text-xs"
        disabled={!text.trim() || parse.isPending}
        onClick={() => parse.mutate()}
      >
        {parse.isPending ? t('add.reading') : t('add.read')}
      </button>
      {parse.isError && <p className="text-xs text-danger">{errorText(parse.error)}</p>}
      {parse.data?.map((p, i) => <ParsedServer key={`${p.spec.name}-${i}`} parsed={p} onAdded={onAdded} />)}
    </div>
  )
}

/** One server read from pasted text, with what it still needs. */
function ParsedServer({ parsed, onAdded }: { parsed: MCPParsed; onAdded: (a: MCPAdded) => void }) {
  const { t } = useTranslation('tools')
  const [name, setName] = useState(parsed.spec.name)
  const [values, setValues] = useState<Record<string, string>>({})
  const add = useAdd(onAdded)
  const local = !parsed.spec.url
  const missingValues = parsed.needs.some((n) => !values[n.key]?.trim())
  return (
    <form
      className="space-y-2 rounded-lg border border-line/60 p-3"
      onSubmit={(e) => {
        e.preventDefault()
        add.mutate({ body: { spec: { ...parsed.spec, name }, values } })
      }}
    >
      <label className="block text-sm">
        <span className="text-ink-muted">{t('add.name')}</span>
        <input className="field mt-1 w-full" value={name} onChange={(e) => setName(e.target.value)} />
      </label>
      <p className="break-anywhere font-mono text-[11px] text-ink-faint">{specSummary(parsed.spec)}</p>
      {parsed.missing && <MissingRuntime name={parsed.missing} />}
      <NeedsInputs needs={parsed.needs} values={values} setValues={setValues} />
      {add.isPending && <Working local={local} />}
      {add.isError && <p className="text-xs text-danger">{errorText(add.error)}</p>}
      <button type="submit" className="btn-primary px-3 py-1.5 text-xs" disabled={add.isPending || missingValues || !name.trim()}>
        {add.isPending ? t('add.adding') : name ? t('add.addName', { name }) : t('add.addIt')}
      </button>
    </form>
  )
}

function NeedsInputs({
  needs,
  values,
  setValues,
}: {
  needs: MCPNeed[]
  values: Record<string, string>
  setValues: (fn: (cur: Record<string, string>) => Record<string, string>) => void
}) {
  const { t } = useTranslation('tools')
  if (needs.length === 0) return null
  return (
    <div className="space-y-2">
      <p className="text-xs text-ink-muted">{t('add.fillIn')}</p>
      {needs.map((n) => (
        <label key={n.key} className="block text-sm">
          <span className="font-mono text-xs text-ink-muted">{n.label}</span>
          <input
            className="field mt-1 w-full"
            type={n.secret ? 'password' : 'text'}
            autoComplete="off"
            spellCheck={false}
            value={values[n.key] ?? ''}
            onChange={(e) => setValues((cur) => ({ ...cur, [n.key]: e.target.value }))}
          />
        </label>
      ))}
      {needs.some((n) => n.secret) && (
        <p className="text-[11px] text-ink-faint">{t('add.secretsKept')}</p>
      )}
    </div>
  )
}

function AppsTab({ onAdded }: { onAdded: (a: MCPAdded) => void }) {
  const { t } = useTranslation('tools')
  const found = useQuery({ queryKey: ['mcp-import'], queryFn: () => api.mcpImportCandidates(), retry: false })
  const groups = useMemo(() => {
    const out = new Map<string, MCPImportCandidate[]>()
    for (const c of found.data ?? []) out.set(c.app_name, [...(out.get(c.app_name) ?? []), c])
    return [...out.entries()]
  }, [found.data])
  if (found.isLoading) return <p className="text-sm text-ink-muted">{t('add.lookingApps')}</p>
  if (found.isError) return <p className="text-sm text-danger">{errorText(found.error)}</p>
  if (groups.length === 0) {
    return (
      <p className="text-sm text-ink-muted">{t('add.noneFound')}</p>
    )
  }
  return (
    <div className="space-y-4">
      <p className="text-sm text-ink-muted">{t('add.appsIntro')}</p>
      {groups.map(([app, list]) => (
        <div key={app} className="space-y-2">
          <p className="label-caps">{app}</p>
          {list.map((c) => (
            <ImportRow key={`${c.app}-${c.spec.name}`} candidate={c} onAdded={onAdded} />
          ))}
        </div>
      ))}
    </div>
  )
}

function ImportRow({ candidate, onAdded }: { candidate: MCPImportCandidate; onAdded: (a: MCPAdded) => void }) {
  const { t } = useTranslation('tools')
  const [values, setValues] = useState<Record<string, string>>({})
  const add = useAdd(onAdded)
  const needs = candidate.needs ?? []
  return (
    <div className="space-y-2 rounded-lg border border-line/60 p-3">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="min-w-0">
          <p className="text-sm font-medium text-ink">{candidate.spec.name}</p>
          <p className="break-anywhere font-mono text-[11px] text-ink-faint">{specSummary(candidate.spec)}</p>
        </div>
        {candidate.added ? (
          <span className="status-chip bg-success/15 text-success">{t('add.added')}</span>
        ) : (
          <button
            type="button"
            className="btn-primary shrink-0 px-3 py-1.5 text-xs"
            disabled={add.isPending || needs.some((n) => !values[n.key]?.trim())}
            onClick={() =>
              add.mutate({ body: { import: { app: candidate.app, name: candidate.spec.name }, values } })
            }
          >
            {add.isPending ? t('add.adding') : t('add.add')}
          </button>
        )}
      </div>
      {!candidate.added && candidate.missing && <MissingRuntime name={candidate.missing} />}
      {!candidate.added && <NeedsInputs needs={needs} values={values} setValues={setValues} />}
      {add.isPending && <Working local={!candidate.spec.url} />}
      {add.isError && <p className="text-xs text-danger">{errorText(add.error)}</p>}
    </div>
  )
}

function CustomTab({ onAdded }: { onAdded: (a: MCPAdded) => void }) {
  const { t } = useTranslation('tools')
  const [where, setWhere] = useState<'local' | 'remote'>('local')
  const [name, setName] = useState('')
  const [command, setCommand] = useState('')
  const [env, setEnv] = useState('')
  const [url, setURL] = useState('')
  const [headers, setHeaders] = useState('')
  const [clientID, setClientID] = useState('')
  const add = useAdd(onAdded)
  const ready = name.trim() && (where === 'local' ? command.trim() : url.trim())
  return (
    <form
      className="space-y-3"
      onSubmit={(e) => {
        e.preventDefault()
        let spec: MCPSpec
        if (where === 'local') {
          const [cmd, ...args] = splitArgs(command)
          spec = { name: name.trim(), command: cmd, args, env: linesToMap(env, '=') }
        } else {
          spec = { name: name.trim(), url: url.trim(), headers: linesToMap(headers, ':'), client_id: clientID.trim() || undefined }
        }
        add.mutate({ body: { spec } })
      }}
    >
      <div className="flex gap-1.5" role="radiogroup" aria-label={t('add.where')} onKeyDown={rovingKeyDown}>
        {(['local', 'remote'] as const).map((w) => (
          <button
            key={w}
            type="button"
            role="radio"
            aria-checked={where === w}
            tabIndex={where === w ? 0 : -1}
            className={where === w ? 'btn-primary px-3 py-1.5 text-xs' : 'btn-secondary px-3 py-1.5 text-xs'}
            onClick={() => setWhere(w)}
          >
            {w === 'local' ? t('add.local') : t('add.remote')}
          </button>
        ))}
      </div>
      <label className="block text-sm">
        <span className="text-ink-muted">{t('add.name')}</span>
        <input className="field mt-1 w-full" value={name} placeholder={t('add.namePlaceholder')} onChange={(e) => setName(e.target.value)} />
      </label>
      {where === 'local' ? (
        <>
          <label className="block text-sm">
            <span className="text-ink-muted">{t('add.command')}</span>
            <input
              className="field mt-1 w-full font-mono text-xs"
              value={command}
              spellCheck={false}
              // eslint-disable-next-line i18next/no-literal-string -- an example of what to type, not prose
              placeholder="npx -y @scope/some-mcp-server --flag"
              onChange={(e) => setCommand(e.target.value)}
            />
          </label>
          <label className="block text-sm">
            <span className="text-ink-muted">
              {t('add.env')}
              <span className="text-ink-faint">{t('add.envHint')}</span>
            </span>
            <textarea
              className="field mt-1 min-h-16 w-full font-mono text-xs"
              value={env}
              spellCheck={false}
              placeholder="API_KEY=…"
              onChange={(e) => setEnv(e.target.value)}
            />
          </label>
        </>
      ) : (
        <>
          <label className="block text-sm">
            <span className="text-ink-muted">{t('add.address')}</span>
            <input
              className="field mt-1 w-full font-mono text-xs"
              value={url}
              spellCheck={false}
              placeholder="https://mcp.example.com/mcp"
              onChange={(e) => setURL(e.target.value)}
            />
          </label>
          <label className="block text-sm">
            <span className="text-ink-muted">
              {t('add.headers')}
              <span className="text-ink-faint">{t('add.headersHint')}</span>
            </span>
            <textarea
              className="field mt-1 min-h-16 w-full font-mono text-xs"
              value={headers}
              spellCheck={false}
              // eslint-disable-next-line i18next/no-literal-string -- an example of what to type, not prose
              placeholder="Authorization: Bearer …"
              onChange={(e) => setHeaders(e.target.value)}
            />
          </label>
          <label className="block text-sm">
            <span className="text-ink-muted">
              {t('add.clientId')}
              <span className="text-ink-faint">{t('add.clientIdHint')}</span>
            </span>
            <input className="field mt-1 w-full" value={clientID} onChange={(e) => setClientID(e.target.value)} />
          </label>
        </>
      )}
      <p className="text-[11px] text-ink-faint">{t('add.keptHere')}</p>
      {add.isPending && <Working local={where === 'local'} />}
      {add.isError && <p className="text-xs text-danger">{errorText(add.error)}</p>}
      <button type="submit" className="btn-primary px-3 py-1.5 text-xs" disabled={!ready || add.isPending}>
        {add.isPending ? t('add.adding') : t('add.add')}
      </button>
    </form>
  )
}
