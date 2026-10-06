import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { formatSequence } from '@/i18n/format'
import { useEffect, useRef, useState, type MouseEvent, type ReactNode } from 'react'
import { Trans, useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { EmptyState } from '@/components/ui/EmptyState'
import { Toggle } from '@/components/ui/Toggle'
import { api } from '@/lib/api'
import { blankProfileTemplate, profileTemplateFromPurpose } from '@/lib/profilePresets'
import { useUIStore } from '@/stores/uiStore'
import type { AIProfile, ModelRole, NodePolicy, OrchestrationPolicy, ToolPolicy } from '@/types/api'
import { KnowledgePicker } from '@/features/knowledge/KnowledgePicker'
import { TOOL_CATALOG, defaultToolsFrom } from './editorTools'
import { CAPABILITIES, capabilityDescription, capabilityEnabled, capabilityLabel, setCapability } from './capabilities'
import {
  computerSelectionLabel,
  createStartOptions,
  filterProfiles,
  isBuiltInProfile,
  isTeamProfile,
  MODEL_ROLES,
  purposeIcon,
  purposeLabel,
  roleDisplayName,
  roleHelp,
  roleHint,
  sortProfilesForDisplay,
  STRATEGY_VALUES,
  strategyLabel,
  strategyOption,
  type StrategyValue,
  toolChipPreview,
  toolSummary,
  type CreateStartFrom,
  type ProfileFilter,
} from './profilePresentation'
import { OrchestrationControls, cleanOrchestration } from './OrchestrationControls'
import { EXECUTION_KEYS, MEMORY_KEYS, ORCHESTRATION_KEYS } from './orchestrationKeys'
import { RealmKicker } from '@/components/ui/Realm'
import { rovingKeyDown, useMenu } from '@/lib/roving'
import { LoadError } from '@/components/ui/LoadError'
import { Skeleton } from '@/components/ui/Skeleton'

// The permission choices; each is profiles:policies.<policy> in the catalog.
const POLICY_OPTIONS: ToolPolicy['policy'][] = ['deny', 'ask', 'allow-for-session', 'allow']

function PurposeGlyph({ purpose }: { purpose: string }) {
  const kind = purposeIcon(purpose)
  const common = 'h-4 w-4 shrink-0 text-primary'
  if (kind === 'coding') {
    return (
      <svg className={common} viewBox="0 0 16 16" fill="none" aria-hidden>
        <path
          d="M5.5 3.5 2 8l3.5 4.5M10.5 3.5 14 8l-3.5 4.5M9 3 7 13"
          stroke="currentColor"
          strokeWidth="1.4"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
    )
  }
  if (kind === 'research') {
    return (
      <svg className={common} viewBox="0 0 16 16" fill="none" aria-hidden>
        <path
          d="M8 2.5 13 8l-5 5.5L3 8l5-5.5Z"
          stroke="currentColor"
          strokeWidth="1.4"
          strokeLinejoin="round"
        />
      </svg>
    )
  }
  if (kind === 'custom') {
    return (
      <svg className={common} viewBox="0 0 16 16" fill="none" aria-hidden>
        <path
          d="M6.2 2.8h3.6l.4 1.6 1.5.9 1.6-.3 1.8 3.1-1.2 1.2v1.4l1.2 1.2-1.8 3.1-1.6-.3-1.5.9-.4 1.6H6.2l-.4-1.6-1.5-.9-1.6.3L1 10.9l1.2-1.2V8.3L1 7.1 2.8 4l1.6.3 1.5-.9.3-1.6Z"
          stroke="currentColor"
          strokeWidth="1.2"
          strokeLinejoin="round"
        />
        <circle cx="8" cy="8" r="1.6" stroke="currentColor" strokeWidth="1.2" />
      </svg>
    )
  }
  return (
    <svg className={common} viewBox="0 0 16 16" fill="none" aria-hidden>
      <circle cx="8" cy="8" r="5.2" stroke="currentColor" strokeWidth="1.4" />
      <circle cx="8" cy="8" r="1.6" fill="currentColor" />
    </svg>
  )
}

export function ProfilesPage() {
  const { t } = useTranslation('profiles')
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const advancedMode = useUIStore((s) => s.advancedMode)
  const setActiveProfileId = useUIStore((s) => s.setActiveProfileId)
  const [startFrom, setStartFrom] = useState<CreateStartFrom>('general')
  const [showCreate, setShowCreate] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [detailsId, setDetailsId] = useState<string | null>(null)
  const [filter, setFilter] = useState<ProfileFilter>('all')
  const [menuOpenId, setMenuOpenId] = useState<string | null>(null)
  const menuRef = useMenu(menuOpenId, () => setMenuOpenId(null))
  const [renamingId, setRenamingId] = useState<string | null>(null)
  const [renameValue, setRenameValue] = useState('')

  const profilesQuery = useQuery({
    queryKey: ['profiles'],
    queryFn: () => api.getProfiles(),
    retry: false,
  })

  const modelsQuery = useQuery({
    queryKey: ['models'],
    queryFn: () => api.getModels(),
    retry: false,
  })

  const nodesQuery = useQuery({
    queryKey: ['nodes'],
    queryFn: () => api.getNodes(),
    retry: false,
  })

  useEffect(() => {
    if (!menuOpenId) return
    const close = () => setMenuOpenId(null)
    window.addEventListener('click', close)
    return () => window.removeEventListener('click', close)
  }, [menuOpenId])

  const createMutation = useMutation({
    mutationFn: () => {
      if (startFrom === 'blank') {
        return api.createProfile(blankProfileTemplate())
      }
      return api.createProfile(profileTemplateFromPurpose(startFrom))
    },
    onSuccess: (created) => {
      queryClient.invalidateQueries({ queryKey: ['profiles'] })
      setShowCreate(false)
      if (created?.id) {
        setEditingId(created.id)
        setDetailsId(created.id)
      }
    },
  })

  const duplicateMutation = useMutation({
    mutationFn: (profile: AIProfile) =>
      api.createProfile({
        name: t('create.copyName', { name: profile.name }),
        purpose: profile.purpose,
        orchestrator_id: profile.orchestrator_id,
        node_policy: profile.node_policy,
        roles: (profile.roles ?? []).map((r) => ({ ...r })),
        tools: (profile.tools ?? []).map((t) => ({ ...t })),
        knowledge_sources: profile.knowledge_sources,
        orchestration: profile.orchestration,
      }),
    onSuccess: (created) => {
      queryClient.invalidateQueries({ queryKey: ['profiles'] })
      setMenuOpenId(null)
      if (created?.id) {
        setEditingId(created.id)
        setDetailsId(created.id)
      }
    },
  })

  const resetMutation = useMutation({
    mutationFn: (id: string) => api.resetProfile(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['profiles'] })
      setMenuOpenId(null)
      setEditingId(null)
    },
  })

  const updateMutation = useMutation({
    mutationFn: (profile: AIProfile) => api.updateProfile(profile.id, profile),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['profiles'] })
      setEditingId(null)
      setRenamingId(null)
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.deleteProfile(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['profiles'] })
      setMenuOpenId(null)
      setEditingId(null)
      setDetailsId(null)
    },
  })

  const allProfiles = sortProfilesForDisplay(profilesQuery.data ?? [])
  const profiles = filterProfiles(allProfiles, filter)
  const installedModels = (modelsQuery.data ?? []).filter(
    (m) => m.installed || (m.installed_on?.length ?? 0) > 0,
  )
  const pairedNodes = (nodesQuery.data ?? []).filter((n) => n.is_local || n.paired)
  const startOptions = createStartOptions()
  const selectedStartMeta =
    startOptions.find((option) => option.id === startFrom) ?? startOptions[0]

  const openProfileInChat = (profileId: string) => {
    setActiveProfileId(profileId)
    navigate(`/chat?profile=${encodeURIComponent(profileId)}&new=1`)
  }

  const beginRename = (profile: AIProfile) => {
    setMenuOpenId(null)
    setRenamingId(profile.id)
    setRenameValue(profile.name)
  }

  const commitRename = (profile: AIProfile) => {
    const next = renameValue.trim()
    if (!next || next === profile.name) {
      setRenamingId(null)
      return
    }
    updateMutation.mutate({ ...profile, name: next })
  }

  return (
    <div className="w-full min-w-0 space-y-6">
      <header className="page-header flex flex-wrap items-end justify-between gap-4">
        <div className="max-w-2xl">
          <RealmKicker />
          <h1 className="page-title">{t('page.title')}</h1>
          <p className="page-subtitle">{t('page.subtitle')}</p>
          <p className="mt-1 text-sm text-ink-muted">{t('page.hint')}</p>
        </div>
        <button
          type="button"
          className="btn-primary"
          onClick={() => setShowCreate(true)}
          title={t('page.createTitle')}
        >
          {t('page.create')}
        </button>
      </header>

      {showCreate && (
        <section className="card space-y-4">
          <div>
            <h2 className="font-display text-lg font-semibold text-ink">{t('create.title')}</h2>
            <p className="mt-1 text-sm text-ink-muted">{t('create.startFrom')}</p>
          </div>
          <div className="grid gap-2 sm:grid-cols-2">
            {startOptions.map((option) => (
              <button
                key={option.id}
                type="button"
                onClick={() => setStartFrom(option.id)}
                className={[
                  'selectable text-start',
                  startFrom === option.id ? 'selectable-active' : '',
                ]
                  .filter(Boolean)
                  .join(' ')}
              >
                <p className="font-semibold text-ink">{option.title}</p>
                <p className="mt-1 text-sm text-ink-muted">{option.description}</p>
              </button>
            ))}
          </div>
          <p className="text-xs text-ink-faint">
            <Trans
              t={t}
              i18nKey="create.startingWith"
              values={{ name: selectedStartMeta.title }}
              components={{ strong: <span className="font-medium text-ink" /> }}
            />
          </p>
          <div className="flex gap-2">
            <button
              type="button"
              className="btn-primary"
              disabled={createMutation.isPending}
              onClick={() => createMutation.mutate()}
            >
              {createMutation.isPending ? t('create.creating') : t('create.create')}
            </button>
            <button type="button" className="btn-secondary" onClick={() => setShowCreate(false)}>
              {t('create.cancel')}
            </button>
          </div>
        </section>
      )}

      {allProfiles.length > 0 && (
        <div className="segmented" role="tablist" aria-label={t('page.filter')} onKeyDown={rovingKeyDown}>
          {(
            [
              { id: 'all', label: t('page.filters.all') },
              { id: 'builtin', label: t('page.filters.builtin') },
              { id: 'custom', label: t('page.filters.custom') },
            ] as const
          ).map((tab) => (
            <button
              key={tab.id}
              type="button"
              role="tab"
              aria-selected={filter === tab.id}
              tabIndex={filter === tab.id ? 0 : -1}
              className="segmented-item"
              onClick={() => setFilter(tab.id)}
            >
              {tab.label}
            </button>
          ))}
        </div>
      )}

      {profilesQuery.isLoading && <Skeleton label={t('page.loading')} shape="cards" />}

      {profilesQuery.isError && !profilesQuery.data && (
        <LoadError error={profilesQuery.error} onRetry={() => void profilesQuery.refetch()} retrying={profilesQuery.isFetching} />
      )}
      {!profilesQuery.isLoading && !profilesQuery.isError && allProfiles.length === 0 && (
        <EmptyState
          title={t('page.emptyTitle')}
          description={t('page.emptyDescription')}
          action={
            <button type="button" className="btn-primary" onClick={() => setShowCreate(true)}>
              {t('page.create')}
            </button>
          }
        />
      )}

      {!profilesQuery.isLoading && allProfiles.length > 0 && profiles.length === 0 && (
        <p className="text-sm text-ink-muted">{t('page.noneInFilter')}</p>
      )}

      {profiles.length > 0 && (
        <ul className="grid gap-3 lg:grid-cols-2">
          {profiles.map((profile) => {
            const roles = profile.roles ?? []
            const nodeMode = profile.node_policy?.mode ?? 'automatic'
            const isEditing = editingId === profile.id
            const showDetails = detailsId === profile.id || isEditing
            const builtIn = isBuiltInProfile(profile.id)
            const orch = strategyLabel(profile, advancedMode)
            const computers = computerSelectionLabel(nodeMode)
            const tools = toolSummary(profile.tools)
            const chips = toolChipPreview(profile.tools)
            const isTeam = isTeamProfile(profile)
            const isRenaming = renamingId === profile.id

            return (
              <li
                key={profile.id}
                className={[
                  'card space-y-3 transition',
                  purposeAccentClass(profile.purpose),
                  // The editor needs the room: it takes the whole row.
                  isEditing ? 'lg:col-span-2' : '',
                ].join(' ')}
              >
                <div className="flex items-start gap-3">
                  <span className="mt-0.5 flex h-8 w-8 items-center justify-center rounded-lg bg-primary-soft/60">
                    <PurposeGlyph purpose={profile.purpose} />
                  </span>
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      {isRenaming ? (
                        <input
                          className="field max-w-full py-1 text-sm font-semibold"
                          value={renameValue}
                          autoFocus
                          onChange={(e) => setRenameValue(e.target.value)}
                          onBlur={() => commitRename(profile)}
                          onKeyDown={(e) => {
                            if (e.key === 'Enter') {
                              e.preventDefault()
                              commitRename(profile)
                            }
                            if (e.key === 'Escape') {
                              setRenamingId(null)
                            }
                          }}
                          aria-label={t('card.rename')}
                        />
                      ) : (
                        <h2 className="font-semibold text-ink">{profile.name}</h2>
                      )}
                      <span className="label-caps">
                        {builtIn ? t('card.builtin') : t('card.custom')}
                      </span>
                    </div>
                    <p className="mt-0.5 text-sm text-ink-muted">
                      {purposeLabel(profile.purpose)}
                    </p>
                  </div>
                </div>

                <div className="space-y-1.5 text-sm">
                  <p className="font-medium text-ink">{orch.name}</p>
                  <p className="text-ink-muted">
                    {nodeMode === 'prefer_local'
                      ? computers.title
                      : nodeMode === 'manual'
                        ? t('card.customSelection')
                        : t('card.automaticSelection')}
                  </p>
                  {tools.enabled > 0 ? (
                    <p className="text-ink-muted">
                      {t('card.tools', { count: tools.enabled })}
                      {tools.ask > 0 ? t('card.askFirst', { count: tools.ask }) : ''}
                    </p>
                  ) : (
                    <p className="text-ink-muted">{t('card.noTools')}</p>
                  )}
                  {chips.length > 0 && (
                    <div className="flex flex-wrap gap-1.5 pt-0.5">
                      {chips.map((chip) => (
                        <span
                          key={chip.label}
                          className={[
                            'rounded-md px-1.5 py-0.5 text-[11px] font-medium',
                            chip.tone === 'ok'
                              ? 'bg-raised text-ink'
                              : chip.tone === 'ask'
                                ? 'bg-warning/15 text-ink'
                                : 'bg-raised text-ink-faint line-through',
                          ].join(' ')}
                        >
                          {chip.label}
                        </span>
                      ))}
                      {(() => {
                        const previewed = 3
                        const remaining = Math.max(0, tools.enabled - previewed)
                        return remaining > 0 ? (
                          <span className="rounded-md px-1.5 py-0.5 text-[11px] text-ink-faint">
                            {t('card.more', { count: remaining })}
                          </span>
                        ) : null
                      })()}
                    </div>
                  )}
                </div>

                {roles.length > 0 && (
                  <div className="pt-0.5">
                    {isTeam && roles.length > 1 ? (
                      <p className="text-sm font-medium text-ink">
                        {formatSequence(roles.map((r) => roleDisplayName(r.role)))}
                      </p>
                    ) : (
                      <p className="text-sm font-medium capitalize text-ink">
                        {roleDisplayName(roles[0]?.role ?? 'assistant')}
                      </p>
                    )}
                    {showDetails && !isEditing && (
                      <ul className="mt-2 space-y-1.5">
                        {roles.map((r) => (
                          <li key={r.role} className="rounded-lg bg-raised/70 px-2.5 py-1.5">
                            <p className="text-xs font-medium capitalize text-ink">
                              {roleDisplayName(r.role)}
                            </p>
                            <p className="text-[11px] text-ink-muted">{roleHint(r)}</p>
                          </li>
                        ))}
                      </ul>
                    )}
                  </div>
                )}

                <div className="flex flex-wrap items-center gap-2 pt-1">
                  <button
                    type="button"
                    className="btn-primary btn-sm"
                    onClick={() => openProfileInChat(profile.id)}
                  >
                    {t('card.useInChat')}
                  </button>
                  <button
                    type="button"
                    className="btn-secondary btn-sm"
                    aria-expanded={advancedMode ? isEditing : showDetails}
                    onClick={() => {
                      if (advancedMode) {
                        setEditingId(isEditing ? null : profile.id)
                        setDetailsId(profile.id)
                      } else {
                        setDetailsId(showDetails && !isEditing ? null : profile.id)
                        setEditingId(null)
                      }
                    }}
                  >
                    {advancedMode
                      ? isEditing
                        ? t('card.close')
                        : t('card.edit')
                      : showDetails
                        ? t('card.hideDetails')
                        : t('card.details')}
                  </button>
                  <div className="relative ms-auto">
                    <button
                      type="button"
                      className="icon-button"
                      aria-label={t('card.moreActions', { name: profile.name })}
                      aria-haspopup="menu"
                      aria-expanded={menuOpenId === profile.id}
                      onClick={(e: MouseEvent) => {
                        e.stopPropagation()
                        setMenuOpenId((id) => (id === profile.id ? null : profile.id))
                      }}
                    >
                      <svg viewBox="0 0 16 16" className="h-4 w-4" fill="currentColor" aria-hidden>
                        <circle cx="3.5" cy="8" r="1.2" />
                        <circle cx="8" cy="8" r="1.2" />
                        <circle cx="12.5" cy="8" r="1.2" />
                      </svg>
                    </button>
                    {menuOpenId === profile.id && (
                      <div
                        className="menu min-w-[9.5rem]"
                        ref={menuRef}
                        role="menu"
                        onKeyDown={rovingKeyDown}
                        onClick={(e) => e.stopPropagation()}
                      >
                        <MenuItem
                          onClick={() => {
                            setMenuOpenId(null)
                            duplicateMutation.mutate(profile)
                          }}
                          disabled={duplicateMutation.isPending}
                        >
                          {t('menu.duplicate')}
                        </MenuItem>
                        <MenuItem onClick={() => beginRename(profile)}>{t('menu.rename')}</MenuItem>
                        {builtIn && (
                          <MenuItem
                            disabled={resetMutation.isPending}
                            onClick={() => {
                              if (window.confirm(t('menu.confirmReset', { name: profile.name }))) {
                                resetMutation.mutate(profile.id)
                              } else {
                                setMenuOpenId(null)
                              }
                            }}
                          >
                            {t('menu.reset')}
                          </MenuItem>
                        )}
                        {!builtIn && (
                          <MenuItem
                            danger
                            disabled={deleteMutation.isPending}
                            onClick={() => {
                              if (window.confirm(t('menu.confirmDelete', { name: profile.name }))) {
                                deleteMutation.mutate(profile.id)
                              } else {
                                setMenuOpenId(null)
                              }
                            }}
                          >
                            {t('menu.delete')}
                          </MenuItem>
                        )}
                      </div>
                    )}
                  </div>
                </div>

                {showDetails && !isEditing && (
                  <div className="space-y-3 border-t border-line pt-3 text-sm">
                    <DetailBlock label={t('card.howItWorks')} body={orch.detail} />
                    <DetailBlock
                      label={t('card.computerSelection')}
                      body={t('card.selectionDetail', { title: computers.title, detail: computers.detail })}
                    />
                    {advancedMode && (
                      <p className="text-xs text-ink-faint">
                        <Trans
                          t={t}
                          i18nKey="card.strategy"
                          values={{
                            strategy: strategyOption(
                              (isTeam ? 'team' : (profile.orchestration?.strategy ?? '')) as StrategyValue,
                            ).label,
                          }}
                          components={{ strong: <span className="font-medium text-ink" /> }}
                        />
                      </p>
                    )}
                  </div>
                )}

                {advancedMode && isEditing && (
                  <AdvancedEditor
                    profile={profile}
                    installedModels={installedModels.map((m) => ({
                      id: m.id,
                      name: m.display_name || m.id,
                    }))}
                    nodes={pairedNodes.map((n) => ({
                      id: n.id,
                      name: n.is_local ? t('card.thisMachine', { name: n.name }) : n.name,
                    }))}
                    saving={updateMutation.isPending}
                    onSave={(next) => updateMutation.mutate(next)}
                    onCancel={() => setEditingId(null)}
                  />
                )}
              </li>
            )
          })}
        </ul>
      )}
    </div>
  )
}

function purposeAccentClass(purpose: string): string {
  switch (purpose) {
    case 'coding':
      return 'border-s-[3px] border-s-primary/50'
    case 'research':
      return 'border-s-[3px] border-s-mimir/45'
    case 'custom':
      return 'border-s-[3px] border-s-ink-faint/40'
    default:
      return 'border-s-[3px] border-s-accent/40'
  }
}

function DetailBlock({ label, body }: { label: string; body: string }) {
  return (
    <div>
      <p className="text-xs font-medium uppercase tracking-wide text-ink-faint">{label}</p>
      <p className="mt-1 text-sm leading-relaxed text-ink-muted">{body}</p>
    </div>
  )
}

function MenuItem({
  children,
  onClick,
  disabled,
  danger,
}: {
  children: ReactNode
  onClick: () => void
  disabled?: boolean
  danger?: boolean
}) {
  return (
    <button
      type="button"
      role="menuitem"
      disabled={disabled}
      className={['menu-item disabled:opacity-50', danger ? 'menu-item-danger' : ''].join(' ')}
      onClick={onClick}
    >
      {children}
    </button>
  )
}

function AdvancedEditor({
  profile,
  installedModels,
  nodes,
  saving,
  onSave,
  onCancel,
}: {
  profile: AIProfile
  installedModels: { id: string; name: string }[]
  nodes: { id: string; name: string }[]
  saving: boolean
  onSave: (profile: AIProfile) => void
  onCancel: () => void
}) {
  const { t } = useTranslation('profiles')
  const [name, setName] = useState(profile.name)
  const [nodeMode, setNodeMode] = useState(profile.node_policy?.mode ?? 'automatic')
  const [placement, setPlacement] = useState(placementFrom(profile))
  const [roles, setRoles] = useState<ModelRole[]>(editorRoles(profile))
  const [tools, setTools] = useState<ToolPolicy[]>(defaultToolsFrom(profile))
  const [knowledge, setKnowledge] = useState<string[]>(profile.knowledge_sources ?? [])
  const [orchestration, setOrchestration] = useState<OrchestrationPolicy>(editorOrchestration(profile))
  const advancedMode = useUIStore((s) => s.advancedMode)
  const profileIdRef = useRef(profile.id)

  useEffect(() => {
    if (profileIdRef.current === profile.id) return
    profileIdRef.current = profile.id
    setName(profile.name)
    setNodeMode(profile.node_policy?.mode ?? 'automatic')
    setPlacement(placementFrom(profile))
    setRoles(editorRoles(profile))
    setTools(defaultToolsFrom(profile))
    setKnowledge(profile.knowledge_sources ?? [])
    setOrchestration(editorOrchestration(profile))
  }, [profile])

  const updateRole = (index: number, patch: Partial<ModelRole>) => {
    setRoles((prev) => prev.map((r, i) => (i === index ? { ...r, ...patch } : r)))
  }

  const updateToolPolicy = (toolId: string, policy: ToolPolicy['policy']) => {
    setTools((prev) =>
      prev.map((t) => (t.tool_id === toolId ? { ...t, policy } : t)),
    )
  }

  const strategy = (orchestration.strategy ?? '') as StrategyValue
  const orchHint = strategyOption(STRATEGY_VALUES.includes(strategy) ? strategy : '').detail
  const fallbacks = orchestration.fallback_models ?? []
  const setFallback = (index: number, id: string) => {
    const next = [...fallbacks]
    next[index] = id
    setOrchestration({ ...orchestration, fallback_models: next.filter(Boolean) })
  }

  const nodeHint = computerSelectionLabel(nodeMode).detail
  const [tab, setTab] = useState<EditorTab>('models')
  const [allRoles, setAllRoles] = useState(false)
  // The roles in use (the primary one, and any with a model or a computer);
  // the rest wait behind "Show all roles" so a simple profile stays short.
  const inUse = (r: ModelRole) => r.role === 'assistant' || Boolean(r.model_id) || Boolean(r.node_id)
  const hiddenRoles = roles.filter((r) => !inUse(r)).length
  const tabID = (id: EditorTab) => `profile-${profile.id}-tab-${id}`
  const panelID = (id: EditorTab) => `profile-${profile.id}-panel-${id}`

  return (
    <div className="space-y-5 border-t border-line pt-4">
      <label className="block max-w-md text-sm text-ink-muted">
        {t('editor.name')}
        <input
          className="field mt-1 w-full"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder={t('editor.namePlaceholder')}
        />
      </label>

      <div className="segmented" role="tablist" aria-label={t('editor.tabs.label')} onKeyDown={rovingKeyDown}>
        {EDITOR_TABS.map((id) => (
          <button
            key={id}
            id={tabID(id)}
            type="button"
            role="tab"
            aria-selected={tab === id}
            aria-controls={panelID(id)}
            tabIndex={tab === id ? 0 : -1}
            className="segmented-item"
            onClick={() => setTab(id)}
          >
            {t(`editor.tabs.${id}`)}
          </button>
        ))}
      </div>

      {tab === 'models' && (
        <section id={panelID('models')} role="tabpanel" aria-labelledby={tabID('models')} className="space-y-3">
          <p className="text-sm text-ink-muted">{t('editor.modelsHint')}</p>
          <div className="grid gap-3 xl:grid-cols-2">
            {roles.map((role, index) =>
              allRoles || inUse(role) ? (
                <div key={`${profile.id}-${role.role}-${index}`} className="space-y-2 rounded-lg bg-raised/60 p-3">
                  <div>
                    <p className="text-sm font-semibold text-ink">{roleDisplayName(role.role)}</p>
                    <p className="text-xs leading-relaxed text-ink-muted">{roleHelp(role.role)}</p>
                  </div>
                  <div className="grid gap-2 sm:grid-cols-2">
                    <label className="text-xs text-ink-muted">
                      {t('editor.model')}
                      <select
                        className="field mt-1 w-full py-1.5 text-sm"
                        value={role.model_id}
                        onChange={(e) => updateRole(index, { model_id: e.target.value })}
                      >
                        <option value="">{t('editor.automatic')}</option>
                        {installedModels.map((m) => (
                          <option key={m.id} value={m.id}>
                            {m.name || m.id}
                          </option>
                        ))}
                        {role.model_id && !installedModels.some((m) => m.id === role.model_id) && (
                          <option value={role.model_id}>{role.model_id}</option>
                        )}
                      </select>
                    </label>
                    <label className="text-xs text-ink-muted">
                      {t('editor.computer')}
                      <select
                        className="field mt-1 w-full py-1.5 text-sm"
                        value={role.node_id ?? ''}
                        onChange={(e) => updateRole(index, { node_id: e.target.value || undefined })}
                      >
                        <option value="">{t('editor.automatic')}</option>
                        {nodes.map((n) => (
                          <option key={n.id} value={n.id}>
                            {n.name}
                          </option>
                        ))}
                      </select>
                    </label>
                  </div>
                </div>
              ) : null,
            )}
          </div>
          {hiddenRoles > 0 || allRoles ? (
            <button type="button" className="btn-secondary btn-sm" aria-expanded={allRoles} onClick={() => setAllRoles((v) => !v)}>
              {allRoles ? t('editor.fewerRoles') : t('editor.moreRoles', { count: hiddenRoles })}
            </button>
          ) : null}
          <div className="space-y-2 rounded-lg border border-line/60 p-3">
            <p className="text-sm font-semibold text-ink">{t('editor.fallback')}</p>
            <p className="text-xs leading-relaxed text-ink-muted">{t('editor.fallbackHint')}</p>
            <div className="grid gap-2 sm:grid-cols-3">
              {[0, 1, 2].map((i) => (
                <label key={i} className="text-xs text-ink-muted">
                  {i + 1}.
                  <select
                    className="field mt-1 w-full py-1.5 text-sm"
                    value={fallbacks[i] ?? ''}
                    disabled={i > fallbacks.length}
                    onChange={(e) => setFallback(i, e.target.value)}
                  >
                    <option value="">{t('editor.none')}</option>
                    {installedModels.map((m) => (
                      <option key={m.id} value={m.id}>
                        {m.name || m.id}
                      </option>
                    ))}
                  </select>
                </label>
              ))}
            </div>
          </div>
        </section>
      )}

      {tab === 'tools' && (
        <section id={panelID('tools')} role="tabpanel" aria-labelledby={tabID('tools')} className="space-y-3">
          <ul className="grid gap-2 xl:grid-cols-2">
            {CAPABILITIES.map((capability) => {
              const on = capabilityEnabled(tools, capability.id)
              return (
                <li key={capability.id} className="flex items-start justify-between gap-3 rounded-lg bg-raised/60 p-3">
                  <div className="min-w-0">
                    <p className="text-sm font-semibold text-ink">{capabilityLabel(capability.id)}</p>
                    <p className="mt-0.5 text-xs leading-relaxed text-ink-muted">{capabilityDescription(capability.id)}</p>
                  </div>
                  <Toggle
                    checked={on}
                    label={capabilityLabel(capability.id)}
                    onChange={() => setTools((current) => setCapability(current, capability.id, !on))}
                  />
                </li>
              )
            })}
          </ul>
          {advancedMode && (
            <details className="rounded-lg border border-line/60 p-3">
              <summary className="cursor-pointer text-sm font-semibold text-ink">{t('editor.individualTools')}</summary>
              <ul className="mt-3 grid gap-2 xl:grid-cols-2">
                {TOOL_CATALOG.map((id) => {
                  const policy = tools.find((tool) => tool.tool_id === id)?.policy ?? 'ask'
                  const policyKey = POLICY_OPTIONS.includes(policy) ? policy : 'ask'
                  return (
                    <li key={id} className="flex flex-wrap items-start justify-between gap-3 rounded-lg bg-raised/60 p-3">
                      <div className="min-w-0 flex-1">
                        <p className="text-sm font-medium text-ink">{t(`tools.${id}.label`)}</p>
                        <p className="mt-0.5 text-xs leading-relaxed text-ink-muted">{t(`tools.${id}.description`)}</p>
                      </div>
                      <select
                        className="field max-w-[200px] py-1 text-sm"
                        value={policy}
                        aria-label={t(`tools.${id}.label`)}
                        title={t(`policies.${policyKey}.description`)}
                        onChange={(e) => updateToolPolicy(id, e.target.value as ToolPolicy['policy'])}
                      >
                        {POLICY_OPTIONS.map((opt) => (
                          <option key={opt} value={opt} title={t(`policies.${opt}.description`)}>
                            {t(`policies.${opt}.label`)}
                          </option>
                        ))}
                      </select>
                    </li>
                  )
                })}
              </ul>
            </details>
          )}
        </section>
      )}

      {tab === 'knowledge' && (
        <section id={panelID('knowledge')} role="tabpanel" aria-labelledby={tabID('knowledge')} className="max-w-2xl space-y-3">
          <p className="text-sm text-ink-muted">{t('editor.memoryHint')}</p>
          <KnowledgePicker selected={knowledge} onChange={setKnowledge} disabled={saving} />
          {advancedMode && (
            <OrchestrationControls value={orchestration} onChange={setOrchestration} disabled={saving} only={MEMORY_KEYS} />
          )}
        </section>
      )}

      {tab === 'behavior' && (
        <section id={panelID('behavior')} role="tabpanel" aria-labelledby={tabID('behavior')} className="grid gap-5 lg:grid-cols-2">
          <div className="space-y-2">
            <h3 className="label-caps">{t('editor.orchestration')}</h3>
            <label className="block text-xs text-ink-muted">
              {t('editor.strategy')}
              <select
                className="field mt-1 w-full py-1.5 text-sm"
                value={strategy}
                onChange={(e) =>
                  setOrchestration({ ...orchestration, strategy: e.target.value as OrchestrationPolicy['strategy'] })
                }
              >
                {STRATEGY_VALUES.map((value) => (
                  <option key={value} value={value}>
                    {strategyOption(value).label}
                  </option>
                ))}
              </select>
            </label>
            <p className="text-xs leading-relaxed text-ink-muted">{orchHint}</p>
            {advancedMode && (
              <OrchestrationControls value={orchestration} onChange={setOrchestration} disabled={saving} only={ORCHESTRATION_KEYS} />
            )}
          </div>
          <div className="space-y-2">
            <h3 className="label-caps">{t('editor.execution')}</h3>
            <label className="block text-xs text-ink-muted">
              {t('editor.computerSelection')}
              <select
                className="field mt-1 w-full py-1.5 text-sm"
                value={nodeMode}
                onChange={(e) => setNodeMode(e.target.value as AIProfile['node_policy']['mode'])}
              >
                <option value="automatic">{t('editor.selectionOptions.automatic')}</option>
                <option value="prefer_local">{t('editor.selectionOptions.prefer_local')}</option>
                <option value="manual">{t('editor.selectionOptions.manual')}</option>
              </select>
            </label>
            <p className="text-xs leading-relaxed text-ink-muted">{nodeHint}</p>
            <label className="flex items-center gap-2 text-xs text-ink-muted">
              <input
                type="checkbox"
                checked={placement.remote === 'off'}
                onChange={(e) => setPlacement({ ...placement, remote: e.target.checked ? 'off' : '' })}
              />
              {t('editor.localOnly')}
            </label>
            {placement.remote !== 'off' && nodes.length > 0 && (
              <div className="space-y-1.5">
                {nodes.map((n) => (
                  <label key={n.id} className="flex items-center justify-between gap-3 text-xs text-ink-muted">
                    <span className="truncate text-ink">{n.name}</span>
                    <select
                      className="field max-w-[160px] py-1 text-xs"
                      value={placement.preferred.includes(n.id) ? 'preferred' : placement.denied.includes(n.id) ? 'denied' : ''}
                      onChange={(e) => setPlacement(withPlacement(placement, n.id, e.target.value))}
                    >
                      <option value="">{t('editor.allowed')}</option>
                      <option value="preferred">{t('editor.preferred')}</option>
                      <option value="denied">{t('editor.never')}</option>
                    </select>
                  </label>
                ))}
              </div>
            )}
            {advancedMode && (
              <OrchestrationControls value={orchestration} onChange={setOrchestration} disabled={saving} only={EXECUTION_KEYS} />
            )}
          </div>
        </section>
      )}

      <div className="flex flex-wrap items-center gap-3 border-t border-line/60 pt-4">
        <button
          type="button"
          className="btn-primary"
          disabled={saving || !name.trim()}
          onClick={() =>
            onSave({
              ...profile,
              name: name.trim(),
              orchestrator_id: 'simple',
              node_policy: nodePolicyFrom(nodeMode, placement),
              roles: savedRoles(roles),
              tools,
              knowledge_sources: knowledge,
              orchestration: cleanOrchestration(orchestration),
            })
          }
        >
          {t('editor.save')}
        </button>
        <button type="button" className="btn-secondary" onClick={onCancel}>
          {t('editor.cancel')}
        </button>
      </div>
    </div>
  )
}

type EditorTab = 'models' | 'tools' | 'knowledge' | 'behavior'
const EDITOR_TABS: EditorTab[] = ['models', 'tools', 'knowledge', 'behavior']

/** One row per model role (spec §20), filled from the profile; older role names map onto the current ones. */
function editorRoles(profile: AIProfile): ModelRole[] {
  const legacy: Record<string, string> = { coordinator: 'planner', researcher: 'assistant' }
  const byRole = new Map<string, ModelRole>()
  for (const r of profile.roles ?? []) {
    const role = legacy[r.role] ?? r.role
    if (!byRole.has(role)) byRole.set(role, { ...r, role })
  }
  return MODEL_ROLES.map(({ role }) => byRole.get(role) ?? { role, model_id: '', required: false })
}

/** Keeps the primary role, and any other role with a model or a computer. */
function savedRoles(roles: ModelRole[]): ModelRole[] {
  return roles.filter((r) => r.role === 'assistant' || r.model_id || r.node_id)
}

/** A profile from an older version that used the Team orchestrator shows as the Team strategy. */
function editorOrchestration(profile: AIProfile): OrchestrationPolicy {
  const o = { ...(profile.orchestration ?? {}) }
  if (profile.orchestrator_id === 'team' && !o.strategy) o.strategy = 'team'
  return o
}

type Placement = { preferred: string[]; denied: string[]; remote: '' | 'off' }

function placementFrom(profile: AIProfile): Placement {
  const p = profile.node_policy ?? { mode: 'automatic' }
  return { preferred: p.preferred_nodes ?? [], denied: p.denied_nodes ?? [], remote: p.remote ?? '' }
}

/** Sets one computer to allowed, preferred, or never used. */
function withPlacement(p: Placement, id: string, choice: string): Placement {
  const preferred = p.preferred.filter((x) => x !== id)
  const denied = p.denied.filter((x) => x !== id)
  if (choice === 'preferred') preferred.push(id)
  if (choice === 'denied') denied.push(id)
  return { ...p, preferred, denied }
}

function nodePolicyFrom(mode: NodePolicy['mode'], p: Placement): NodePolicy {
  const out: NodePolicy = { mode }
  if (p.remote === 'off') {
    out.remote = 'off'
    return out
  }
  if (p.preferred.length) out.preferred_nodes = p.preferred
  if (p.denied.length) out.denied_nodes = p.denied
  return out
}
