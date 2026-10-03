import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import i18n from '@/i18n'
import { api } from '@/lib/api'
import type { ToolRecord } from '@/types/api'
import { RealmKicker } from '@/components/ui/Realm'
import { ImageSetupCard, VideoSetupCard } from './ImageSetup'
import { ToolSources } from './ToolSources'
import { formatDateTime } from '@/i18n/format'
import { LoadError } from '@/components/ui/LoadError'

// The filters, in order; each is tools:page.filters.<id> in the catalog.
const FILTERS = ['all', 'builtin', 'added', 'disabled'] as const

/** Test arguments for a tool: its required fields, left for you to fill. */
function exampleArgs(tool: ToolRecord): string {
  if (tool.id === 'internet.search') return '{"query":"Juneau AK weather"}'
  try {
    const schema = JSON.parse(tool.schema) as Record<string, string>
    const out: Record<string, string> = {}
    for (const key of Object.keys(schema)) if (!key.endsWith('?')) out[key] = ''
    return JSON.stringify(out)
  } catch {
    return '{}'
  }
}

/** Where a tool comes from, in words. */
function sourceLabel(source: string): string {
  if (source === 'builtin') return i18n.t('tools:source.builtin')
  const [kind, id] = source.split(':')
  if (kind === 'mcp') return i18n.t('tools:source.mcp', { id })
  if (kind === 'connector') return i18n.t('tools:source.connector', { id })
  return source
}

export function ToolsPage() {
  const { t } = useTranslation('tools')
  const queryClient = useQueryClient()
  const [query, setQuery] = useState('')
  const [filter, setFilter] = useState<(typeof FILTERS)[number]>('all')
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [testArgs, setTestArgs] = useState('{"query":"Juneau AK weather"}')
  const [testOutput, setTestOutput] = useState('')

  const toolsQuery = useQuery({
    queryKey: ['tools'],
    queryFn: () => api.listTools(),
  })
  const tools = toolsQuery.data ?? []
  const selected = tools.find((tool) => tool.id === selectedId) ?? null

  const visible = useMemo(() => {
    const needle = query.trim().toLowerCase()
    return tools.filter((tool) => {
      if (filter === 'builtin' && tool.source !== 'builtin') return false
      if (filter === 'added' && tool.source === 'builtin') return false
      if (filter === 'disabled' && tool.enabled) return false
      if (!needle) return true
      return [tool.name, tool.description, tool.capability, tool.source, tool.id]
        .join(' ')
        .toLowerCase()
        .includes(needle)
    })
  }, [tools, query, filter])

  const toggle = useMutation({
    mutationFn: (tool: ToolRecord) => api.setToolEnabled(tool.id, !tool.enabled),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['tools'] }),
  })

  const test = useMutation({
    mutationFn: () => {
      const args = JSON.parse(testArgs) as Record<string, unknown>
      return api.testTool(selected?.id || '', args)
    },
    onSuccess: (result) => setTestOutput(JSON.stringify(result, null, 2)),
    onError: (error) => setTestOutput(error instanceof Error ? error.message : t('page.testFailed')),
  })

  return (
    <div className="page-fill gap-4 overflow-y-auto p-4">
      <div>
        <RealmKicker />
        <h1 className="font-display text-2xl font-semibold text-ink">{t('page.title')}</h1>
        <p className="mt-1 max-w-2xl text-sm text-ink-muted">{t('page.description')}</p>
      </div>
      <ImageSetupCard />
      <VideoSetupCard />
      <ToolSources />
      <div>
        <h2 className="section-title">{t('page.allTitle')}</h2>
        <p className="mt-1 max-w-2xl text-sm text-ink-muted">{t('page.allDescription')}</p>
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <input
          className="field min-w-[16rem] flex-1"
          value={query}
          placeholder={t('page.search')}
          onChange={(event) => setQuery(event.target.value)}
        />
        {FILTERS.map((item) => (
          <button
            key={item}
            type="button"
            className={filter === item ? 'btn-primary px-3 py-1.5 text-xs' : 'btn-secondary px-3 py-1.5 text-xs'}
            onClick={() => setFilter(item)}
          >
            {t(`page.filters.${item}`)}
          </button>
        ))}
      </div>
      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(18rem,24rem)]">
        <ul className="space-y-2">
          {toolsQuery.isLoading && <li className="text-sm text-ink-muted">{t('page.loading')}</li>}
          {toolsQuery.isError && !toolsQuery.data && (
            <li>
              <LoadError error={toolsQuery.error} onRetry={() => void toolsQuery.refetch()} retrying={toolsQuery.isFetching} />
            </li>
          )}
          {visible.map((tool) => (
            <li key={tool.id}>
              <button
                type="button"
                className="card w-full text-start"
                onClick={() => {
                  setSelectedId(tool.id)
                  setTestOutput('')
                  setTestArgs(exampleArgs(tool))
                }}
              >
                <div className="flex items-start justify-between gap-3">
                  <div>
                    <p className="font-medium text-ink">{tool.name}</p>
                    <p className="mt-1 text-xs text-ink-muted">{tool.description}</p>
                  </div>
                  <span className={tool.enabled ? 'text-xs text-success' : 'text-xs text-ink-faint'}>
                    {tool.enabled ? t('page.enabled') : t('page.disabled')}
                  </span>
                </div>
                <p className="mt-2 text-xs text-ink-faint">
                  {tool.capability} · {sourceLabel(tool.source)}
                </p>
              </button>
            </li>
          ))}
        </ul>
        {selected && (
          <aside className="card h-fit space-y-3">
            <h2 className="font-display text-lg font-semibold text-ink">{selected.name}</h2>
            <p className="text-sm text-ink-muted">{selected.description}</p>
            <dl className="space-y-1 text-xs text-ink-muted">
              <Row label={t('details.source')} value={sourceLabel(selected.source)} />
              <Row label={t('details.capability')} value={selected.capability} />
              <Row label={t('details.permission')} value={selected.default_policy} />
              {selected.level && <Row label={t('details.level')} value={`${selected.level} · ${selected.level_name ?? ''}`} />}
              {selected.outputs && <Row label={t('details.returns')} value={selected.outputs.join(', ')} />}
              {selected.timeout_seconds ? (
                <Row label={t('details.timeLimit')} value={t('details.seconds', { count: selected.timeout_seconds })} />
              ) : null}
              {selected.requirements && <Row label={t('details.needs')} value={needs(selected) || t('details.nothingElse')} />}
              {selected.health && <Row label={t('details.health')} value={t(`details.healthStates.${selected.health}`)} />}
              <Row label={t('details.profiles')} value={selected.profiles.join(', ') || t('details.none')} />
            </dl>
            <pre className="log-panel text-xs">{selected.schema}</pre>
            <button
              type="button"
              className="btn-secondary px-3 py-1.5 text-xs"
              disabled={toggle.isPending}
              onClick={() => toggle.mutate(selected)}
            >
              {selected.enabled ? t('page.disable') : t('page.enable')}
            </button>
            <RecentCalls toolId={selected.id} />
            {selected.risk === 'read' && (
              <div className="space-y-2">
                <textarea
                  className="field min-h-20 w-full font-mono text-xs"
                  value={testArgs}
                  onChange={(event) => setTestArgs(event.target.value)}
                />
                <button
                  type="button"
                  className="btn-primary px-3 py-1.5 text-xs"
                  disabled={test.isPending}
                  onClick={() => test.mutate()}
                >
                  {t('page.test')}
                </button>
                {testOutput && <pre className="log-panel max-h-64 text-xs">{testOutput}</pre>}
              </div>
            )}
          </aside>
        )}
      </div>
    </div>
  )
}

/** What a tool needs besides itself, in words. */
function needs(tool: ToolRecord): string {
  const r = tool.requirements
  if (!r) return ''
  return [
    r.network && i18n.t('tools:details.requirements.network'),
    r.filesystem && i18n.t('tools:details.requirements.filesystem'),
    r.credentials && i18n.t('tools:details.requirements.credentials'),
    r.runtime,
    r.gpu && i18n.t('tools:details.requirements.gpu'),
  ]
    .filter(Boolean)
    .join(', ')
}

/** The tool's latest audited calls (Gungnir §13). */
function RecentCalls({ toolId }: { toolId: string }) {
  const { t } = useTranslation('tools')
  const runs = useQuery({ queryKey: ['tool-runs', toolId], queryFn: () => api.listToolRuns(toolId), retry: false })
  const list = runs.data ?? []
  return (
    <div className="space-y-1">
      <p className="text-xs font-medium text-ink-muted">{t('calls.title')}</p>
      {list.length === 0 ? (
        <p className="text-xs text-ink-faint">{t('calls.none')}</p>
      ) : (
        <ul className="space-y-1 text-xs">
          {list.map((run) => (
            <li key={run.id} className="flex justify-between gap-2">
              <span className="min-w-0 truncate text-ink-muted" title={run.error || run.summary}>
                {formatDateTime(run.at)} · {t(`calls.status.${run.status}`)}
                {run.approval === 'you' ? t('calls.youApproved') : run.approval === 'session' ? t('calls.sessionAllowed') : ''}
                {run.summary ? ` · ${run.summary}` : ''}
              </span>
              {run.duration_ms > 0 && <span className="shrink-0 text-ink-faint">{t('calls.ms', { count: run.duration_ms })}</span>}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex justify-between gap-3">
      <dt>{label}</dt>
      <dd className="text-end text-ink">{value}</dd>
    </div>
  )
}
