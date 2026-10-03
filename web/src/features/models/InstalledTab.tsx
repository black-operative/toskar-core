import { useTranslation } from 'react-i18next'
import { useEffect, useState, type MouseEvent } from 'react'
import { Link } from 'react-router-dom'
import type {
  AIProfile,
  Model,
  ModelDownloadProgressPayload,
  ModelFit,
  Node,
  RunningModelView,
} from '@/types/api'
import { formatBytes } from '@/lib/format'
import { formatLastUsed, largerAlternative } from './modelPresentation'
import { SmallModelNote } from './SmallModelNote'
import { CommunityScore, RateButton } from './ratings'
import { formatPercent } from '@/i18n/format'
import { rovingKeyDown, useMenu } from '@/lib/roving'

export function InstalledTab({
  models,
  running,
  profiles,
  progress,
  nodes,
  showManualControls,
  search,
  tightModelIds,
  fits,
  onInstall,
  onStart,
  onStop,
  onDelete,
  onInstallElsewhere,
  installingId,
}: {
  models: Model[]
  running: RunningModelView[]
  profiles: AIProfile[]
  progress: Record<string, ModelDownloadProgressPayload>
  nodes: Node[]
  showManualControls: boolean
  search: string
  tightModelIds?: Set<string>
  /** Fit per model on this computer, to suggest a larger model. */
  fits?: Record<string, ModelFit>
  onInstall?: (id: string) => void
  onStart: (id: string) => void
  onStop: (id: string, instanceId: string) => void
  onDelete: (id: string) => void
  onInstallElsewhere?: (id: string) => void
  installingId?: string | null
}) {
  const { t } = useTranslation('models')
  const [menuOpenId, setMenuOpenId] = useState<string | null>(null)
  const menuRef = useMenu(menuOpenId, () => setMenuOpenId(null))
  const q = search.trim().toLowerCase()
  const installed = models.filter((m) => {
    const isInstalled =
      m.installed || (m.installed_on?.length ?? 0) > 0 || m.status === 'downloading'
    if (!isInstalled) return false
    if (!q) return true
    return (
      m.display_name.toLowerCase().includes(q) ||
      m.id.toLowerCase().includes(q)
    )
  })
  const runningByModel = new Map(running.map((r) => [r.model_id, r]))
  const alternative = largerAlternative(models, fits ?? {})
  const paired = nodes.filter((n) => (n.paired || n.is_local) && n.status !== 'offline')

  useEffect(() => {
    if (!menuOpenId) return
    const close = () => setMenuOpenId(null)
    window.addEventListener('click', close)
    return () => window.removeEventListener('click', close)
  }, [menuOpenId])

  if (installed.length === 0) {
    return (
      <p className="text-sm text-ink-muted">{t('installedTab.empty')}</p>
    )
  }

  return (
    <ul className="space-y-3">
      {installed.map((model) => {
        const live = runningByModel.get(model.id)
        const usedBy = profiles
          .filter((p) => p.roles?.some((r) => r.model_id === model.id))
          .map((p) => p.name)
        const dl = progress[model.id]
        const onNodes = model.installed_on ?? []
        const missing =
          paired.length > 1
            ? paired.filter((n) => !onNodes.some((p) => p.node_id === n.id))
            : []
        const where =
          onNodes.length > 0
            ? onNodes.map((p) => p.node_name).join(', ')
            : t('installedTab.thisComputer')

        return (
          <li key={model.id} className="card relative min-w-0">
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div className="min-w-0">
                <h3 className="font-display text-lg font-semibold text-ink">
                  {model.display_name}
                </h3>
                {tightModelIds?.has(model.id) ? (
                  <p
                    className="mt-1 text-xs text-ink-muted"
                    title={t('tightFit.hint')}
                  >
                    {t('tightFit.label')}
                  </p>
                ) : null}
                <p className="mt-1 text-sm text-ink-muted">
                  {live ? (
                    <span className="text-success">{t('installedTab.runningOn', { computer: live.node_name })}</span>
                  ) : dl ? (
                    <span>{t('installedTab.downloading', { percent: formatPercent(dl.percent / 100) })}</span>
                  ) : (
                    <span>
                      {t('installedTab.installedOn', { where })}
                      {model.size_bytes ? ` · ${formatBytes(model.size_bytes)}` : ''}
                    </span>
                  )}
                </p>
                <p className="mt-1 text-xs text-ink-faint">
                  {usedBy.length > 0
                    ? t('installedTab.usedBy', { profiles: usedBy.join(', ') })
                    : t('installedTab.lastUsed', { when: formatLastUsed(model.last_used_at) })}
                </p>
                <CommunityScore modelId={model.id} />
                <SmallModelNote model={model} alternative={alternative} onInstallAlternative={onInstall} />
              </div>

              <div className="flex flex-wrap items-center gap-2">
                {live ? (
                  <Link to="/chat" className="btn-primary px-3 py-1.5 text-xs">
                    {t('installedTab.chat')}
                  </Link>
                ) : (
                  showManualControls &&
                  !dl && (
                    <button
                      type="button"
                      className="btn-primary px-3 py-1.5 text-xs"
                      onClick={() => onStart(model.id)}
                    >
                      {t('installedTab.start')}
                    </button>
                  )
                )}
                {missing.length > 0 && onInstallElsewhere && (
                  <button
                    type="button"
                    className="btn-secondary px-3 py-1.5 text-xs"
                    disabled={installingId === model.id}
                    onClick={() => onInstallElsewhere(model.id)}
                  >
                    {installingId === model.id ? t('installedTab.installing') : t('installedTab.installElsewhere')}
                  </button>
                )}
                {!dl && <RateButton modelId={model.id} modelName={model.display_name} />}
                <div className="relative">
                  <button
                    type="button"
                    className="rounded-md px-2 py-1.5 text-xs text-ink-faint hover:bg-raised hover:text-ink"
                    aria-label={t('installedTab.moreActions', { model: model.display_name })}
                    aria-expanded={menuOpenId === model.id}
                    onClick={(e: MouseEvent) => {
                      e.stopPropagation()
                      setMenuOpenId((id) => (id === model.id ? null : model.id))
                    }}
                  >
                    ···
                  </button>
                  {menuOpenId === model.id && (
                    <div
                      className="absolute end-0 top-full z-20 mt-1 min-w-[9rem] rounded-lg border border-line bg-surface py-1 shadow-panel"
                      ref={menuRef}
                      role="menu"
                      onKeyDown={rovingKeyDown}
                      onClick={(e) => e.stopPropagation()}
                    >
                      {live && showManualControls && (
                        <button
                          type="button"
                          role="menuitem"
                          className="block w-full px-3 py-1.5 text-start text-sm text-ink hover:bg-raised"
                          onClick={() => {
                            setMenuOpenId(null)
                            onStop(model.id, live.instance_id)
                          }}
                        >
                          {t('installedTab.stop')}
                        </button>
                      )}
                      <button
                        type="button"
                        role="menuitem"
                        className="block w-full px-3 py-1.5 text-start text-sm text-danger hover:bg-danger/10"
                        onClick={() => {
                          setMenuOpenId(null)
                          onDelete(model.id)
                        }}
                      >
                        {t('installedTab.remove')}
                      </button>
                    </div>
                  )}
                </div>
              </div>
            </div>
          </li>
        )
      })}
    </ul>
  )
}
