import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate } from 'react-router-dom'
import { LoadingSpinner } from '@/components/ui/LoadingSpinner'
import { api } from '@/lib/api'
import { displayVersion } from '@/lib/appVersion'
import {
  isDesktopShell,
  notifyDesktopBackgroundMode,
  notifyDesktopLaunchAtLogin,
  openPathInOS,
} from '@/lib/desktopBridge'
import { useUIStore } from '@/stores/uiStore'
import type { SettingsPatch } from '@/types/api'
import { RealmKicker } from '@/components/ui/Realm'
import { NotificationDestinations } from './NotificationDestinations'
import { WhatLeft } from './WhatLeft'
import { YourData } from './YourData'
import { Toggle } from '@/components/ui/Toggle'
import { LanguageSettings } from './LanguageSettings'
import { Personalization } from './Personalization'
import { YggdrasilMark } from '@/components/ui/YggdrasilMark'
import { formatGigabytes } from '@/i18n/format'

function ChoiceGroup<T extends string>({
  value,
  options,
  onChange,
}: {
  value: T
  options: { id: T; label: string }[]
  onChange: (id: T) => void
}) {
  return (
    <div className="flex flex-wrap gap-2">
      {options.map((option) => {
        const active = value === option.id
        return (
          <button
            key={option.id}
            type="button"
            onClick={() => onChange(option.id)}
            className={[
              'rounded-lg px-4 py-2 text-sm font-medium transition',
              active
                ? 'bg-primary-soft text-primary-active'
                : 'bg-raised/70 text-ink-muted hover:text-ink',
            ].join(' ')}
          >
            {option.label}
          </button>
        )
      })}
    </div>
  )
}

function PathRow({
  label,
  path,
  openLabel,
}: {
  label: string
  path?: string
  openLabel: string
}) {
  const { t } = useTranslation('settings')
  const [status, setStatus] = useState<'idle' | 'opened' | 'failed'>('idle')
  return (
    <div className="min-w-0 space-y-1.5">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-sm font-medium text-ink">{label}</p>
        {path ? (
          <button
            type="button"
            className="text-xs text-primary hover:underline"
            onClick={async () => {
              const ok = await openPathInOS(path)
              setStatus(ok ? 'opened' : 'failed')
              setTimeout(() => setStatus('idle'), 2000)
            }}
          >
            {status === 'opened' ? t('storage.opened') : status === 'failed' ? t('storage.copyPath') : openLabel}
          </button>
        ) : null}
      </div>
      <p className="break-anywhere font-mono text-xs text-ink-muted" title={path}>
        {path ?? t('storage.pending')}
      </p>
    </div>
  )
}

function isMac(): boolean {
  const ua = typeof navigator !== 'undefined' ? navigator.userAgent : ''
  return /Mac/i.test(ua)
}

export function SettingsPage() {
  const { t } = useTranslation('settings')
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const advancedMode = useUIStore((s) => s.advancedMode)
  const setAdvancedMode = useUIStore((s) => s.setAdvancedMode)
  const theme = useUIStore((s) => s.theme)
  const setTheme = useUIStore((s) => s.setTheme)
  const resetToDefaults = useUIStore((s) => s.resetToDefaults)
  const [confirmReset, setConfirmReset] = useState(false)
  const [deleteModels, setDeleteModels] = useState(false)
  const [resetError, setResetError] = useState<string | null>(null)
  const folderLabel = isMac() ? t('storage.showInFinder') : t('storage.openFolder')

  const settingsQuery = useQuery({
    queryKey: ['settings'],
    queryFn: () => api.getSettings(),
    retry: false,
  })

  const versionQuery = useQuery({
    queryKey: ['version'],
    queryFn: () => api.getVersion(),
    retry: false,
  })

  const healthQuery = useQuery({
    queryKey: ['health'],
    queryFn: () => api.getHealth(),
    retry: false,
  })

  const profilesQuery = useQuery({
    queryKey: ['profiles'],
    queryFn: () => api.getProfiles(),
    retry: false,
  })
  const scheduleQuery = useQuery({
    queryKey: ['automations'],
    queryFn: () => api.listAutomations(),
    retry: false,
  })
  const hasSchedule = (scheduleQuery.data?.length ?? 0) > 0

  useEffect(() => {
    if (settingsQuery.data?.advanced_mode != null) {
      setAdvancedMode(settingsQuery.data.advanced_mode)
    }
  }, [settingsQuery.data?.advanced_mode, setAdvancedMode])

  const patchMutation = useMutation({
    mutationFn: (patch: SettingsPatch) => api.updateSettings(patch),
    onSuccess: (settings) => {
      if (settings?.advanced_mode != null) {
        setAdvancedMode(settings.advanced_mode)
      }
      queryClient.invalidateQueries({ queryKey: ['settings'] })
      queryClient.invalidateQueries({ queryKey: ['nodes'] })
      queryClient.invalidateQueries({ queryKey: ['ratings-community'] })
      queryClient.invalidateQueries({ queryKey: ['model-rating'] })
    },
  })

  const backgroundMutation = useMutation({
    mutationFn: async (enabled: boolean) => {
      const settings = await api.updateSettings({
        keep_running_in_background: enabled,
      })
      await notifyDesktopBackgroundMode(enabled)
      return settings
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['settings'] })
    },
  })

  const launchAtLoginMutation = useMutation({
    mutationFn: async (enabled: boolean) => {
      const settings = await api.updateSettings({ launch_at_login: enabled })
      await notifyDesktopLaunchAtLogin(enabled)
      return settings
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['settings'] })
    },
  })

  const resetMutation = useMutation({
    mutationFn: async () => {
      const settings = await api.resetApp({ delete_models: deleteModels })
      await notifyDesktopLaunchAtLogin(false)
      return settings
    },
    onSuccess: () => {
      resetToDefaults()
      queryClient.clear()
      navigate('/onboarding', { replace: true })
    },
    onError: (error) => {
      setConfirmReset(false)
      setResetError(
        error instanceof Error
          ? error.message
          : t('reset.failed'),
      )
    },
  })

  const clearHistoryMutation = useMutation({
    mutationFn: async () => {
      const conversations = (await api.getConversations()) ?? []
      for (const c of conversations) {
        await api.deleteConversation(c.id)
      }
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['conversations'] })
      queryClient.invalidateQueries({ queryKey: ['performance'] })
    },
  })

  const settings = settingsQuery.data
  const profiles = profilesQuery.data ?? []

  const patch = (body: SettingsPatch) => patchMutation.mutate(body)
  const busy = patchMutation.isPending || settingsQuery.isLoading

  return (
    <div className="mx-auto w-full max-w-2xl min-w-0 space-y-6">
      <header className="page-header">
        <RealmKicker />
        <h1 className="page-title">{t('page.title')}</h1>
        <p className="page-subtitle">{t('page.subtitle')}</p>
      </header>

      {(settingsQuery.isLoading || versionQuery.isLoading) && (
        <LoadingSpinner label={t('page.loading')} />
      )}

      <div className="settings-group">
        <p className="settings-group-label">{t('groups.general')}</p>
        <section className="card space-y-4">
          <div>
            <h2 className="section-title">{t('appearance.title')}</h2>
            <p className="mt-1 text-sm text-ink-muted">{t('appearance.description')}</p>
          </div>
          <ChoiceGroup
            value={theme}
            onChange={setTheme}
            options={[
              { id: 'dark', label: t('appearance.dark') },
              { id: 'light', label: t('appearance.light') },
              { id: 'system', label: t('appearance.system') },
            ]}
          />
        </section>

        <LanguageSettings />

        <Personalization />

        <section className="card space-y-4">
          <div className="flex items-start justify-between gap-4">
            <div>
              <h2 className="section-title">{t('background.title')}</h2>
              <p className="mt-1 text-sm text-ink-muted">{t('background.description')}</p>
            </div>
            <Toggle
              label={t('background.title')}
              checked={(settings?.keep_running_in_background ?? false) || hasSchedule}
              disabled={backgroundMutation.isPending || busy || hasSchedule}
              onChange={() =>
                backgroundMutation.mutate(!(settings?.keep_running_in_background ?? false))
              }
            />
          </div>
        </section>

        {isDesktopShell() && (
          <section className="card space-y-4">
            <div className="flex items-start justify-between gap-4">
              <div>
                <h2 className="section-title">{t('launchAtLogin.title')}</h2>
                <p className="mt-1 text-sm text-ink-muted">{t('launchAtLogin.description')}</p>
              </div>
              <Toggle
                label={t('launchAtLogin.title')}
                checked={settings?.launch_at_login ?? false}
                disabled={launchAtLoginMutation.isPending || busy}
                onChange={() =>
                  launchAtLoginMutation.mutate(!(settings?.launch_at_login ?? false))
                }
              />
            </div>
          </section>
        )}
      </div>

      <div className="settings-group">
        <p className="settings-group-label">{t('groups.ai')}</p>
        <section className="card space-y-4">
          <div>
            <h2 id="default-profile-title" className="section-title">{t('defaultProfile.title')}</h2>
            <p className="mt-1 text-sm text-ink-muted">{t('defaultProfile.description')}</p>
          </div>
          <select
            className="field w-full"
            aria-labelledby="default-profile-title"
            value={settings?.default_profile_id ?? ''}
            disabled={busy}
            onChange={(e) => patch({ default_profile_id: e.target.value })}
          >
            <option value="">{t('defaultProfile.automatic')}</option>
            {profiles.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
        </section>

        <section className="card space-y-4">
          <div>
            <h2 className="section-title">{t('runOn.title')}</h2>
            <p className="mt-1 text-sm text-ink-muted">{t('runOn.description')}</p>
          </div>
          <ChoiceGroup
            value={(settings?.default_execution ?? 'automatic') as 'automatic' | 'local' | 'ask'}
            onChange={(id) => patch({ default_execution: id })}
            options={[
              { id: 'automatic', label: t('runOn.automatic') },
              { id: 'local', label: t('runOn.local') },
              { id: 'ask', label: t('runOn.ask') },
            ]}
          />
        </section>

        <section className="card space-y-4">
          <div>
            <h2 className="section-title">{t('lifecycle.title')}</h2>
            <p className="mt-1 text-sm text-ink-muted">{t('lifecycle.description')}</p>
          </div>
          <ChoiceGroup
            value={(settings?.model_lifecycle ?? 'automatic') as 'automatic' | 'manual'}
            onChange={(id) => patch({ model_lifecycle: id })}
            options={[
              { id: 'automatic', label: t('lifecycle.automatic') },
              { id: 'manual', label: t('lifecycle.manual') },
            ]}
          />
          {(settings?.model_lifecycle ?? 'automatic') === 'automatic' && (
            <label className="block text-sm text-ink-muted">
              {t('lifecycle.idleUnload')}
              <p className="mt-0.5 text-xs text-ink-faint">{t('lifecycle.idleUnloadHint')}</p>
              <div className="mt-2 flex items-center gap-2">
                <input
                  type="number"
                  min={0}
                  max={240}
                  className="field max-w-[8rem]"
                  defaultValue={settings?.idle_unload_minutes ?? 15}
                  key={settings?.idle_unload_minutes ?? 15}
                  onBlur={(e) => {
                    const n = Number(e.target.value)
                    if (Number.isFinite(n) && n >= 0) {
                      patch({ idle_unload_minutes: Math.round(n) })
                    }
                  }}
                />
                <span className="text-xs text-ink-faint">{t('lifecycle.minutes')}</span>
              </div>
            </label>
          )}
        </section>

        <section className="card space-y-4">
          <div>
            <h2 className="section-title">{t('downloads.title')}</h2>
            <p className="mt-1 text-sm text-ink-muted">{t('downloads.description')}</p>
          </div>
          <ChoiceGroup
            value={(settings?.download_behavior ?? 'ask') as 'ask' | 'automatic'}
            onChange={(id) => patch({ download_behavior: id })}
            options={[
              { id: 'ask', label: t('downloads.ask') },
              { id: 'automatic', label: t('downloads.automatic') },
            ]}
          />
        </section>

        <section className="card space-y-4">
          <div className="flex items-start justify-between gap-4">
            <div>
              <h2 className="section-title">{t('advanced.title')}</h2>
              <p className="mt-1 text-sm text-ink-muted">{t('advanced.description')}</p>
            </div>
            <Toggle
              label={t('advanced.title')}
              checked={advancedMode}
              disabled={busy}
              onChange={() => {
                const next = !advancedMode
                setAdvancedMode(next)
                patch({ advanced_mode: next })
              }}
            />
          </div>
        </section>
      </div>

      <div className="settings-group">
        <p className="settings-group-label">{t('groups.notifications')}</p>
        <section className="card space-y-4">
          <div>
            <h2 className="section-title">{t('notifications.title')}</h2>
            <p className="mt-1 text-sm text-ink-muted">{t('notifications.description')}</p>
          </div>
          <div className="flex items-start justify-between gap-4">
            <div>
              <p className="text-sm font-medium text-ink">{t('notifications.taskFinish')}</p>
            </div>
            <Toggle
              label={t('notifications.taskFinish')}
              checked={settings?.notify_task_finish ?? true}
              disabled={busy}
              onChange={() =>
                patch({ notify_task_finish: !(settings?.notify_task_finish ?? true) })
              }
            />
          </div>
          <div className="flex items-start justify-between gap-4">
            <div>
              <p className="text-sm font-medium text-ink">{t('notifications.peerOffline')}</p>
            </div>
            <Toggle
              label={t('notifications.peerOffline')}
              checked={settings?.notify_peer_offline ?? true}
              disabled={busy}
              onChange={() =>
                patch({ notify_peer_offline: !(settings?.notify_peer_offline ?? true) })
              }
            />
          </div>
        </section>
        <NotificationDestinations />
      </div>

      <div className="settings-group">
        <p className="settings-group-label">{t('groups.privacy')}</p>
        <YourData />
        <WhatLeft />
        <section className="card space-y-4">
          <div>
            <h2 className="section-title">{t('toolPermissions.title')}</h2>
            <p className="mt-1 text-sm text-ink-muted">{t('toolPermissions.description')}</p>
          </div>
          {(
            [
              { key: 'tool_terminal' as const, label: t('toolPermissions.terminal') },
              { key: 'tool_file_writes' as const, label: t('toolPermissions.fileWrites') },
              { key: 'tool_git' as const, label: t('toolPermissions.git') },
            ] as const
          ).map((row) => {
            const value = (settings?.[row.key] ?? 'ask') as string
            return (
              <label key={row.key} className="block text-sm">
                <span className="text-ink-muted">{row.label}</span>
                <select
                  className="field mt-1 w-full"
                  value={value}
                  disabled={busy}
                  onChange={(e) => patch({ [row.key]: e.target.value } as SettingsPatch)}
                >
                  <option value="ask">{t('toolPermissions.ask')}</option>
                  <option value="allow-for-session">{t('toolPermissions.allowSession')}</option>
                  <option value="allow">{t('toolPermissions.allow')}</option>
                  <option value="deny">{t('toolPermissions.deny')}</option>
                </select>
              </label>
            )
          })}
        </section>

        <section className="card space-y-4">
          <div>
            <h2 className="section-title">{t('history.title')}</h2>
            <p className="mt-1 text-sm text-ink-muted">{t('history.description')}</p>
          </div>
          <div className="flex items-start justify-between gap-4">
            <p className="text-sm font-medium text-ink">{t('history.saveChats')}</p>
            <Toggle
              label={t('history.saveChats')}
              checked={settings?.save_chat_history ?? true}
              disabled={busy}
              onChange={() =>
                patch({ save_chat_history: !(settings?.save_chat_history ?? true) })
              }
            />
          </div>
          <div className="flex items-start justify-between gap-4">
            <p className="text-sm font-medium text-ink">{t('history.saveTasks')}</p>
            <Toggle
              label={t('history.saveTasks')}
              checked={settings?.save_task_history ?? true}
              disabled={busy}
              onChange={() =>
                patch({ save_task_history: !(settings?.save_task_history ?? true) })
              }
            />
          </div>
          <button
            type="button"
            className="btn-secondary px-3 py-1.5 text-xs"
            disabled={clearHistoryMutation.isPending}
            onClick={() => {
              if (window.confirm(t('history.confirmClear'))) {
                clearHistoryMutation.mutate()
              }
            }}
          >
            {clearHistoryMutation.isPending ? t('history.clearing') : t('history.clear')}
          </button>
        </section>

        <section className="card space-y-4">
          <div>
            <h2 className="section-title">{t('ratings.title')}</h2>
            <p className="mt-1 text-sm text-ink-muted">{t('ratings.description')}</p>
          </div>
          <div className="flex items-start justify-between gap-4">
            <div>
              <p className="text-sm font-medium text-ink">{t('ratings.show')}</p>
              <p className="mt-1 text-xs text-ink-muted">{t('ratings.showHint')}</p>
            </div>
            <Toggle
              label={t('ratings.show')}
              checked={settings?.community_ratings ?? false}
              disabled={busy}
              onChange={() => patch({ community_ratings: !(settings?.community_ratings ?? false) })}
            />
          </div>
          <div className="flex items-start justify-between gap-4">
            <div>
              <p className="text-sm font-medium text-ink">{t('ratings.ask')}</p>
              <p className="mt-1 text-xs text-ink-muted">{t('ratings.askHint')}</p>
            </div>
            <Toggle
              label={t('ratings.ask')}
              checked={settings?.ratings_prompts ?? true}
              disabled={busy}
              onChange={() => patch({ ratings_prompts: !(settings?.ratings_prompts ?? true) })}
            />
          </div>
        </section>
      </div>

      <div className="settings-group">
        <p className="settings-group-label">{t('groups.storage')}</p>
        <section className="card space-y-4">
          <h2 className="section-title">{t('storage.folders')}</h2>
          <div className="space-y-4">
            <PathRow label={t('storage.data')} path={settings?.data_dir} openLabel={folderLabel} />
            <PathRow label={t('storage.models')} path={settings?.models_dir} openLabel={folderLabel} />
            {advancedMode && (
              <>
                <PathRow
                  label={t('storage.runtimes')}
                  path={settings?.runtimes_dir}
                  openLabel={folderLabel}
                />
                <PathRow label={t('storage.logs')} path={settings?.logs_dir} openLabel={folderLabel} />
              </>
            )}
          </div>
        </section>

        <section className="card space-y-4">
          <div>
            <h2 className="section-title">{t('storage.limit')}</h2>
            <p className="mt-1 text-sm text-ink-muted">{t('storage.limitDescription')}</p>
          </div>
          <ChoiceGroup
            value={String(settings?.model_storage_limit_gb ?? 0)}
            onChange={(id) => patch({ model_storage_limit_gb: Number(id) })}
            options={[
              { id: '0', label: t('storage.unlimited') },
              { id: '50', label: formatGigabytes(50) },
              { id: '100', label: formatGigabytes(100) },
              { id: '250', label: formatGigabytes(250) },
            ]}
          />
        </section>
      </div>

      <div className="settings-group">
        <p className="settings-group-label">{t('groups.about')}</p>
        <section className="card space-y-4">
          <div className="flex items-center gap-4">
            <YggdrasilMark size={72} lore />
            <div className="min-w-0">
              <p className="font-display text-xl font-semibold tracking-tight text-ink">
                Toskar
              </p>
              <p className="mt-0.5 text-sm text-ink-muted">{t('about.tagline')}</p>
              <p className="mt-2 text-sm font-medium text-ink">
                {displayVersion(versionQuery.data?.version ?? healthQuery.data?.version) ||
                  t('about.versionUnavailable')}
              </p>
              {versionQuery.data?.commit &&
              versionQuery.data.commit !== 'unknown' &&
              versionQuery.data.commit.trim() !== '' ? (
                <p className="mt-0.5 font-mono text-xs text-ink-faint">
                  {t('about.build', { commit: versionQuery.data.commit.slice(0, 7) })}
                </p>
              ) : null}
              {versionQuery.data?.source ? (
                <p className="mt-2 text-sm">
                  <a
                    href={versionQuery.data.source}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="text-primary underline-offset-2 hover:underline"
                  >
                    {t('about.source')}
                  </a>
                  {versionQuery.data.license ? (
                    <span className="text-ink-muted"> · {versionQuery.data.license}</span>
                  ) : null}
                </p>
              ) : null}
            </div>
          </div>
          <dl className="space-y-3 border-t border-line/50 pt-4 text-sm">
            <div>
              <dt className="text-xs uppercase tracking-wide text-ink-faint">{t('about.thisComputer')}</dt>
              <dd className="font-medium text-ink">{settings?.node_name ?? t('about.unavailable')}</dd>
            </div>
            <div>
              <dt className="text-xs uppercase tracking-wide text-ink-faint">{t('about.service')}</dt>
              <dd className="font-medium capitalize text-ink">
                {healthQuery.data?.status ?? t('about.unavailable')}
              </dd>
            </div>
          </dl>
          <Link to="/diagnostics" className="btn-secondary inline-block">
            {t('about.diagnostics')}
          </Link>
        </section>
      </div>

      <div className="settings-group">
        <p className="settings-group-label">{t('groups.danger')}</p>
        <section className="card space-y-4">
          <div>
            <h2 className="section-title text-danger">{t('reset.title')}</h2>
            <p className="mt-1 text-sm text-ink-muted">{t('reset.description')}</p>
          </div>

          {resetError && (
            <div className="rounded-lg bg-danger/10 px-4 py-3 text-sm text-danger">
              {resetError}
            </div>
          )}

          {!confirmReset ? (
            <button
              type="button"
              className="btn-danger"
              onClick={() => {
                setResetError(null)
                setDeleteModels(false)
                setConfirmReset(true)
              }}
            >
              {t('reset.button')}
            </button>
          ) : (
            <div className="space-y-3 rounded-xl bg-danger/10 p-4">
              <p className="text-sm text-danger">{deleteModels ? t('reset.warningDelete') : t('reset.warningKeep')}</p>
              <label className="flex items-start gap-2 text-sm text-ink">
                <input
                  type="checkbox"
                  className="mt-0.5 rounded border-line"
                  checked={deleteModels}
                  onChange={(e) => setDeleteModels(e.target.checked)}
                />
                <span>{t('reset.deleteModels')}</span>
              </label>
              <div className="flex flex-wrap gap-2">
                <button
                  type="button"
                  className="rounded-lg bg-danger px-4 py-2 text-sm font-medium text-[#EEF2F6] transition hover:bg-danger/90 disabled:opacity-50"
                  disabled={resetMutation.isPending}
                  onClick={() => resetMutation.mutate()}
                >
                  {resetMutation.isPending ? t('reset.resetting') : t('reset.confirm')}
                </button>
                <button
                  type="button"
                  className="btn-secondary"
                  disabled={resetMutation.isPending}
                  onClick={() => setConfirmReset(false)}
                >
                  {t('reset.cancel')}
                </button>
              </div>
            </div>
          )}
        </section>
      </div>
    </div>
  )
}
