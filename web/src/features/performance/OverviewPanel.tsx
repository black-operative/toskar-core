import { useQuery } from '@tanstack/react-query'
import { Trans, useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { LoadingSpinner } from '@/components/ui/LoadingSpinner'
import { api } from '@/lib/api'
import { describeNodeHardware } from '@/features/nodes/nodePresentation'
import type { Node, RunningModelView, Task } from '@/types/api'
import { formatTokPerSec, memoryUsePercent } from './performanceFormat'
import { formatPercent } from '@/i18n/format'
import { LoadError } from '@/components/ui/LoadError'

function MemoryBar({ percent }: { percent: number }) {
  const { t } = useTranslation('performance')
  return (
    <div className="flex items-center gap-3">
      <span className="w-16 shrink-0 text-xs text-ink-muted">{t('overview.memory')}</span>
      <div className="h-2 flex-1 overflow-hidden rounded-full bg-raised">
        <div
          className="h-full bg-primary/80 transition-all duration-300"
          style={{ width: `${percent}%` }}
        />
      </div>
      <span className="w-10 shrink-0 text-end text-xs tabular-nums text-ink">{formatPercent(percent / 100)}</span>
    </div>
  )
}

function NodeLiveCard({
  node,
  running,
}: {
  node: Node
  running: RunningModelView[]
}) {
  const { t } = useTranslation('performance')
  const hw = describeNodeHardware(node.hardware)
  const memPct = memoryUsePercent(
    node.hardware?.memory?.total_bytes,
    node.hardware?.memory?.available_bytes,
  )
  const online = node.is_local || node.status === 'online'
  const idle = running.length === 0

  return (
    <article
      className={[
        'card space-y-3',
        !online ? 'opacity-70' : '',
      ].join(' ')}
    >
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <h2 className="font-display text-lg font-semibold text-ink">{node.name}</h2>
          {!hw.unavailable && (
            <p className="mt-0.5 text-sm text-ink-muted">
              {[hw.primary, hw.secondary].filter(Boolean).join(' · ')}
            </p>
          )}
        </div>
        <span
          className={[
            'status-chip shrink-0',
            online ? 'bg-success/15 text-success' : 'bg-raised text-ink-muted',
          ].join(' ')}
        >
          {online ? (idle ? t('overview.idle') : t('overview.active')) : t('overview.offline')}
        </span>
      </div>

      {!online ? (
        <p className="text-sm text-ink-faint">{t('overview.unreachable')}</p>
      ) : idle ? (
        <p className="text-sm text-ink-muted">{t('overview.idleNoModels')}</p>
      ) : (
        <div className="space-y-3">
          {memPct != null && <MemoryBar percent={memPct} />}
          <ul className="space-y-3">
            {running.map((r) => (
              <li key={r.instance_id} className="space-y-1 text-sm">
                <div className="flex justify-between gap-2">
                  <span className="text-ink-muted">{t('overview.model')}</span>
                  <span className="truncate font-medium text-ink">{r.display_name}</span>
                </div>
                <div className="flex justify-between gap-2">
                  <span className="text-ink-muted">{t('overview.speed')}</span>
                  <span className="tabular-nums text-ink">{formatTokPerSec(r.speed_tok_per_sec ?? 0)}</span>
                </div>
              </li>
            ))}
          </ul>
        </div>
      )}
    </article>
  )
}

export function OverviewPanel() {
  const { t } = useTranslation('performance')
  const nodesQuery = useQuery({
    queryKey: ['nodes'],
    queryFn: () => api.getNodes(),
    retry: false,
    refetchInterval: 5_000,
  })

  const runningQuery = useQuery({
    queryKey: ['models-running'],
    queryFn: () => api.listRunningModels(),
    retry: false,
    refetchInterval: 3_000,
  })

  const tasksQuery = useQuery({
    queryKey: ['tasks'],
    queryFn: () => api.listTasks(),
    retry: false,
    refetchInterval: 5_000,
  })

  const fleet = (nodesQuery.data ?? []).filter((n) => n.is_local || n.paired)
  const running = runningQuery.data ?? []
  const tasks = (tasksQuery.data ?? []).filter(
    (task: Task) => task.status === 'running' || task.status === 'pending',
  )

  const connected = fleet.filter((n) => n.is_local || n.status === 'online').length
  const crossing =
    running.length > 0 &&
    new Set(running.map((r) => r.node_id).filter(Boolean)).size > 1

  const byNode = new Map<string, RunningModelView[]>()
  for (const r of running) {
    const id = r.node_id || ''
    const list = byNode.get(id) ?? []
    list.push(r)
    byNode.set(id, list)
  }

  if (nodesQuery.isLoading) {
    return <LoadingSpinner label={t('overview.loading')} />
  }

  return (
    <div className="space-y-6 animate-fade">
      <section className="rounded-2xl bg-raised/40 px-5 py-4">
        <p className="text-sm text-ink">
          {[
            t('overview.computers', { count: connected }),
            t('overview.models', { count: running.length }),
            t('overview.tasks', { count: tasks.length }),
            ...(crossing ? [t('overview.acrossMachines')] : []),
          ].join(' · ')}
        </p>
        {tasks.length > 0 && (
          <ul className="mt-3 space-y-1.5">
            {tasks.slice(0, 3).map((task) => (
              <li key={task.id} className="truncate text-xs text-ink-muted">
                <span className="font-medium text-accent">{t(`overview.taskStatus.${task.status}`)}</span>
                {' — '}
                {task.prompt?.slice(0, 80) || t('overview.taskInProgress')}
              </li>
            ))}
          </ul>
        )}
      </section>

      {nodesQuery.isError && !nodesQuery.data ? (
        <LoadError error={nodesQuery.error} onRetry={() => void nodesQuery.refetch()} retrying={nodesQuery.isFetching} />
      ) : fleet.length === 0 ? (
        <p className="text-sm text-ink-muted">
          {t('overview.noComputers')}{' '}
          <Link to="/nodes" className="text-primary hover:underline">
            {t('overview.addComputer')}
          </Link>
        </p>
      ) : (
        <ul className="grid gap-4 md:grid-cols-2">
          {fleet.map((node) => (
            <li key={node.id}>
              <NodeLiveCard
                node={node}
                running={byNode.get(node.id) ?? []}
              />
            </li>
          ))}
        </ul>
      )}

      {running.length === 0 && fleet.length > 0 && (
        <p className="text-sm text-ink-faint">
          <Trans
            t={t}
            i18nKey="overview.nothingLoaded"
            components={{ chat: <Link to="/chat" className="text-primary hover:underline" /> }}
          />
        </p>
      )}
    </div>
  )
}
