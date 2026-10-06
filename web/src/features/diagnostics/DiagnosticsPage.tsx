import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { Trans, useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import i18n from '@/i18n'
import { LoadingSpinner } from '@/components/ui/LoadingSpinner'
import { api } from '@/lib/api'
import { displayVersion } from '@/lib/appVersion'
import { formatBytes } from '@/lib/format'
import type { LogEntry, Node } from '@/types/api'
import { useUIStore } from '@/stores/uiStore'
import { RealmKicker } from '@/components/ui/Realm'
import { CapabilityPanel } from './CapabilityPanel'
import { ToolProvidersPanel } from './ToolProvidersPanel'
import { CachePanel } from './CachePanel'
import { Ratatoskr } from '@/components/ui/Ratatoskr'
import { formatRelativeTime } from '@/i18n/format'
import { MemoryPanel } from './MemoryPanel'
import { GPUSetupList } from './GPUSetupList'

function kindLabel(kind: string, advanced: boolean): string {
  const known = kind === 'daemon' || kind === 'runtime'
  if (!advanced) return i18n.t(`diagnostics:logs.kinds.${known ? kind : 'other'}`)
  return known ? i18n.t(`diagnostics:logs.kindsAdvanced.${kind}`) : kind
}

function relativeAgo(iso?: string): string | null {
  if (!iso) return null
  const t = new Date(iso).getTime()
  if (Number.isNaN(t)) return null
  const mins = Math.max(0, Math.round((Date.now() - t) / 60_000))
  if (mins < 1) return i18n.t('diagnostics:ago.justNow')
  if (mins < 60) return i18n.t('diagnostics:ago.minutes', { count: mins })
  const hours = Math.round(mins / 60)
  if (hours < 48) return i18n.t('diagnostics:ago.hours', { count: hours })
  return i18n.t('diagnostics:ago.days', { count: Math.round(hours / 24) })
}

function lastCheckedLabel(updatedAt: number | undefined): string {
  if (!updatedAt) return i18n.t('diagnostics:checked.never')
  const secs = Math.round((Date.now() - updatedAt) / 1000)
  if (secs < 8) return i18n.t('diagnostics:checked.justNow')
  return i18n.t('diagnostics:checked.at', { when: formatRelativeTime(updatedAt, new Date(), 'narrow') })
}

type RowTone = 'ok' | 'warn' | 'bad'

type StatusAction =
  | { kind: 'link'; label: string; to: string }
  | { kind: 'button'; label: string; onClick: () => void; busy?: boolean }

type StatusRow = {
  id: string
  label: string
  tone: RowTone
  detail: string
  message?: string
  /** More than a message, such as the GPU setup steps with their commands. */
  extra?: ReactNode
  actions?: StatusAction[]
}

function StatusIcon({ tone }: { tone: RowTone }) {
  const cls =
    tone === 'ok' ? 'text-success' : tone === 'warn' ? 'text-warning' : 'text-danger'
  const mark = tone === 'ok' ? '✓' : '⚠'
  return (
    <span className={['mt-0.5 shrink-0 text-sm', cls].join(' ')} aria-hidden>
      {mark}
    </span>
  )
}

function HealthRow({ row }: { row: StatusRow }) {
  const { t } = useTranslation('diagnostics')
  return (
    <li className="border-b border-line/50 py-3 last:border-0">
      <div className="flex gap-3">
        <StatusIcon tone={row.tone} />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
            <p className="text-sm font-medium text-ink">{row.label}</p>
            <p
              className={[
                'text-sm',
                row.tone === 'ok'
                  ? 'text-ink-muted'
                  : row.tone === 'warn'
                    ? 'text-warning'
                    : 'text-danger',
              ].join(' ')}
            >
              {row.detail}
            </p>
          </div>
          {row.message && (
            <p className="mt-1.5 text-sm text-ink-muted">{row.message}</p>
          )}
          {row.extra ? <div className="mt-2">{row.extra}</div> : null}
          {row.actions && row.actions.length > 0 && (
            <div className="mt-2.5 flex flex-wrap gap-2">
              {row.actions.map((action) =>
                action.kind === 'link' ? (
                  <Link
                    key={action.label}
                    to={action.to}
                    className="btn-secondary px-3 py-1.5 text-xs"
                  >
                    {action.label}
                  </Link>
                ) : (
                  <button
                    key={action.label}
                    type="button"
                    className="btn-secondary px-3 py-1.5 text-xs"
                    disabled={action.busy}
                    onClick={action.onClick}
                  >
                    {action.busy ? t('rows.working') : action.label}
                  </button>
                ),
              )}
            </div>
          )}
        </div>
      </div>
    </li>
  )
}

function offlineRemotes(nodes: Node[]): Node[] {
  return nodes.filter((n) => n.paired && !n.is_local && n.status === 'offline')
}

export function DiagnosticsPage() {
  const { t } = useTranslation('diagnostics')
  const advancedMode = useUIStore((s) => s.advancedMode)
  const queryClient = useQueryClient()
  const [selectedName, setSelectedName] = useState<string | null>(null)
  const [autoRefresh, setAutoRefresh] = useState(true)
  const [copyState, setCopyState] = useState<'idle' | 'copied' | 'failed'>('idle')
  const [showLogs, setShowLogs] = useState(false)
  const [tick, setTick] = useState(0)

  useEffect(() => {
    const id = window.setInterval(() => setTick((t) => t + 1), 15_000)
    return () => window.clearInterval(id)
  }, [])

  const healthQuery = useQuery({
    queryKey: ['health'],
    queryFn: () => api.getHealth(),
    retry: false,
    refetchInterval: 15_000,
  })
  const versionQuery = useQuery({
    queryKey: ['version'],
    queryFn: () => api.getVersion(),
    retry: false,
    enabled: advancedMode,
    staleTime: 60_000,
  })
  const nodesQuery = useQuery({
    queryKey: ['nodes'],
    queryFn: () => api.getNodes(),
    retry: false,
    refetchInterval: 15_000,
  })
  const modelsQuery = useQuery({
    queryKey: ['models'],
    queryFn: () => api.getModels(),
    retry: false,
  })
  const runningQuery = useQuery({
    queryKey: ['models-running'],
    queryFn: () => api.listRunningModels(),
    retry: false,
    refetchInterval: 10_000,
  })
  const hardwareQuery = useQuery({
    queryKey: ['hardware'],
    queryFn: () => api.getHardware(),
    retry: false,
    staleTime: 30_000,
  })
  const settingsQuery = useQuery({
    queryKey: ['settings'],
    queryFn: () => api.getSettings(),
    retry: false,
  })
  const gpuSetupQuery = useQuery({
    queryKey: ['gpu-setup'],
    queryFn: () => api.getGPUSetup(),
    retry: false,
    refetchInterval: 60_000,
  })
  const runtimesQuery = useQuery({
    queryKey: ['runtimes'],
    queryFn: () => api.listRuntimes(),
    retry: false,
  })

  const logsQuery = useQuery({
    queryKey: ['logs'],
    queryFn: () => api.listLogs(),
    retry: false,
    enabled: advancedMode && showLogs,
    refetchInterval: autoRefresh && showLogs ? 5_000 : false,
  })

  const refreshMutation = useMutation({
    mutationFn: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['health'] }),
        queryClient.invalidateQueries({ queryKey: ['nodes'] }),
        queryClient.invalidateQueries({ queryKey: ['models-running'] }),
        queryClient.invalidateQueries({ queryKey: ['hardware'] }),
        api.refreshNodes().catch(() => null),
      ])
    },
  })

  const logEntries = logsQuery.data ?? []
  const serviceOk = healthQuery.data?.status === 'ok'
  const nodes = nodesQuery.data ?? []
  const fleet = nodes.filter((n) => n.is_local || n.paired)
  const connected = fleet.filter((n) => n.is_local || n.status === 'online').length
  const running = runningQuery.data ?? []
  const hasModel = (modelsQuery.data ?? []).some(
    (m) => m.installed || (m.installed_on?.length ?? 0) > 0,
  )
  const runtimeInstalled = (runtimesQuery.data ?? []).some(
    (r) => r.status === 'installed' || r.detection?.installed,
  )
  const diskAvail = hardwareQuery.data?.disk?.available_bytes
  const modelsBytes = (modelsQuery.data ?? [])
    .filter((m) => m.installed || (m.installed_on?.length ?? 0) > 0)
    .reduce((sum, m) => sum + (m.size_bytes ?? 0), 0)
  const settings = settingsQuery.data
  const apiDetail = !settings ? '—' : settings.lan_api_enabled ? t('rows.api.lan') : t('rows.api.local')

  const rows: StatusRow[] = useMemo(() => {
    const offlineNodes = offlineRemotes(nodes)
    const out: StatusRow[] = []

    out.push({
      id: 'control',
      label: t('rows.control.label'),
      tone: serviceOk ? 'ok' : 'bad',
      detail: serviceOk ? t('rows.healthy') : t('rows.control.notResponding'),
      message: serviceOk ? undefined : t('rows.control.message'),
      actions: serviceOk
        ? undefined
        : [
            {
              kind: 'button',
              label: t('rows.retry'),
              onClick: () => refreshMutation.mutate(),
              busy: refreshMutation.isPending,
            },
            { kind: 'link', label: t('rows.openSettings'), to: '/settings' },
          ],
    })

    let runtimeTone: RowTone = 'ok'
    let runtimeDetail: string
    let runtimeMessage: string | undefined
    let runtimeActions: StatusAction[] | undefined
    if (!serviceOk) {
      runtimeTone = 'bad'
      runtimeDetail = t('rows.runtime.needsAttention')
      runtimeMessage = t('rows.runtime.notResponding')
      runtimeActions = [
        { kind: 'link', label: t('rows.viewDetails'), to: '/models' },
        {
          kind: 'button',
          label: t('rows.retry'),
          onClick: () => refreshMutation.mutate(),
          busy: refreshMutation.isPending,
        },
      ]
    } else if (!runtimeInstalled && !hasModel) {
      runtimeTone = 'warn'
      runtimeDetail = t('rows.runtime.notSetUp')
      runtimeMessage = t('rows.runtime.installModel')
      runtimeActions = [{ kind: 'link', label: t('rows.openModels'), to: '/models' }]
    } else if (running.length === 0) {
      runtimeDetail = t('rows.runtime.idle')
    } else {
      runtimeDetail = t('rows.runtime.running', { count: running.length })
    }
    out.push({
      id: 'runtime',
      label: t('rows.runtime.label'),
      tone: runtimeTone,
      detail: runtimeDetail,
      message: runtimeMessage,
      actions: runtimeActions,
    })

    // Where this computer's loaded models run (#317): amber when a model is
    // only partly on the GPU, or a GPU sits unused; the reason comes from
    // the model in that state.
    // and what this computer still needs for its GPU, each with its fix.
    const accel = healthQuery.data?.acceleration
    const gpuProblems = Array.isArray(gpuSetupQuery.data?.problems) ? gpuSetupQuery.data.problems : []
    if (serviceOk && (accel || gpuProblems.length > 0)) {
      const localId = nodes.find((n) => n.is_local)?.id
      const reason = running.find(
        (r) => (!localId || r.node_id === localId) && r.acceleration?.state === accel,
      )?.acceleration?.reason
      const modelsOff = accel === 'partial' || accel === 'cpu'
      out.push({
        id: 'gpu',
        label: t('rows.gpu.label'),
        tone: modelsOff || gpuProblems.length > 0 ? 'warn' : 'ok',
        detail: gpuProblems.length > 0 ? t('rows.gpu.setupNeeded') : t(`rows.gpu.${accel}`),
        // The setup steps say what to do; without them, the model's reason does.
        message: gpuProblems.length === 0 && modelsOff && reason ? i18n.t(`common:acceleration.reason.${reason}`) : undefined,
        extra: gpuProblems.length > 0 ? <GPUSetupList problems={gpuProblems} /> : undefined,
        actions: modelsOff ? [{ kind: 'link', label: t('rows.gpu.openModels'), to: '/models' }] : undefined,
      })
    }

    out.push({
      id: 'database',
      label: t('rows.database.label'),
      tone: serviceOk ? 'ok' : 'bad',
      detail: serviceOk ? t('rows.healthy') : t('rows.unavailable'),
    })

    if (offlineNodes.length > 0) {
      const names = offlineNodes.map((n) => n.name).join(', ')
      const ago = relativeAgo(offlineNodes[0]?.last_seen_at)
      out.push({
        id: 'bifrost',
        label: t('rows.bifrost.label'),
        tone: 'warn',
        detail: t('rows.bifrost.offline', { count: offlineNodes.length }),
        message: ago
          ? t('rows.bifrost.silentFor', { count: offlineNodes.length, names, ago })
          : t('rows.bifrost.silent', { count: offlineNodes.length, names }),
        actions: [
          {
            kind: 'button',
            label: t('rows.retry'),
            onClick: () => refreshMutation.mutate(),
            busy: refreshMutation.isPending,
          },
          { kind: 'link', label: t('rows.openComputers'), to: '/nodes' },
        ],
      })
    } else {
      out.push({
        id: 'bifrost',
        label: t('rows.bifrost.label'),
        tone: connected > 0 || serviceOk ? 'ok' : 'warn',
        detail: connected > 0 ? t('rows.bifrost.connected', { count: connected }) : t('rows.bifrost.none'),
        message: connected === 0 ? t('rows.bifrost.noneHint') : undefined,
        actions:
          connected === 0
            ? [
                { kind: 'link', label: t('rows.openComputers'), to: '/nodes' },
                {
                  kind: 'button',
                  label: t('rows.retry'),
                  onClick: () => refreshMutation.mutate(),
                  busy: refreshMutation.isPending,
                },
              ]
            : undefined,
      })
    }

    const storageTight =
      diskAvail != null && diskAvail > 0 && diskAvail < 5 * 1024 ** 3
    out.push({
      id: 'storage',
      label: t('rows.storage.label'),
      tone: !serviceOk ? 'bad' : storageTight ? 'warn' : 'ok',
      detail:
        diskAvail != null && diskAvail > 0
          ? t('rows.storage.available', { size: formatBytes(diskAvail) })
          : serviceOk
            ? t('rows.healthy')
            : t('rows.unavailable'),
      message:
        modelsBytes > 0
          ? t('rows.storage.modelsUse', { size: formatBytes(modelsBytes) })
          : storageTight
            ? t('rows.storage.freeUp')
            : undefined,
      actions: storageTight ? [{ kind: 'link', label: t('rows.manageModels'), to: '/models' }] : undefined,
    })

    out.push({
      id: 'api',
      label: t('rows.api.label'),
      tone: serviceOk ? 'ok' : 'bad',
      detail: serviceOk ? apiDetail : t('rows.unavailable'),
      message: settings?.lan_api_enabled ? t('rows.api.lanHint') : undefined,
      actions: serviceOk ? [{ kind: 'link', label: t('rows.apiSettings'), to: '/api-access' }] : undefined,
    })

    return out
  }, [
    serviceOk,
    connected,
    nodes,
    running,
    healthQuery.data?.acceleration,
    gpuSetupQuery.data,
    runtimeInstalled,
    hasModel,
    diskAvail,
    modelsBytes,
    apiDetail,
    settings?.lan_api_enabled,
    refreshMutation,
    tick,
    t,
  ])

  const allOk = rows.every((r) => r.tone === 'ok')
  const hasWarn = rows.some((r) => r.tone === 'warn')

  useEffect(() => {
    if (!advancedMode) setShowLogs(false)
  }, [advancedMode])

  useEffect(() => {
    if (!selectedName && logEntries.length > 0) {
      const preferred =
        logEntries.find((l) => l.name === 'daemon.log') ?? logEntries[0]
      setSelectedName(preferred.name)
    }
  }, [logEntries, selectedName])

  const logQuery = useQuery({
    queryKey: ['log', selectedName],
    queryFn: () => (selectedName ? api.getLog(selectedName) : null),
    enabled: Boolean(selectedName) && advancedMode && showLogs,
    retry: false,
    refetchInterval: autoRefresh && selectedName && showLogs ? 3_000 : false,
  })

  const diagnosticsMutation = useMutation({
    mutationFn: () => api.exportDiagnostics(false),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['logs'] })
    },
  })

  const selectedMeta: LogEntry | undefined = useMemo(
    () => logEntries.find((l) => l.name === selectedName),
    [logEntries, selectedName],
  )

  const handleCopy = async () => {
    const text = logQuery.data?.content ?? ''
    try {
      await navigator.clipboard.writeText(text)
      setCopyState('copied')
      setTimeout(() => setCopyState('idle'), 2000)
    } catch {
      setCopyState('failed')
      setTimeout(() => setCopyState('idle'), 2000)
    }
  }

  const headline = !serviceOk ? t('page.attention') : hasWarn ? t('page.mostly') : t('page.normal')

  return (
    <div className="mx-auto flex w-full max-w-3xl min-w-0 flex-col gap-6">
      <header className="page-header flex min-w-0 flex-wrap items-end justify-between gap-3">
        <div className="flex min-w-0 items-center gap-4">
          {!serviceOk ? <Ratatoskr state="error" size={96} /> : null}
          <div className="min-w-0">
            <RealmKicker />
            <h1 className="page-title">{t('page.title')}</h1>
            <p className="page-subtitle">{headline}</p>
          </div>
        </div>
        <div className="shrink-0 text-end">
          <button
            type="button"
            className="btn-primary"
            disabled={diagnosticsMutation.isPending}
            onClick={() => diagnosticsMutation.mutate()}
          >
            {diagnosticsMutation.isPending ? t('page.creating') : t('page.export')}
          </button>
          <p className="mt-1.5 max-w-[14rem] text-start text-[11px] text-ink-faint sm:text-end">{t('page.exportHint')}</p>
        </div>
      </header>

      <section className="card min-w-0 space-y-1">
        <div className="flex flex-wrap items-start justify-between gap-3 border-b border-line/50 pb-3">
          <div>
            <p className="label-caps text-[10px] text-ink-faint">{t('page.overall')}</p>
            <p
              className={[
                'mt-1 text-sm font-medium',
                allOk ? 'text-success' : hasWarn ? 'text-warning' : 'text-danger',
              ].join(' ')}
            >
              {allOk ? t('page.allOk') : !serviceOk ? t('page.needsAttention') : t('page.checkBelow')}
            </p>
          </div>
          <div className="flex items-center gap-2 text-xs text-ink-faint">
            <span>{lastCheckedLabel(healthQuery.dataUpdatedAt)}</span>
            <button
              type="button"
              className="text-primary hover:underline"
              disabled={refreshMutation.isPending || healthQuery.isFetching}
              onClick={() => refreshMutation.mutate()}
            >
              {refreshMutation.isPending || healthQuery.isFetching ? t('page.checking') : t('page.refresh')}
            </button>
          </div>
        </div>

        <ul>
          {rows.map((row) => (
            <HealthRow key={row.id} row={row} />
          ))}
        </ul>

        {!hasModel && serviceOk && (
          <p className="border-t border-line/50 pt-3 text-sm text-ink-muted">{t('page.tip')}</p>
        )}
      </section>

      {diagnosticsMutation.isSuccess && diagnosticsMutation.data && (
        <div className="min-w-0 rounded-lg bg-primary-soft px-4 py-3 text-sm text-ink">
          {advancedMode ? (
            <Trans
              t={t}
              i18nKey="page.bundleAt"
              values={{ path: diagnosticsMutation.data.path }}
              components={{ path: <code className="break-anywhere font-mono text-xs" title={diagnosticsMutation.data.path} /> }}
            />
          ) : (
            t('page.bundleShare')
          )}
        </div>
      )}
      {diagnosticsMutation.isError && (
        <div className="rounded-lg bg-danger/10 px-4 py-3 text-sm text-danger">
          {t('page.bundleFailed')}
        </div>
      )}

      <CapabilityPanel />
      <ToolProvidersPanel />
      <MemoryPanel />

      {advancedMode && <ToolActivityPanel />}

      {advancedMode && <CachePanel />}

      {advancedMode && (
        <section className="card space-y-3">
          <h2 className="section-title">{t('env.title')}</h2>
          <dl className="grid gap-2 text-sm sm:grid-cols-2">
            <div className="flex justify-between gap-2 sm:block">
              <dt className="text-ink-muted">{t('env.version')}</dt>
              <dd className="tabular-nums text-ink">
                {displayVersion(versionQuery.data?.version ?? healthQuery.data?.version) || '—'}
              </dd>
            </div>
            <div className="flex justify-between gap-2 sm:block">
              <dt className="text-ink-muted">{t('env.apiBind')}</dt>
              <dd className="font-mono text-xs text-ink">
                {settings
                  ? `${settings.api_host}:${settings.api_port}`
                  : '—'}
              </dd>
            </div>
            <div className="flex justify-between gap-2 sm:block">
              <dt className="text-ink-muted">{t('env.node')}</dt>
              <dd className="truncate text-ink" title={settings?.node_id}>
                {settings?.node_name ?? '—'}
              </dd>
            </div>
            <div className="flex justify-between gap-2 sm:block">
              <dt className="text-ink-muted">{t('env.dataDir')}</dt>
              <dd
                className="break-anywhere font-mono text-xs text-ink"
                title={settings?.data_dir}
              >
                {settings?.data_dir ?? '—'}
              </dd>
            </div>
          </dl>
        </section>
      )}

      {advancedMode && (
        <div>
          <button
            type="button"
            className="btn-secondary"
            onClick={() => setShowLogs((v) => !v)}
          >
            {showLogs ? t('logs.hide') : t('logs.view')}
          </button>
        </div>
      )}

      {advancedMode && showLogs && (
        <div className="grid h-[min(36rem,calc(100dvh-14rem))] min-h-[20rem] min-w-0 gap-4 lg:grid-cols-[minmax(0,14rem)_minmax(0,1fr)]">
          <aside className="card flex min-h-0 min-w-0 flex-col overflow-hidden">
            <div className="mb-2 flex shrink-0 items-center justify-between gap-2">
              <h2 className="text-sm font-semibold text-ink">{t('logs.files')}</h2>
              <label className="flex items-center gap-1.5 text-xs text-ink-muted">
                <input
                  type="checkbox"
                  checked={autoRefresh}
                  onChange={(e) => setAutoRefresh(e.target.checked)}
                  className="rounded border-line"
                />
                {t('logs.live')}
              </label>
            </div>
            {logsQuery.isLoading && <LoadingSpinner label={t('logs.loading')} />}
            {!logsQuery.isLoading && logEntries.length === 0 && (
              <p className="mt-3 text-sm text-ink-muted">{t('logs.none')}</p>
            )}
            <ul className="mt-3 min-h-0 flex-1 space-y-1 overflow-y-auto overflow-x-hidden">
              {logEntries.map((entry) => (
                <li key={entry.name} className="min-w-0">
                  <button
                    type="button"
                    onClick={() => setSelectedName(entry.name)}
                    className={[
                      'w-full min-w-0 rounded-lg px-3 py-2 text-start transition',
                      selectedName === entry.name
                        ? 'bg-raised text-ink'
                        : 'text-ink-muted hover:bg-raised',
                    ].join(' ')}
                  >
                    <p className="truncate text-sm font-medium">
                      {entry.label || entry.name}
                    </p>
                    <p className="mt-0.5 text-xs text-ink-muted">
                      {kindLabel(entry.kind, true)} · {formatBytes(entry.size_bytes)}
                    </p>
                    <p className="mono-id mt-0.5 text-ink-faint" title={entry.name}>
                      {entry.name}
                    </p>
                  </button>
                </li>
              ))}
            </ul>
          </aside>

          <section className="card flex min-h-0 min-w-0 flex-col overflow-hidden">
            {!selectedName ? (
              <div className="flex flex-1 flex-col justify-center">
                <p className="font-display text-lg font-semibold text-ink">{t('logs.select')}</p>
                <p className="mt-1 max-w-prose text-sm text-ink-muted">{t('logs.selectHint')}</p>
              </div>
            ) : (
              <>
                <div className="mb-3 flex shrink-0 flex-wrap items-center justify-between gap-2 pb-3">
                  <div className="min-w-0">
                    <p className="truncate font-medium text-ink">
                      {logQuery.data?.label ?? selectedMeta?.label ?? selectedName}
                    </p>
                    <p className="text-xs text-ink-muted">
                      {logQuery.data?.truncated ? t('logs.recent') : t('logs.full')}
                      {selectedMeta
                        ? ` · ${formatBytes(selectedMeta.size_bytes)}`
                        : ''}
                    </p>
                  </div>
                  <button type="button" className="btn-secondary shrink-0" onClick={handleCopy}>
                    {copyState === 'copied' ? t('logs.copied') : copyState === 'failed' ? t('logs.copyFailed') : t('logs.copy')}
                  </button>
                </div>

                {logQuery.isLoading && <LoadingSpinner label={t('logs.reading')} />}
                {logQuery.isError && (
                  <p className="text-sm text-danger">{t('logs.readFailed')}</p>
                )}
                {logQuery.data && (
                  <pre className="log-panel flex-1">{logQuery.data.content || t('logs.empty')}</pre>
                )}
              </>
            )}
          </section>
        </div>
      )}
    </div>
  )
}

function ToolActivityPanel() {
  const { t } = useTranslation('diagnostics')
  const activityQuery = useQuery({
    queryKey: ['tool-activity'],
    queryFn: () => api.toolActivity(),
    refetchInterval: 5000,
  })
  const rows = [...(activityQuery.data ?? [])].reverse().slice(0, 12)
  return (
    <section className="card space-y-2">
      <h2 className="section-title">{t('tools.title')}</h2>
      {rows.length === 0 ? (
        <p className="text-sm text-ink-muted">{t('tools.none')}</p>
      ) : (
        <ul className="space-y-1 text-xs text-ink-muted">
          {rows.map((row, index) => (
            <li key={`${row.at}-${row.tool_id}-${index}`}>
              <span className="text-ink">{row.tool_id}</span> · {row.status}
              {row.summary ? ` · ${row.summary}` : ''}
              {row.error ? ` · ${row.error}` : ''}
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
