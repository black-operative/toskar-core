import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { Toggle } from '@/components/ui/Toggle'
import { api } from '@/lib/api'
import type { MCPPrompt, MCPServer, MCPSpec, MCPUpdate } from '@/types/api'
import { AddToolSource } from './AddToolSource'
import { preopenSignInWindow } from '@/lib/desktopBridge'
import { errorText, openSignIn, quoteArg, SOURCES_KEY, specSummary, splitArgs, statusOf, toneClass } from './mcpShared'
import { formatDate } from '@/i18n/format'
import { LoadError } from '@/components/ui/LoadError'

/**
 * Tool sources: MCP servers the person added. Each card says in plain words
 * whether it is ready, and fixes are one click: sign in, check again, or
 * install what it needs.
 */
export function ToolSources() {
  const { t } = useTranslation('tools')
  const queryClient = useQueryClient()
  const [adding, setAdding] = useState(false)
  const sources = useQuery({
    queryKey: SOURCES_KEY,
    queryFn: () => api.listMCPServers(),
    retry: false,
    // While a sign-in is open in another window, notice when it finishes.
    refetchInterval: (q) => (q.state.data?.some((s) => s.status === 'sign_in') ? 3000 : false),
  })
  useEffect(() => {
    const onMessage = (e: MessageEvent) => {
      // The old name comes from a computer on a version before the rename (#237).
      const type = (e.data as { type?: string } | null)?.type
      if (type === 'toskar-mcp-sign-in' || type === 'yggdrasil-mcp-sign-in') {
        void queryClient.invalidateQueries({ queryKey: SOURCES_KEY })
        void queryClient.invalidateQueries({ queryKey: ['tools'] })
      }
    }
    window.addEventListener('message', onMessage)
    return () => window.removeEventListener('message', onMessage)
  }, [queryClient])

  const list = sources.data ?? []
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-end justify-between gap-2">
        <div>
          <h2 className="section-title">{t('sources.title')}</h2>
          <p className="mt-1 max-w-2xl text-sm text-ink-muted">{t('sources.description')}</p>
        </div>
        {!adding && (
          <button type="button" className="btn-primary px-3 py-1.5 text-xs" onClick={() => setAdding(true)}>
            {t('sources.add')}
          </button>
        )}
      </div>
      {adding && <AddToolSource onClose={() => setAdding(false)} />}
      {sources.isError && !sources.data && (
        <LoadError error={sources.error} onRetry={() => void sources.refetch()} retrying={sources.isFetching} />
      )}
      {!sources.isLoading && !sources.isError && list.length === 0 && !adding && (
        <button
          type="button"
          className="card-outline w-full text-start text-sm text-ink-muted hover:bg-raised/40"
          onClick={() => setAdding(true)}
        >
          {t('sources.empty')}
        </button>
      )}
      <ul className="space-y-2">
        {list.map((s) => (
          <li key={s.id}>
            <SourceCard source={s} />
          </li>
        ))}
      </ul>
    </div>
  )
}

function useSourceMutations(source: MCPServer) {
  const queryClient = useQueryClient()
  const [error, setError] = useState('')
  const refresh = () => {
    setError('')
    void queryClient.invalidateQueries({ queryKey: SOURCES_KEY })
    void queryClient.invalidateQueries({ queryKey: ['tools'] })
    void queryClient.invalidateQueries({ queryKey: ['mcp-gallery'] })
  }
  const onError = (err: unknown) => setError(errorText(err))
  return {
    error,
    update: useMutation({ mutationFn: (u: MCPUpdate) => api.updateMCPServer(source.id, u), onSuccess: refresh, onError }),
    check: useMutation({ mutationFn: () => api.checkMCPServer(source.id), onSuccess: refresh, onError }),
    remove: useMutation({ mutationFn: () => api.removeMCPServer(source.id), onSuccess: refresh, onError }),
    signIn: useMutation({
      mutationFn: async (popup: Window | null) => {
        try {
          openSignIn(await api.signInMCPServer(source.id), popup)
        } catch (err) {
          popup?.close()
          throw err
        }
      },
      onError,
    }),
    signOut: useMutation({ mutationFn: () => api.signOutMCPServer(source.id), onSuccess: refresh, onError }),
    toggleTool: useMutation({
      mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) => api.setToolEnabled(id, enabled),
      onSuccess: refresh,
      onError,
    }),
  }
}

function SourceCard({ source }: { source: MCPServer }) {
  const { t } = useTranslation('tools')
  const [open, setOpen] = useState(false)
  const m = useSourceMutations(source)
  const status = statusOf(source)
  const startSignIn = () => m.signIn.mutate(preopenSignInWindow())
  return (
    <div className="card space-y-3 p-4">
      <div className="flex items-start justify-between gap-3">
        <button type="button" className="min-w-0 flex-1 text-start" aria-expanded={open} onClick={() => setOpen((v) => !v)}>
          <span className="flex items-center gap-2">
            <span className={['h-2 w-2 shrink-0 rounded-full', toneClass[status.tone]].join(' ')} aria-hidden />
            <span className="truncate font-medium text-ink">{source.name}</span>
            <span className="text-[11px] text-ink-faint">{source.where === 'remote' ? t('sources.onWeb') : t('sources.onComputer')}</span>
          </span>
          {source.description && <span className="mt-0.5 block text-xs text-ink-muted">{source.description}</span>}
          <span className={['mt-1 block text-xs', status.tone === 'bad' ? 'text-danger' : 'text-ink-faint'].join(' ')}>
            {status.text}
          </span>
        </button>
        <Toggle
          checked={source.enabled}
          label={t(source.enabled ? 'sources.turnOff' : 'sources.turnOn', { name: source.name })}
          disabled={m.update.isPending}
          onChange={() => m.update.mutate({ enabled: !source.enabled })}
        />
      </div>

      {source.enabled && (source.status === 'sign_in' || source.status === 'error') && (
        <div className="flex flex-wrap gap-2">
          {source.status === 'sign_in' && (
            <button type="button" className="btn-primary px-3 py-1.5 text-xs" disabled={m.signIn.isPending} onClick={startSignIn}>
              {t('sources.signInTo', { name: source.name })}
            </button>
          )}
          <button type="button" className="btn-secondary px-3 py-1.5 text-xs" disabled={m.check.isPending} onClick={() => m.check.mutate()}>
            {m.check.isPending ? t('sources.checking') : t('sources.checkAgain')}
          </button>
          {source.status === 'error' && (
            <button type="button" className="btn-secondary px-3 py-1.5 text-xs" onClick={() => setOpen(true)}>
              {t('sources.seeLog')}
            </button>
          )}
        </div>
      )}
      {m.error && <p className="text-xs text-danger">{m.error}</p>}
      {open && <SourceDetails source={source} m={m} onSignIn={startSignIn} />}
    </div>
  )
}

type Mutations = ReturnType<typeof useSourceMutations>

function SourceDetails({ source, m, onSignIn }: { source: MCPServer; m: Mutations; onSignIn: () => void }) {
  const { t } = useTranslation('tools')
  const [editing, setEditing] = useState(false)
  const [confirmRemove, setConfirmRemove] = useState(false)
  const [keywords, setKeywords] = useState(source.keywords.join(', '))
  return (
    <div className="space-y-4 border-t border-line/50 pt-3">
      {source.tools.length > 0 && (
        <div className="space-y-1.5">
          <p className="label-caps">{t('sources.tools')}</p>
          <ul className="divide-y divide-line/40">
            {source.tools.map((tool) => (
              <li key={tool.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 py-1.5">
                <div className="min-w-0 flex-1">
                  <p className="text-sm text-ink">
                    {tool.name}{' '}
                    <span className={['status-chip', tool.risk === 'read' ? 'bg-raised text-ink-muted' : 'bg-warning/15 text-warning'].join(' ')}>
                      {tool.risk === 'read' ? t('sources.reads') : t('sources.changes')}
                    </span>
                  </p>
                  {tool.description && <p className="line-clamp-2 text-xs text-ink-faint">{tool.description}</p>}
                </div>
                {tool.remote_name && (
                  <label className="flex items-center gap-1.5 text-xs text-ink-muted">
                    <input
                      type="checkbox"
                      checked={tool.policy === 'ask'}
                      disabled={m.update.isPending}
                      onChange={(e) =>
                        m.update.mutate({ policies: { [tool.remote_name]: e.target.checked ? 'ask' : 'allow' } })
                      }
                    />
                    {t('sources.askFirst')}
                  </label>
                )}
                <Toggle
                  checked={tool.enabled}
                  label={t(tool.enabled ? 'sources.turnOff' : 'sources.turnOn', { name: tool.name })}
                  disabled={m.toggleTool.isPending}
                  onChange={() => m.toggleTool.mutate({ id: tool.id, enabled: !tool.enabled })}
                />
              </li>
            ))}
          </ul>
        </div>
      )}

      <div className="space-y-2">
        <p className="label-caps">{t('sources.whenUsed')}</p>
        <SettingRow
          title={t('sources.alwaysOffer')}
          help={t('sources.alwaysOfferHelp')}
          checked={source.always_offer}
          disabled={m.update.isPending}
          onChange={() => m.update.mutate({ always_offer: !source.always_offer })}
        />
        <SettingRow
          title={t('sources.sampling')}
          help={t('sources.samplingHelp')}
          checked={source.allow_sampling}
          disabled={m.update.isPending}
          onChange={() => m.update.mutate({ allow_sampling: !source.allow_sampling })}
        />
        <form
          className="flex flex-wrap items-end gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            m.update.mutate({ keywords: keywords.split(',').map((k) => k.trim()).filter(Boolean) })
          }}
        >
          <label className="block min-w-[14rem] flex-1 text-sm">
            <span className="text-ink-muted">{t('sources.keywords')}</span>
            <input className="field mt-1 w-full" value={keywords} placeholder={t('sources.keywordsPlaceholder')} onChange={(e) => setKeywords(e.target.value)} />
          </label>
          <button type="submit" className="btn-secondary px-3 py-1.5 text-xs" disabled={m.update.isPending}>
            {t('sources.save')}
          </button>
        </form>
      </div>

      {source.has_prompts && <Prompts source={source} />}
      <SourceLog id={source.id} />

      <div className="space-y-2">
        <p className="label-caps">{t('sources.connection')}</p>
        <p className="break-anywhere font-mono text-[11px] text-ink-muted">{specSummary(source)}</p>
        {(source.env.length > 0 || source.headers.length > 0) && (
          <ul className="font-mono text-[11px] text-ink-faint">
            {[...source.env, ...source.headers].map((v) => (
              <li key={v.key}>
                {v.key} = {v.value || t('sources.blank')}
              </li>
            ))}
          </ul>
        )}
        {source.server_name && (
          <p className="text-[11px] text-ink-faint">
            {source.server_name} {source.server_version} · MCP {source.protocol}
          </p>
        )}
        <div className="flex flex-wrap gap-2">
          <button type="button" className="btn-secondary px-3 py-1.5 text-xs" disabled={m.check.isPending} onClick={() => m.check.mutate()}>
            {m.check.isPending ? t('sources.checking') : t('sources.check')}
          </button>
          <button type="button" className="btn-secondary px-3 py-1.5 text-xs" onClick={() => setEditing((v) => !v)}>
            {editing ? t('sources.cancelEdit') : t('sources.edit')}
          </button>
          {source.where === 'remote' &&
            (source.signed_in ? (
              <button type="button" className="btn-secondary px-3 py-1.5 text-xs" disabled={m.signOut.isPending} onClick={() => m.signOut.mutate()}>
                {t('sources.signOut')}
              </button>
            ) : (
              <button type="button" className="btn-secondary px-3 py-1.5 text-xs" disabled={m.signIn.isPending} onClick={onSignIn}>
                {t('sources.signIn')}
              </button>
            ))}
          {confirmRemove ? (
            <>
              <button type="button" className="btn-danger px-3 py-1.5 text-xs" disabled={m.remove.isPending} onClick={() => m.remove.mutate()}>
                {t('sources.removeAll', { name: source.name })}
              </button>
              <button type="button" className="btn-secondary px-3 py-1.5 text-xs" onClick={() => setConfirmRemove(false)}>
                {t('sources.keep')}
              </button>
            </>
          ) : (
            <button type="button" className="btn-danger px-3 py-1.5 text-xs" onClick={() => setConfirmRemove(true)}>
              {t('sources.remove')}
            </button>
          )}
        </div>
        {editing && <EditSource source={source} onDone={() => setEditing(false)} />}
      </div>
    </div>
  )
}

function SettingRow({
  title,
  help,
  checked,
  disabled,
  onChange,
}: {
  title: string
  help: string
  checked: boolean
  disabled?: boolean
  onChange: () => void
}) {
  return (
    <div className="flex items-start justify-between gap-3">
      <div>
        <p className="text-sm text-ink">{title}</p>
        <p className="text-xs text-ink-faint">{help}</p>
      </div>
      <Toggle checked={checked} label={title} disabled={disabled} onChange={onChange} />
    </div>
  )
}

/** A source's ready-made prompts, which start a chat. */
function Prompts({ source }: { source: MCPServer }) {
  const { t } = useTranslation('tools')
  const navigate = useNavigate()
  const prompts = useQuery({ queryKey: ['mcp-prompts', source.id], queryFn: () => api.mcpServerPrompts(source.id), retry: false })
  const [chosen, setChosen] = useState<MCPPrompt | null>(null)
  const [args, setArgs] = useState<Record<string, string>>({})
  const use = useMutation({
    mutationFn: (p: MCPPrompt) => api.getMCPPrompt(source.id, p.name, args),
    onSuccess: (text) => navigate('/chat', { state: { draft: text } }),
  })
  if (!prompts.data?.length) return null
  return (
    <div className="space-y-2">
      <p className="label-caps">{t('sources.prompts')}</p>
      <div className="flex flex-wrap gap-1.5">
        {prompts.data.map((p) => (
          <button
            key={p.name}
            type="button"
            className={chosen?.name === p.name ? 'btn-primary px-3 py-1.5 text-xs' : 'btn-secondary px-3 py-1.5 text-xs'}
            title={p.description}
            onClick={() => {
              setChosen(p)
              setArgs({})
              if (!p.arguments?.length) use.mutate(p)
            }}
          >
            {p.title || p.name}
          </button>
        ))}
      </div>
      {chosen && (chosen.arguments?.length ?? 0) > 0 && (
        <form
          className="space-y-2"
          onSubmit={(e) => {
            e.preventDefault()
            use.mutate(chosen)
          }}
        >
          {chosen.arguments?.map((a) => (
            <label key={a.name} className="block text-sm">
              <span className="text-ink-muted">
                {a.description || a.name}
                {!a.required && <span className="text-ink-faint">{t('sources.optional')}</span>}
              </span>
              <input className="field mt-1 w-full" value={args[a.name] ?? ''} onChange={(e) => setArgs((c) => ({ ...c, [a.name]: e.target.value }))} />
            </label>
          ))}
          <button
            type="submit"
            className="btn-primary px-3 py-1.5 text-xs"
            disabled={use.isPending || (chosen.arguments ?? []).some((a) => a.required && !args[a.name]?.trim())}
          >
            {t('sources.startChat')}
          </button>
        </form>
      )}
      {use.isError && <p className="text-xs text-danger">{errorText(use.error)}</p>}
    </div>
  )
}

function SourceLog({ id }: { id: string }) {
  const { t } = useTranslation('tools')
  const [open, setOpen] = useState(false)
  const logs = useQuery({ queryKey: ['mcp-logs', id], queryFn: () => api.mcpServerLogs(id), enabled: open, refetchInterval: open ? 4000 : false })
  return (
    <div className="space-y-1.5">
      <button type="button" className="label-caps hover:text-ink" aria-expanded={open} onClick={() => setOpen((v) => !v)}>
        {open ? '▾' : '▸'} {t('sources.log')}
      </button>
      {open && (
        <pre className="log-panel max-h-56 text-[11px]">
          {(logs.data ?? []).length === 0
            ? t('sources.logEmpty')
            : (logs.data ?? []).map((l) => `${formatDate(l.at, { timeStyle: 'medium' })}  ${l.level.padEnd(6)} ${l.text}`).join('\n')}
        </pre>
      )}
    </div>
  )
}

/** Changing a source's command, address, or values. Secrets left blank are kept. */
function EditSource({ source, onDone }: { source: MCPServer; onDone: () => void }) {
  const { t } = useTranslation('tools')
  const queryClient = useQueryClient()
  const [name, setName] = useState(source.name)
  // Secret arguments show masked and keep their place; left as they are,
  // the stored values are kept.
  const [command, setCommand] = useState([source.command ?? '', ...(source.args ?? [])].map(quoteArg).join(' ').trim())
  const [url, setURL] = useState(source.url ?? '')
  const [values, setValues] = useState<Record<string, string>>(() =>
    Object.fromEntries(source.env.filter((v) => !v.secret).map((v) => [v.key, v.value])),
  )
  const save = useMutation({
    mutationFn: () => {
      const env = Object.fromEntries(source.env.map((v) => [v.key, values[v.key] ?? '']))
      const headers = Object.fromEntries(source.headers.map((v) => [v.key, values[`header:${v.key}`] ?? '']))
      let spec: MCPSpec
      if (source.where === 'remote') {
        spec = { name, url, headers, keywords: source.keywords }
      } else {
        const [cmd, ...args] = splitArgs(command)
        spec = { name, command: cmd, args: args.map((a) => (a.startsWith('••••') ? '' : a)), env, keywords: source.keywords }
      }
      return api.replaceMCPServer(source.id, spec)
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: SOURCES_KEY })
      void queryClient.invalidateQueries({ queryKey: ['tools'] })
      onDone()
    },
  })
  return (
    <form
      className="space-y-2 rounded-lg border border-line/60 p-3"
      onSubmit={(e) => {
        e.preventDefault()
        save.mutate()
      }}
    >
      <label className="block text-sm">
        <span className="text-ink-muted">{t('sources.name')}</span>
        <input className="field mt-1 w-full" value={name} onChange={(e) => setName(e.target.value)} />
      </label>
      {source.where === 'remote' ? (
        <label className="block text-sm">
          <span className="text-ink-muted">{t('sources.address')}</span>
          <input className="field mt-1 w-full font-mono text-xs" value={url} onChange={(e) => setURL(e.target.value)} />
        </label>
      ) : (
        <label className="block text-sm">
          <span className="text-ink-muted">{t('sources.command')}</span>
          <input className="field mt-1 w-full font-mono text-xs" value={command} spellCheck={false} onChange={(e) => setCommand(e.target.value)} />
        </label>
      )}
      {source.env.map((v) => (
        <label key={v.key} className="block text-sm">
          <span className="font-mono text-xs text-ink-muted">{v.key}</span>
          <input
            className="field mt-1 w-full"
            type={v.secret ? 'password' : 'text'}
            autoComplete="off"
            placeholder={v.secret ? t('sources.stored', { value: v.value }) : undefined}
            value={values[v.key] ?? ''}
            onChange={(e) => setValues((c) => ({ ...c, [v.key]: e.target.value }))}
          />
        </label>
      ))}
      {source.headers.map((v) => (
        <label key={v.key} className="block text-sm">
          <span className="font-mono text-xs text-ink-muted">{v.key}</span>
          <input
            className="field mt-1 w-full"
            type="password"
            autoComplete="off"
            placeholder={t('sources.stored', { value: v.value })}
            value={values[`header:${v.key}`] ?? ''}
            onChange={(e) => setValues((c) => ({ ...c, [`header:${v.key}`]: e.target.value }))}
          />
        </label>
      ))}
      {save.isError && <p className="text-xs text-danger">{errorText(save.error)}</p>}
      <button type="submit" className="btn-primary px-3 py-1.5 text-xs" disabled={save.isPending}>
        {save.isPending ? t('sources.checking') : t('sources.saveAndCheck')}
      </button>
    </form>
  )
}
