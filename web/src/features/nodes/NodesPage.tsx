import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo, useState, type MouseEvent } from 'react'
import { Trans, useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import i18n from '@/i18n'
import { EmptyState } from '@/components/ui/EmptyState'
import { ApiError, api } from '@/lib/api'
import { formatBytes } from '@/lib/format'
import { useUIStore } from '@/stores/uiStore'
import type { Model, Node, PairingSession } from '@/types/api'
import { DeployModelsPanel } from './DeployModelsPanel'
import { NetworkSettings } from './NetworkSettings'
import { JoinByCommand } from './JoinByCommand'
import { HowItWorks, stepIcons } from '@/components/ui/HowItWorks'
import {
  availableForLabels,
  combinedMemoryBytes,
  describeNodeHardware,
  membershipKind,
  membershipLabel,
  onlineLabel,
  onlineState,
  teamBestAt,
} from './nodePresentation'
import { RealmKicker } from '@/components/ui/Realm'
import { Ratatoskr } from '@/components/ui/Ratatoskr'
import { useMascotState } from '@/lib/ratatoskr/useMascotState'
import { LoadError } from '@/components/ui/LoadError'
import { Skeleton } from '@/components/ui/Skeleton'

function errorMessage(err: unknown): string {
  if (err instanceof ApiError) return err.message
  if (err instanceof Error) return err.message
  return i18n.t('computers:errors.generic')
}

function modelsOnNode(models: Model[], nodeId: string): Model[] {
  return models.filter((m) =>
    (m.installed_on ?? []).some((p) => p.node_id === nodeId),
  )
}

function diskUsePercent(node: Node): number | null {
  const total = node.hardware?.disk?.total_bytes
  const avail = node.hardware?.disk?.available_bytes
  if (!total || total <= 0 || avail == null) return null
  const used = total - avail
  if (used < 0) return null
  return Math.min(100, Math.round((used / total) * 100))
}

function Badge({
  kind,
}: {
  kind: ReturnType<typeof membershipKind>
}) {
  const styles: Record<typeof kind, string> = {
    local: 'bg-primary/15 text-primary',
    paired: 'bg-accent/15 text-accent',
    nearby: 'bg-bifrost/15 text-bifrost',
    offline: 'bg-raised text-ink-muted',
  }
  return (
    <span className={['status-chip shrink-0', styles[kind]].join(' ')}>
      {membershipLabel(kind)}
    </span>
  )
}

function ComputerCard({
  node,
  runningCount,
  installedModels,
  claimCode,
  onClaimCode,
  onPair,
  onApproveCode,
  onRevoke,
  onRemoveModel,
  removingModelId,
  pairingBusy,
  claimBusy,
  revokeBusy,
}: {
  node: Node
  runningCount: number
  installedModels: Model[]
  claimCode: string
  onClaimCode: (code: string) => void
  onPair: () => void
  onApproveCode: () => void
  onRevoke: () => void
  onRemoveModel: (modelId: string) => void
  removingModelId: string | null
  pairingBusy: boolean
  claimBusy: boolean
  revokeBusy: boolean
}) {
  const { t } = useTranslation('computers')
  const advancedMode = useUIStore((s) => s.advancedMode)
  const [manageOpen, setManageOpen] = useState(false)
  const [menuOpen, setMenuOpen] = useState(false)
  const kind = membershipKind(node)
  const hw = describeNodeHardware(node.hardware)
  const capabilities = availableForLabels(installedModels, node.hardware)
  const diskPct = diskUsePercent(node)
  const diskFree = node.hardware?.disk?.available_bytes
  const diskTotal = node.hardware?.disk?.total_bytes
  const diskTight = diskPct != null && diskPct >= 90
  const online = onlineLabel(node)
  const isOnline = onlineState(node) === 'online'
  const nearby = kind === 'nearby'
  const inTeam = kind === 'local' || kind === 'paired' || kind === 'offline'

  useEffect(() => {
    if (!menuOpen) return
    const close = () => setMenuOpen(false)
    window.addEventListener('click', close)
    return () => window.removeEventListener('click', close)
  }, [menuOpen])

  return (
    <article
      className={[
        'card flex h-full flex-col transition duration-150 animate-fade',
        nearby ? 'ring-1 ring-bifrost/40' : '',
        kind === 'local' ? 'ring-1 ring-primary/25' : '',
        diskTight && inTeam ? 'ring-1 ring-danger/40' : '',
        kind === 'offline' ? 'opacity-80' : '',
      ].join(' ')}
    >
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <h2 className="font-display text-lg font-semibold tracking-tight text-ink">
            {node.name}
          </h2>
          <p className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-sm text-ink-muted">
            <Badge kind={kind} />
            {node.training ? (
              <span className="status-chip shrink-0 bg-warning/15 text-warning" title={t('card.trainingHint')}>
                {t('card.training')}
              </span>
            ) : null}
            {kind !== 'offline' && (
              <>
                <span className="text-ink-faint">·</span>
                <span
                  className={[
                    'inline-flex items-center gap-1.5',
                    isOnline ? 'text-success' : 'text-ink-muted',
                  ].join(' ')}
                >
                  <span
                    className={[
                      'h-1.5 w-1.5 rounded-full',
                      isOnline ? 'bg-success' : 'bg-ink-faint',
                    ].join(' ')}
                    aria-hidden
                  />
                  {online}
                </span>
              </>
            )}
          </p>
        </div>
      </div>

      {nearby ? (
        <p className="mt-4 text-sm text-ink-muted">{t('card.nearbyHint')}</p>
      ) : (
        <div className="mt-4 space-y-3 text-sm">
          {hw.unavailable ? (
            <p className="text-ink-faint">{t('hardware.unavailable')}</p>
          ) : (
            <div className="text-ink-muted">
              {hw.primary ? <p className="text-ink">{hw.primary}</p> : null}
              {hw.secondary ? <p className="mt-0.5">{hw.secondary}</p> : null}
            </div>
          )}

          <p className="text-ink">
            <Trans
              t={t}
              i18nKey="card.running"
              values={{ models: t('card.models', { count: runningCount }) }}
              components={{ strong: <span className="font-medium" /> }}
            />
          </p>

          {capabilities.length > 0 && (
            <p className="text-sm text-ink-muted">
              <span className="text-ink-faint">{t('card.availableFor')}</span>{' '}
              <span className="text-ink">{capabilities.join(', ')}</span>
            </p>
          )}

          {advancedMode && node.address ? (
            <p className="truncate font-mono text-[11px] text-ink-faint">{node.address}</p>
          ) : null}
        </div>
      )}

      {manageOpen && inTeam && (
        <div className="mt-4 space-y-3 border-t border-line/60 pt-4 text-sm text-ink-muted animate-fade">
          {diskPct != null && diskFree != null && diskTotal != null && (
            <div>
              <div className="mb-1 flex justify-between text-xs">
                <span>{t('card.disk')}</span>
                <span
                  className={[
                    'tabular-nums',
                    diskTight ? 'font-medium text-danger' : 'text-ink',
                  ].join(' ')}
                >
                  {t('card.diskFree', { free: formatBytes(diskFree), total: formatBytes(diskTotal) })}
                </span>
              </div>
              <div className="h-1.5 overflow-hidden rounded-full bg-raised">
                <div
                  className={[
                    'h-full transition-all duration-300',
                    diskTight ? 'bg-danger/80' : 'bg-accent/80',
                  ].join(' ')}
                  style={{ width: `${diskPct}%` }}
                />
              </div>
              {diskTight ? (
                <p className="mt-1 text-xs text-danger">{t('card.diskLow')}</p>
              ) : null}
            </div>
          )}

          <div>
            <p className="text-xs font-medium text-ink">
              {installedModels.length > 0
                ? t('card.modelsHereCount', { count: installedModels.length })
                : t('card.modelsHere')}
            </p>
            {installedModels.length === 0 ? (
              <p className="mt-1 text-xs text-ink-faint">{t('card.noModels')}</p>
            ) : (
              <ul className="mt-2 space-y-1.5">
                {installedModels.map((m) => (
                  <li
                    key={m.id}
                    className="flex items-center justify-between gap-2 rounded-lg bg-raised/50 px-2.5 py-1.5"
                  >
                    <div className="min-w-0">
                      <p className="truncate text-xs font-medium text-ink">{m.display_name}</p>
                      {m.size_bytes ? (
                        <p className="text-[10px] text-ink-faint">{formatBytes(m.size_bytes)}</p>
                      ) : null}
                    </div>
                    <button
                      type="button"
                      // Plain red text: the tinted danger button is too low-contrast on this tinted row.
                      className="inline-flex h-8 shrink-0 items-center rounded-lg px-2.5 text-xs font-semibold text-danger transition hover:bg-danger/10 disabled:opacity-50"
                      disabled={removingModelId === m.id}
                      onClick={() => onRemoveModel(m.id)}
                    >
                      {removingModelId === m.id ? t('card.removing') : t('card.remove')}
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </div>
        </div>
      )}

      <div className="mt-auto flex flex-wrap items-center gap-2 pt-5">
        {nearby && (
          <>
            <button
              type="button"
              className="btn-primary"
              onClick={onPair}
              disabled={pairingBusy || !node.address}
              title={!node.address ? t('card.noAddress') : undefined}
            >
              {pairingBusy ? t('card.connecting') : t('card.addToTeam')}
            </button>
            <div className="flex flex-wrap items-center gap-2">
              <input
                type="text"
                inputMode="numeric"
                maxLength={6}
                placeholder={t('card.codePlaceholder')}
                aria-label={t('card.codeFor', { name: node.name })}
                className="field w-24 px-2 py-1.5 text-xs font-mono"
                value={claimCode}
                onChange={(e) => onClaimCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
              />
              <button
                type="button"
                className="btn-secondary btn-sm"
                disabled={claimBusy || !node.address || claimCode.length !== 6}
                onClick={onApproveCode}
              >
                {claimBusy ? t('card.approving') : t('card.approveCode')}
              </button>
            </div>
          </>
        )}

        {inTeam && (
          <>
            <button
              type="button"
              className="btn-secondary btn-sm"
              onClick={() => setManageOpen((o) => !o)}
              aria-expanded={manageOpen}
            >
              {manageOpen ? t('card.hideDetails') : t('card.manage')}
            </button>
            {!node.is_local && node.paired && (
              <div className="relative ms-auto">
                <button
                  type="button"
                  className="icon-button"
                  aria-label={t('card.moreActions', { name: node.name })}
                  aria-haspopup="menu"
                  aria-expanded={menuOpen}
                  onClick={(e: MouseEvent) => {
                    e.stopPropagation()
                    setMenuOpen((o) => !o)
                  }}
                >
                  <svg viewBox="0 0 16 16" className="h-4 w-4" fill="currentColor" aria-hidden>
                    <circle cx="3.5" cy="8" r="1.2" />
                    <circle cx="8" cy="8" r="1.2" />
                    <circle cx="12.5" cy="8" r="1.2" />
                  </svg>
                </button>
                {menuOpen && (
                  <div className="menu min-w-[10rem]" role="menu" onClick={(e) => e.stopPropagation()}>
                    <button
                      type="button"
                      role="menuitem"
                      className="menu-item menu-item-danger"
                      disabled={revokeBusy}
                      onClick={() => {
                        setMenuOpen(false)
                        // Removing takes the computer out of the team; adding it back needs a new approval.
                        if (window.confirm(t('card.confirmRemove', { name: node.name }))) onRevoke()
                      }}
                    >
                      {revokeBusy ? t('card.removing') : t('card.removeFromTeam')}
                    </button>
                  </div>
                )}
              </div>
            )}
          </>
        )}
      </div>
    </article>
  )
}

export function NodesPage() {
  const { t } = useTranslation('computers')
  const queryClient = useQueryClient()
  const advancedMode = useUIStore((s) => s.advancedMode)
  const [pairingSession, setPairingSession] = useState<PairingSession | null>(null)
  const [claimCodes, setClaimCodes] = useState<Record<string, string>>({})
  const [removingKey, setRemovingKey] = useState<string | null>(null)
  const [joinOpen, setJoinOpen] = useState(false)

  const nodesQuery = useQuery({
    queryKey: ['nodes'],
    queryFn: () => api.getNodes(),
    retry: false,
    refetchInterval: 10_000,
  })

  const pendingQuery = useQuery({
    queryKey: ['pairing-pending'],
    queryFn: () => api.listPairingOffers(),
    retry: false,
    refetchInterval: 3000,
  })

  const runningQuery = useQuery({
    queryKey: ['models-running'],
    queryFn: () => api.listRunningModels(),
    retry: false,
    refetchInterval: 5000,
  })

  const modelsQuery = useQuery({
    queryKey: ['models'],
    queryFn: () => api.getModels(),
    retry: false,
    refetchInterval: 10_000,
  })

  const pairMutation = useMutation({
    mutationFn: (nodeId: string) => api.pairNode(nodeId),
    onSuccess: (session) => {
      if (session) setPairingSession(session)
      queryClient.invalidateQueries({ queryKey: ['nodes'] })
    },
  })

  const approveMutation = useMutation({
    mutationFn: (sessionId: string) => api.approvePairing(sessionId),
    onSuccess: () => {
      setPairingSession(null)
      queryClient.invalidateQueries({ queryKey: ['nodes'] })
      queryClient.invalidateQueries({ queryKey: ['pairing-pending'] })
    },
  })

  const claimMutation = useMutation({
    mutationFn: async ({ nodeId, code }: { nodeId: string; code: string }) => {
      const session = await api.claimPairing(nodeId, code)
      if (!session?.id) {
        throw new Error(t('errors.claimFailed'))
      }
      return api.approvePairing(session.id)
    },
    onSuccess: () => {
      setClaimCodes({})
      setPairingSession(null)
      queryClient.invalidateQueries({ queryKey: ['nodes'] })
      queryClient.invalidateQueries({ queryKey: ['pairing-pending'] })
    },
  })

  const revokeMutation = useMutation({
    mutationFn: (nodeId: string) => api.revokeNode(nodeId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['nodes'] })
    },
  })

  const refreshMutation = useMutation({
    mutationFn: () => api.refreshNodes(),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['nodes'] })
    },
  })

  // Ratatoskr carries a deploy to the other computers until a download
  // finishes or fails, and celebrates a pairing or a finished download.
  const [delivering, setDelivering] = useState(false)
  const eventMascot = useMascotState({ react: ['success', 'error'] })
  useEffect(() => {
    if (eventMascot === 'success' || eventMascot === 'error') setDelivering(false)
  }, [eventMascot])
  const headerMascot = delivering ? 'deliver' : eventMascot === 'success' ? 'success' : null

  const deployMutation = useMutation({
    onMutate: () => setDelivering(true),
    onError: () => setDelivering(false),
    mutationFn: async ({ modelId, nodeIds }: { modelId: string; nodeIds: string[] }) => {
      const errors: string[] = []
      for (const nodeId of nodeIds) {
        try {
          await api.installModel(modelId, { node_id: nodeId })
        } catch (err) {
          errors.push(`${nodeId}: ${errorMessage(err)}`)
        }
      }
      if (errors.length > 0) {
        throw new Error(errors.join('; '))
      }
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['models'] })
      queryClient.invalidateQueries({ queryKey: ['nodes'] })
    },
  })

  const removeMutation = useMutation({
    mutationFn: async ({ modelId, nodeId }: { modelId: string; nodeId: string }) => {
      setRemovingKey(`${nodeId}:${modelId}`)
      try {
        await api.deleteModel(modelId, { node_id: nodeId })
      } finally {
        setRemovingKey(null)
      }
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['models'] })
      queryClient.invalidateQueries({ queryKey: ['models-running'] })
      queryClient.invalidateQueries({ queryKey: ['nodes'] })
    },
  })

  const nodes = nodesQuery.data ?? []
  const pending = pendingQuery.data ?? []
  const running = runningQuery.data ?? []
  const models = modelsQuery.data ?? []

  const nearby = useMemo(
    () => nodes.filter((n) => !n.is_local && !n.paired),
    [nodes],
  )
  const fleet = useMemo(
    () => nodes.filter((n) => n.is_local || n.paired),
    [nodes],
  )
  const pairedRemotes = nodes.filter((n) => !n.is_local && n.paired)
  // Only this computer so far: the page explains how to add another.
  const alone = nodesQuery.isSuccess && pairedRemotes.length === 0
  const [showIntro, setShowIntro] = useState(false)

  const runningByNode = useMemo(() => {
    const map = new Map<string, number>()
    for (const r of running) {
      map.set(r.node_id, (map.get(r.node_id) ?? 0) + 1)
    }
    return map
  }, [running])

  const teamMem = useMemo(() => combinedMemoryBytes(fleet), [fleet])
  const teamCaps = useMemo(() => teamBestAt(fleet, models), [fleet, models])
  const totalRunning = running.length

  const sectionTitle = advancedMode ? t('fleet.titleAdvanced') : t('fleet.title')

  const actionError =
    pairMutation.error ??
    approveMutation.error ??
    claimMutation.error ??
    revokeMutation.error ??
    refreshMutation.error ??
    deployMutation.error ??
    removeMutation.error

  return (
    <div className="w-full min-w-0 space-y-8">
      <header className="page-header flex flex-wrap items-end justify-between gap-4">
        <div>
          <RealmKicker />
          <h1 className="page-title">{t('nav.computers', { ns: 'common' })}</h1>
          <p className="page-subtitle">{t('page.subtitle')}</p>
        </div>
        <div className="flex flex-wrap gap-2">
          {alone ? null : (
            <button
              type="button"
              className="btn-secondary"
              aria-expanded={showIntro}
              aria-controls="computers-intro"
              onClick={() => setShowIntro((v) => !v)}
            >
              {showIntro ? t('page.hideHowItWorks') : t('page.howItWorks')}
            </button>
          )}
          <button type="button" className="btn-secondary" aria-expanded={joinOpen} onClick={() => setJoinOpen((v) => !v)}>
            {t('join.open')}
          </button>
          <button
            type="button"
            className="btn-primary"
            disabled={refreshMutation.isPending}
            onClick={() => refreshMutation.mutate()}
          >
            {refreshMutation.isPending ? t('page.scanning') : t('page.find')}
          </button>
        </div>
        {headerMascot ? <Ratatoskr state={headerMascot} size={96} className="order-first sm:order-none" /> : null}
      </header>

      {fleet.length > 0 && (
        <section className="rounded-2xl bg-raised/40 px-5 py-4 animate-fade">
          <p className="label-caps text-[10px] text-ink-faint">
            {advancedMode ? t('status.cluster') : t('status.team')}
          </p>
          <p className="mt-1 text-sm text-ink">
            {t('status.computers', { count: fleet.length })}
            {teamMem > 0 ? t('status.combinedMemory', { size: formatBytes(teamMem) }) : ''}
            {t('status.modelsRunning', { count: totalRunning })}
          </p>
          {teamCaps.length > 0 && (
            <p className="mt-2 text-sm text-ink-muted">
              <span className="text-ink-faint">{t('status.bestAt')}</span>{' '}
              <span className="text-ink">{teamCaps.join(' · ')}</span>
            </p>
          )}
        </section>
      )}

      {alone || showIntro ? (
        <HowItWorks
          id="computers-intro"
          title={t('intro.title')}
          tone="bg-bifrost/15 text-bifrost"
          mascot={alone && !headerMascot}
          steps={[
            { icon: stepIcons.download, title: t('intro.steps.install.title'), body: t('intro.steps.install.body') },
            {
              icon: stepIcons.plug,
              title: t('intro.steps.connect.title'),
              body: t('intro.steps.connect.body', {
                available: t('nearby.title'),
                add: t('card.addToTeam'),
                command: t('join.open'),
              }),
            },
            { icon: stepIcons.spark, title: t('intro.steps.share.title'), body: t('intro.steps.share.body') },
          ]}
        />
      ) : null}

      {joinOpen ? <JoinByCommand onClose={() => setJoinOpen(false)} /> : null}

      {actionError && (
        <div className="rounded-lg bg-danger/10 px-4 py-3 text-sm text-danger">
          <p className="font-medium">{t('errors.action')}</p>
          <p className="mt-1">{errorMessage(actionError)}</p>
          {(pairMutation.error ||
            approveMutation.error ||
            claimMutation.error ||
            revokeMutation.error ||
            refreshMutation.error) && (
            <p className="mt-2 text-xs opacity-90">{t('errors.network')}</p>
          )}
        </div>
      )}

      {pairingSession && (
        <section className="card space-y-2 ring-1 ring-accent/40 animate-fade">
          <h2 className="font-display text-lg font-semibold text-ink">{t('pairing.waiting')}</h2>
          <p className="text-sm text-ink">
            <Trans
              t={t}
              i18nKey="pairing.pairingWith"
              values={{
                name: pairingSession.remote_name || pairingSession.remote_node_id || t('pairing.otherComputer'),
              }}
              components={{ name: <span className="font-medium" /> }}
            />
          </p>
          <p className="text-sm text-ink-muted">
            <Trans
              t={t}
              i18nKey="pairing.code"
              values={{ code: pairingSession.code || '—' }}
              components={{ code: <span className="font-mono text-lg font-semibold text-ink" /> }}
            />
          </p>
        </section>
      )}

      {pending.length > 0 && (
        <section className="card space-y-3 animate-fade">
          <h2 className="font-display text-lg font-semibold text-ink">{t('pairing.incoming')}</h2>
          <ul className="space-y-2">
            {pending.map((offer) => (
              <li
                key={offer.id}
                className="flex flex-wrap items-center justify-between gap-3 rounded-xl bg-raised/60 px-4 py-3"
              >
                <div>
                  <p className="font-medium text-ink">{offer.remote_name}</p>
                  <p className="text-xs text-ink-muted">
                    <Trans
                      t={t}
                      i18nKey="pairing.offerCode"
                      values={{ code: offer.code }}
                      components={{ code: <span className="font-mono" /> }}
                    />
                  </p>
                </div>
                <button
                  type="button"
                  className="btn-primary btn-sm"
                  disabled={approveMutation.isPending}
                  onClick={() => approveMutation.mutate(offer.id)}
                >
                  {t('pairing.approve')}
                </button>
              </li>
            ))}
          </ul>
        </section>
      )}

      {nodesQuery.isLoading && <Skeleton label={t('page.looking')} shape="cards" count={2} />}

      {nodesQuery.isError && !nodesQuery.data && (
        <LoadError error={nodesQuery.error} onRetry={() => void nodesQuery.refetch()} retrying={nodesQuery.isFetching} />
      )}
      {!nodesQuery.isLoading && !nodesQuery.isError && nodes.length === 0 && (
        <EmptyState
          title={t('page.emptyTitle')}
          description={t('page.emptyDescription')}
          action={
            <button
              type="button"
              className="btn-primary"
              disabled={refreshMutation.isPending}
              onClick={() => refreshMutation.mutate()}
            >
              {t('page.find')}
            </button>
          }
        />
      )}

      {nearby.length > 0 && (
        <section className="space-y-3">
          <div>
            <h2 className="section-title">{t('nearby.title')}</h2>
            <p className="mt-1 text-sm text-ink-muted">{t('nearby.description')}</p>
          </div>
          <ul className="grid gap-4 md:grid-cols-2">
            {nearby.map((node) => (
              <li key={node.id}>
                <ComputerCard
                  node={node}
                  runningCount={0}
                  installedModels={[]}
                  claimCode={claimCodes[node.id] ?? ''}
                  onClaimCode={(code) =>
                    setClaimCodes((prev) => ({ ...prev, [node.id]: code }))
                  }
                  onPair={() => pairMutation.mutate(node.id)}
                  onApproveCode={() =>
                    claimMutation.mutate({
                      nodeId: node.id,
                      code: claimCodes[node.id] ?? '',
                    })
                  }
                  onRevoke={() => revokeMutation.mutate(node.id)}
                  onRemoveModel={() => {}}
                  removingModelId={null}
                  pairingBusy={pairMutation.isPending}
                  claimBusy={claimMutation.isPending}
                  revokeBusy={revokeMutation.isPending}
                />
              </li>
            ))}
          </ul>
        </section>
      )}

      {fleet.length > 0 && (
        <section className="space-y-3">
          <div className="flex flex-wrap items-end justify-between gap-3">
            <div>
              <h2 className="section-title">{sectionTitle}</h2>
              <p className="mt-1 text-sm text-ink-muted">
                {pairedRemotes.length > 0
                  ? advancedMode
                    ? t('fleet.movesAdvanced')
                    : t('fleet.moves')
                  : t('fleet.alone')}
              </p>
            </div>
            <div className="flex flex-wrap gap-2">
              <Link to="/models" className="btn-secondary btn-sm">
                {t('fleet.installModels')}
              </Link>
              <Link to="/chat" className="btn-secondary btn-sm">
                {t('fleet.openChat')}
              </Link>
            </div>
          </div>
          <ul className="grid gap-4 md:grid-cols-2">
            {fleet.map((node) => (
              <li key={node.id}>
                <ComputerCard
                  node={node}
                  runningCount={runningByNode.get(node.id) ?? 0}
                  installedModels={modelsOnNode(models, node.id)}
                  claimCode={claimCodes[node.id] ?? ''}
                  onClaimCode={(code) =>
                    setClaimCodes((prev) => ({ ...prev, [node.id]: code }))
                  }
                  onPair={() => pairMutation.mutate(node.id)}
                  onApproveCode={() =>
                    claimMutation.mutate({
                      nodeId: node.id,
                      code: claimCodes[node.id] ?? '',
                    })
                  }
                  onRevoke={() => revokeMutation.mutate(node.id)}
                  onRemoveModel={(modelId) =>
                    removeMutation.mutate({ modelId, nodeId: node.id })
                  }
                  removingModelId={
                    removingKey?.startsWith(`${node.id}:`)
                      ? removingKey.slice(node.id.length + 1)
                      : null
                  }
                  pairingBusy={pairMutation.isPending}
                  claimBusy={claimMutation.isPending}
                  revokeBusy={revokeMutation.isPending}
                />
              </li>
            ))}
          </ul>
        </section>
      )}

      {fleet.length > 0 && (
        <DeployModelsPanel
          models={models}
          nodes={fleet}
          busy={deployMutation.isPending}
          onDeploy={(modelId, nodeIds) =>
            deployMutation.mutate({ modelId, nodeIds })
          }
        />
      )}

      <NetworkSettings />
    </div>
  )
}
