import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { Trans, useTranslation } from 'react-i18next'
import { Link, useSearchParams } from 'react-router-dom'
import i18n from '@/i18n'
import { api } from '@/lib/api'
import type { ToolRecord } from '@/types/api'
import { RealmKicker } from '@/components/ui/Realm'
import { ImageSetupCard, VideoSetupCard } from './ImageSetup'
import { ConnectedServices } from './ConnectedServices'
import { ToolSources } from './ToolSources'
import { formatDateTime } from '@/i18n/format'
import { LoadError } from '@/components/ui/LoadError'
import { Skeleton } from '@/components/ui/Skeleton'
import { HowItWorks, stepIcons } from '@/components/ui/HowItWorks'
import { rovingKeyDown } from '@/lib/roving'

// The page's sections, as tabs; each is tools:tabs.<id>. ?tab=add opens Add more.
const TABS = ['yours', 'add', 'media'] as const
type Tab = (typeof TABS)[number]

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
  const [searchParams, setSearchParams] = useSearchParams()
  const asked = searchParams.get('tab')
  const tab: Tab = TABS.includes(asked as Tab) ? (asked as Tab) : 'yours'
  const setTab = (next: Tab) => setSearchParams(next === 'yours' ? {} : { tab: next }, { replace: true })
  const [showIntro, setShowIntro] = useState(false)

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
    <div className="page-fill gap-5 overflow-y-auto p-4 sm:p-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="min-w-0 max-w-2xl">
          <RealmKicker />
          <h1 className="page-title">{t('page.title')}</h1>
          <p className="page-subtitle mt-1">{t('page.description')}</p>
        </div>
        <button
          type="button"
          className="btn-secondary"
          aria-expanded={showIntro}
          aria-controls="tools-intro"
          onClick={() => setShowIntro((open) => !open)}
        >
          {showIntro ? t('page.hideHowItWorks') : t('page.howItWorks')}
        </button>
      </div>
      {showIntro ? (
        <div className="space-y-2">
          <HowItWorks
            id="tools-intro"
            title={t('intro.title')}
            tone="bg-gungnir/15 text-gungnir"
            steps={[
              { icon: stepIcons.spark, title: t('intro.steps.builtin.title'), body: t('intro.steps.builtin.body') },
              { icon: stepIcons.plug, title: t('intro.steps.add.title'), body: t('intro.steps.add.body') },
              { icon: stepIcons.person, title: t('intro.steps.choose.title'), body: t('intro.steps.choose.body') },
            ]}
          />
          <p className="px-1 text-sm text-ink-muted">
            <Trans
              t={t}
              i18nKey="intro.profiles"
              values={{ page: t('nav.profiles', { ns: 'common' }) }}
              components={{ go: <Link to="/profiles" className="font-medium text-primary-active underline-offset-2 hover:underline" /> }}
            />
          </p>
        </div>
      ) : null}

      {/* Its own size: in this scrolling column it would otherwise shrink to
          nothing on a phone and stretch across the page on a desktop. */}
      <div className="segmented shrink-0 self-start" role="tablist" aria-label={t('tabs.label')} onKeyDown={rovingKeyDown}>
        {TABS.map((id) => (
          <button
            key={id}
            id={`tools-tab-${id}`}
            type="button"
            role="tab"
            aria-selected={tab === id}
            aria-controls={`tools-panel-${id}`}
            tabIndex={tab === id ? 0 : -1}
            className="segmented-item"
            onClick={() => setTab(id)}
          >
            {t(`tabs.${id}`)}
          </button>
        ))}
      </div>

      {tab === 'add' && (
        <div id="tools-panel-add" role="tabpanel" aria-labelledby="tools-tab-add" className="space-y-5">
          <ConnectedServices />
          <ToolSources />
        </div>
      )}

      {tab === 'media' && (
        <div id="tools-panel-media" role="tabpanel" aria-labelledby="tools-tab-media" className="space-y-5">
          <ImageSetupCard />
          <VideoSetupCard />
        </div>
      )}

      {tab === 'yours' && (
      <div id="tools-panel-yours" role="tabpanel" aria-labelledby="tools-tab-yours" className="space-y-4">
      <p className="max-w-2xl text-sm text-ink-muted">{t('page.allDescription')}</p>
      <div className="flex flex-wrap items-center gap-2">
        <label className="search-field min-w-[16rem] flex-1 sm:max-w-sm">
          <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth={1.5} strokeLinecap="round" aria-hidden>
            <circle cx="7" cy="7" r="4.5" />
            <path d="m10.5 10.5 3 3" />
          </svg>
          <input
            type="search"
            className="field"
            value={query}
            aria-label={t('page.search')}
            placeholder={t('page.search')}
            onChange={(event) => setQuery(event.target.value)}
          />
        </label>
        <div className="segmented" role="group" aria-label={t('page.allTitle')}>
          {FILTERS.map((item) => (
            <button key={item} type="button" className="segmented-item" aria-pressed={filter === item} onClick={() => setFilter(item)}>
              {t(`page.filters.${item}`)}
            </button>
          ))}
        </div>
      </div>
      <div className={selected ? 'grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(18rem,24rem)]' : ''}>
        <ul className={['grid gap-2 sm:grid-cols-2', selected ? '' : 'xl:grid-cols-3'].join(' ')}>
          {toolsQuery.isLoading && (
            <li className="sm:col-span-2 xl:col-span-3">
              <Skeleton label={t('page.loading')} count={4} />
            </li>
          )}
          {toolsQuery.isError && !toolsQuery.data && (
            <li className="sm:col-span-2 xl:col-span-3">
              <LoadError error={toolsQuery.error} onRetry={() => void toolsQuery.refetch()} retrying={toolsQuery.isFetching} />
            </li>
          )}
          {visible.map((tool) => (
            <li key={tool.id}>
              <button
                type="button"
                className={['selectable flex h-full w-full flex-col', selectedId === tool.id ? 'selectable-active' : ''].join(' ')}
                aria-pressed={selectedId === tool.id}
                onClick={() => {
                  setSelectedId(selectedId === tool.id ? null : tool.id)
                  setTestOutput('')
                  setTestArgs(exampleArgs(tool))
                }}
              >
                <span className="flex w-full items-start justify-between gap-2">
                  <span className="selectable-title text-sm font-semibold text-ink">{tool.name}</span>
                  <span className={['status-chip shrink-0', tool.enabled ? 'bg-success/15 text-success' : 'bg-raised text-ink-muted'].join(' ')}>
                    {tool.enabled ? t('page.enabled') : t('page.disabled')}
                  </span>
                </span>
                <span className="mt-1 line-clamp-2 text-xs text-ink-muted">{tool.description}</span>
                <span className="mt-auto pt-2 text-[11px] text-ink-faint">
                  {tool.capability} · {sourceLabel(tool.source)}
                </span>
              </button>
            </li>
          ))}
        </ul>
        {selected && (
          <aside className="card h-fit space-y-3 lg:sticky lg:top-0">
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
              className="btn-secondary btn-sm"
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
                  aria-label={t('page.test')}
                  value={testArgs}
                  onChange={(event) => setTestArgs(event.target.value)}
                />
                <button
                  type="button"
                  className="btn-primary btn-sm"
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
      )}
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
