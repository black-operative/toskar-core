import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router-dom'
import i18n from '@/i18n'
import { EmptyState } from '@/components/ui/EmptyState'
import { api, ApiError } from '@/lib/api'
import { subscribeEvents } from '@/lib/events'
import { readScreenshotLaunch } from '@/lib/screenshotMode'
import type { Automation, AutomationDetail, AutomationInput, AutomationRun, Model } from '@/types/api'
import { AutomationForm } from './AutomationForm'
import { clockDetail, compactWhen, explainRun, runTiming } from './display'
import { notificationLabel, resultProse, scheduleLabel, visibleTask } from './parseRequest'
import { RealmKicker } from '@/components/ui/Realm'
import { LoadError } from '@/components/ui/LoadError'

const screenshotSentence =
  'Every morning at 8:00 AM, check this product and tell me if the price is below $500.'

export function AutomationsPage() {
  const { t } = useTranslation('automations')
  const queryClient = useQueryClient()
  const [searchParams, setSearchParams] = useSearchParams()
  const [selectedID, setSelectedID] = useState<string | null>(() => searchParams.get('id'))
  const [creating, setCreating] = useState(false)
  const [editing, setEditing] = useState(false)
  const [formError, setFormError] = useState('')
  const [query, setQuery] = useState('')
  const [filter, setFilter] = useState<'all' | 'active' | 'paused' | 'attention'>('all')

  const listQuery = useQuery({
    queryKey: ['automations'],
    queryFn: () => api.listAutomations(),
  })
  const profilesQuery = useQuery({
    queryKey: ['profiles'],
    queryFn: () => api.getProfiles(),
  })
  const toolsQuery = useQuery({
    queryKey: ['tools'],
    queryFn: () => api.listTools(),
  })
  const modelsQuery = useQuery({
    queryKey: ['models'],
    queryFn: () => api.getModels(),
  })
  const detailQuery = useQuery({
    queryKey: ['automation', selectedID],
    queryFn: () => api.getAutomation(selectedID ?? ''),
    enabled: Boolean(selectedID),
  })

  useEffect(() => {
    return subscribeEvents({
      onEvent: (event) => {
        if (!event.type.startsWith('automation.')) return
        void queryClient.invalidateQueries({ queryKey: ['automations'] })
        void queryClient.invalidateQueries({ queryKey: ['automation'] })
      },
    })
  }, [queryClient])

  const items = (listQuery.data ?? []).filter((item) => matchesAutomation(item, query, filter))
  const detail = detailQuery.data
  const profiles = profilesQuery.data ?? []
  const tools = toolsQuery.data ?? []
  const models = modelsQuery.data ?? []

  useEffect(() => {
    if (!readScreenshotLaunch()?.enabled) return
    const compose = new URLSearchParams(window.location.search).get('compose') === '1'
    if (compose) {
      setCreating(true)
      return
    }
    if (!selectedID && items[0]) setSelectedID(items[0].id)
  }, [items, selectedID])

  // A notification links here with ?id=…; open that automation.
  const linkedID = searchParams.get('id')
  useEffect(() => {
    if (!linkedID) return
    setSelectedID(linkedID)
    setCreating(false)
    setEditing(false)
    setSearchParams({}, { replace: true })
  }, [linkedID, setSearchParams])

  function refresh() {
    void queryClient.invalidateQueries({ queryKey: ['automations'] })
    void queryClient.invalidateQueries({ queryKey: ['automation'] })
  }

  const save = useMutation({
    mutationFn: async (input: AutomationInput) => {
      if (editing && selectedID) return api.updateAutomation(selectedID, input)
      return api.createAutomation(input)
    },
    onSuccess: (saved) => {
      setFormError('')
      setCreating(false)
      setEditing(false)
      if (saved?.id) setSelectedID(saved.id)
      refresh()
    },
    onError: (error) => setFormError(error instanceof ApiError ? error.message : t('page.saveFailed')),
  })

  const pause = useMutation({
    mutationFn: (item: Automation) => (item.enabled ? api.pauseAutomation(item.id) : api.resumeAutomation(item.id)),
    onSuccess: refresh,
  })
  const remove = useMutation({
    mutationFn: (id: string) => api.deleteAutomation(id),
    onSuccess: () => {
      setSelectedID(null)
      setEditing(false)
      refresh()
    },
  })
  const runNow = useMutation({
    mutationFn: (id: string) => api.runAutomation(id),
    onSuccess: refresh,
  })

  return (
    <div className="page-fill gap-4 overflow-y-auto p-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <RealmKicker />
          <h1 className="font-display text-2xl font-semibold text-ink">{t('page.title')}</h1>
          <p className="mt-1 max-w-2xl text-sm text-ink-muted">{t('page.description')}</p>
        </div>
        <button
          type="button"
          className="btn-primary px-3 py-1.5 text-xs"
          onClick={() => {
            setCreating(true)
            setEditing(false)
            setSelectedID(null)
            setFormError('')
          }}
        >
          {t('page.new')}
        </button>
      </div>
      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(20rem,32rem)]">
        <section className="space-y-3">
          <div className="flex flex-wrap items-center gap-2">
            <input
              className="field min-w-40 flex-1"
              value={query}
              placeholder={t('page.search')}
              aria-label={t('page.searchLabel')}
              onChange={(event) => setQuery(event.target.value)}
            />
            {(['all', 'active', 'paused', 'attention'] as const).map((key) => (
              <button
                key={key}
                type="button"
                className={filter === key ? 'btn-primary px-3 py-1.5 text-xs' : 'btn-secondary px-3 py-1.5 text-xs'}
                onClick={() => setFilter(key)}
              >
                {t(`page.filters.${key}`)}
              </button>
            ))}
          </div>
          {listQuery.isLoading && <p className="text-sm text-ink-muted">{t('page.loading')}</p>}
          {listQuery.isError && !listQuery.data && (
            <LoadError error={listQuery.error} onRetry={() => void listQuery.refetch()} retrying={listQuery.isFetching} />
          )}
          {!listQuery.isLoading && !listQuery.isError && (listQuery.data ?? []).length === 0 && !creating && (
            <EmptyState
              mascot="idle"
              title={t('page.emptyTitle')}
              description={t('page.emptyDescription')}
            />
          )}
          {!listQuery.isLoading && (listQuery.data ?? []).length > 0 && items.length === 0 && (
            <p className="text-sm text-ink-muted">{t('page.noMatches')}</p>
          )}
          <ul className="space-y-2">
            {items.map((item) => (
              <li key={item.id}>
                <button
                  type="button"
                  className={[
                    'selectable w-full',
                    selectedID === item.id ? 'shadow-[inset_0_0_0_1.5px_rgb(var(--rgb-primary))]' : '',
                  ].filter(Boolean).join(' ')}
                  onClick={() => {
                    setSelectedID(item.id)
                    setCreating(false)
                    setEditing(false)
                    setFormError('')
                  }}
                >
                  <div className="flex items-start justify-between gap-3">
                    <p className="font-medium text-ink">{item.name}</p>
                    <StatusPill item={item} />
                  </div>
                  <p className="mt-1 text-xs text-ink-muted">{scheduleLabel(item.schedule)}</p>
                  <p className="mt-2 text-xs text-ink-faint">
                    {t('page.lastNext', {
                      last: compactWhen(item.last_run_at, item.schedule.time_zone),
                      next: compactWhen(item.next_run_at, item.schedule.time_zone),
                    })}
                  </p>
                  {resultProse(item.last_result) && <p className="mt-2 line-clamp-2 text-sm text-ink-muted">{resultProse(item.last_result)}</p>}
                </button>
              </li>
            ))}
          </ul>
        </section>
        <aside>
          {creating || editing ? (
            <AutomationForm
              key={editing ? selectedID ?? 'edit' : 'new'}
              profiles={profiles}
              models={models}
              tools={tools}
              initial={editing ? detail : null}
              seedDescription={
                creating && new URLSearchParams(window.location.search).get('compose') === '1'
                  ? screenshotSentence
                  : ''
              }
              pending={save.isPending}
              error={formError}
              onCancel={() => {
                setCreating(false)
                setEditing(false)
                setFormError('')
              }}
              onSubmit={(input) => save.mutate(input)}
            />
          ) : detail ? (
            <Detail
              detail={detail}
              models={models}
              running={runNow.isPending}
              runError={runNow.error instanceof Error ? runNow.error.message : ''}
              onRun={() => runNow.mutate(detail.id)}
              onToggle={() => pause.mutate(detail)}
              onEdit={() => {
                setEditing(true)
                setFormError('')
              }}
              onDelete={() => {
                if (window.confirm(t('page.confirmDelete', { name: detail.name }))) {
                  remove.mutate(detail.id)
                }
              }}
            />
          ) : (
            <div className="card text-sm text-ink-muted">{t('page.selectHint')}</div>
          )}
        </aside>
      </div>
    </div>
  )
}

function Detail({
  detail,
  models,
  running,
  runError,
  onRun,
  onToggle,
  onEdit,
  onDelete,
}: {
  detail: AutomationDetail
  models: Model[]
  running: boolean
  runError: string
  onRun: () => void
  onToggle: () => void
  onEdit: () => void
  onDelete: () => void
}) {
  const { t } = useTranslation('automations')
  const zone = detail.schedule.time_zone
  return (
    <div className="card space-y-4">
      <div>
        <div className="flex items-start justify-between gap-3">
          <h2 className="font-display text-lg font-semibold text-ink">{detail.name}</h2>
          <StatusPill item={detail} />
        </div>
        <div className="mt-4 space-y-3 text-sm">
          <div>
            <p className="text-xs tracking-wide text-ink-faint">{t('detail.schedule')}</p>
            <p className="text-ink">{scheduleLabel(detail.schedule)}</p>
            <p className="text-ink-muted">{t('detail.next', { when: compactWhen(detail.next_run_at, zone) })}</p>
          </div>
          <div>
            <p className="text-xs tracking-wide text-ink-faint">{t('detail.task')}</p>
            <p className="whitespace-pre-wrap text-ink">{visibleTask(detail.prompt)}</p>
          </div>
          <div>
            <p className="text-xs tracking-wide text-ink-faint">{t('detail.notification')}</p>
            <p className="text-ink">{notificationLabel(detail.notification)}</p>
            <p className="text-ink-muted">{detail.notification.mode === 'none' ? t('detail.storedHere') : t('detail.onThisComputer')}</p>
          </div>
        </div>
        {detail.last_error && <p className="mt-3 text-sm text-danger">{detail.last_error}</p>}
      </div>
      <div className="flex flex-wrap gap-2">
        <button type="button" className="btn-primary px-3 py-1.5 text-xs" disabled={running} onClick={onRun}>
          {running ? t('detail.running') : t('detail.runNow')}
        </button>
        <button type="button" className="btn-secondary px-3 py-1.5 text-xs" onClick={onToggle}>
          {detail.enabled ? t('detail.pause') : t('detail.resume')}
        </button>
        <button type="button" className="btn-secondary px-3 py-1.5 text-xs" onClick={onEdit}>
          {t('detail.edit')}
        </button>
        <button type="button" className="btn-danger px-3 py-1.5 text-xs" onClick={onDelete}>
          {t('detail.delete')}
        </button>
      </div>
      {runError && <p className="text-sm text-danger">{runError}</p>}
      <div>
        <h3 className="text-sm font-medium text-ink">{t('detail.history')}</h3>
        {detail.history.length === 0 ? (
          <p className="mt-2 text-sm text-ink-muted">{t('detail.notRunYet')}</p>
        ) : (
          <ul className="mt-2 space-y-3">
            {detail.history.map((run, index) => (
              <HistoryRow
                key={run.id}
                run={run}
                zone={zone}
                notification={detail.notification}
                previous={detail.history.slice(index + 1).find((item) => item.status === 'succeeded')?.result}
                previousNotified={detail.history.slice(index + 1).find((item) => item.status === 'succeeded')?.notification_sent ?? false}
                models={models}
              />
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}

function HistoryRow({
  run,
  zone,
  notification,
  previous,
  previousNotified = false,
  models,
}: {
  run: AutomationRun
  zone: string
  notification: AutomationDetail['notification']
  previous?: string
  previousNotified?: boolean
  models: Model[]
}) {
  const { t } = useTranslation('automations')
  const notice = explainRun(notification, run, previous, previousNotified)
  const prose = resultProse(run.result)
  const modelName = models.find((model) => model.id === run.model_id)?.display_name || run.model_id
  return (
    <li className="rounded-lg bg-raised/50 p-3">
      <div className="flex items-start justify-between gap-3">
        <p className="text-sm font-medium text-ink">{statusLabel(run.status)}</p>
        <p className="text-xs text-ink-faint">{notice.title}</p>
      </div>
      <p className="mt-1 text-xs text-ink-faint">{runTiming(run, zone)}</p>
      {prose && <p className="mt-2 whitespace-pre-wrap text-sm text-ink-muted">{prose}</p>}
      {notice.detail && <p className="mt-1 text-sm text-ink-muted">{notice.detail}</p>}
      {run.error && <p className="mt-2 text-sm text-danger">{run.error}</p>}
      <details className="mt-2">
        <summary className="cursor-pointer text-xs text-ink-faint">{t('run.details')}</summary>
        <div className="mt-2 space-y-1 text-xs text-ink-faint">
          <p>{t('run.scheduled', { when: clockDetail(run.occurrence_at, zone) })}</p>
          {run.started_at && <p>{t('run.started', { when: clockDetail(run.started_at, zone) })}</p>}
          {run.finished_at && <p>{t('run.finished', { when: clockDetail(run.finished_at, zone) })}</p>}
          {modelName && <p>{t('run.model', { model: modelName })}</p>}
          {run.node_id && <p>{t('run.computer', { computer: run.node_id })}</p>}
          {run.attempt > 1 && <p>{t('run.attempt', { n: run.attempt })}</p>}
        </div>
      </details>
    </li>
  )
}

function StatusPill({ item }: { item: Pick<Automation, 'enabled' | 'last_status' | 'consecutive_failures'> }) {
  const { t } = useTranslation('automations')
  const failed = item.last_status === 'failed' || item.consecutive_failures > 0
  const label = !item.enabled ? t('pill.paused') : failed ? t('pill.failed') : t('pill.enabled')
  const mark = !item.enabled ? 'Ⅱ' : failed ? '!' : '●'
  return (
    <span className={failed ? 'text-xs text-danger' : item.enabled ? 'text-xs text-success' : 'text-xs text-ink-faint'}>
      {mark} {label}
    </span>
  )
}

function matchesAutomation(item: Automation, query: string, filter: 'all' | 'active' | 'paused' | 'attention'): boolean {
  const needle = query.trim().toLowerCase()
  if (needle && !`${item.name} ${item.prompt} ${item.last_result ?? ''}`.toLowerCase().includes(needle)) return false
  if (filter === 'active') return item.enabled
  if (filter === 'paused') return !item.enabled
  if (filter === 'attention') return item.last_status === 'failed' || item.consecutive_failures > 0
  return true
}

function statusLabel(status: string | undefined): string {
  switch (status) {
    case 'succeeded':
      return i18n.t('automations:run.status.succeeded')
    case 'failed':
      return i18n.t('automations:run.status.failed')
    case 'retrying':
      return i18n.t('automations:run.status.retrying')
    case 'running':
    case 'claimed':
      return i18n.t('automations:run.status.running')
    default:
      return i18n.t('automations:run.status.notRun')
  }
}
