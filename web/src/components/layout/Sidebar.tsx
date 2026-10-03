import type { Ref } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { NavLink } from 'react-router-dom'
import { Rune } from '@/components/ui/Realm'
import { api } from '@/lib/api'
import { realms } from '@/lib/realms'
import { displayVersion } from '@/lib/appVersion'
import { useUIStore } from '@/stores/uiStore'
import { YggdrasilMark } from '@/components/ui/YggdrasilMark'
import { NotificationBell } from './NotificationBell'

// Labels are keys in the common namespace (i18n/locales/<language>/common.json).
const mainNav = [
  { to: '/chat', label: 'nav.chat' },
  { to: '/automations', label: 'nav.automations' },
  { to: '/models', label: 'nav.models' },
  { to: '/train', label: 'nav.train' },
  { to: '/knowledge', label: 'nav.knowledge' },
  { to: '/memory', label: 'nav.memory' },
  { to: '/nodes', label: 'nav.computers' },
] as const

const systemNav = [
  { to: '/performance', label: 'nav.performance' },
  { to: '/diagnostics', label: 'nav.diagnostics' },
  { to: '/profiles', label: 'nav.profiles', advanced: true },
  { to: '/tools', label: 'nav.tools', advanced: true },
  { to: '/api-access', label: 'nav.apiAccess', advanced: true },
] as const

/** A nav entry: the rune, then the label in the UI language. */
function NavItem({ to, label }: { to: string; label: string }) {
  const realm = realms[to]
  return (
    <NavLink
      to={to}
      title={realm ? `${label} · ${realm.norse}` : undefined}
      className={({ isActive }) =>
        ['nav-link', isActive ? 'nav-link-active' : ''].filter(Boolean).join(' ')
      }
    >
      {realm ? <Rune id={realm.rune} className="nav-rune h-4 w-2.5" /> : null}
      {label}
    </NavLink>
  )
}

/** open shows the sidebar as a drawer below the md breakpoint; wider, it is always shown. */
export function Sidebar({ open = false, ref }: { open?: boolean; ref?: Ref<HTMLDivElement> }) {
  const { t } = useTranslation()
  const advancedMode = useUIStore((s) => s.advancedMode)
  const healthQuery = useQuery({
    queryKey: ['health'],
    queryFn: async () => {
      const health = await api.getHealth()
      if (!health || health.status !== 'ok') {
        throw new Error('Service not ready')
      }
      return health
    },
    refetchInterval: (query) =>
      query.state.data?.status === 'ok' ? 15_000 : 1_000,
    retry: true,
    retryDelay: (attempt) => Math.min(400 + attempt * 250, 2000),
  })
  const nodesQuery = useQuery({
    queryKey: ['nodes'],
    queryFn: () => api.getNodes(),
    refetchInterval: 30_000,
    retry: false,
    enabled: healthQuery.data?.status === 'ok',
  })
  const modelsQuery = useQuery({
    queryKey: ['models'],
    queryFn: () => api.getModels(),
    refetchInterval: 60_000,
    retry: false,
    enabled: healthQuery.data?.status === 'ok',
  })

  const serviceOk = healthQuery.data?.status === 'ok'
  const healthPending =
    !serviceOk &&
    (healthQuery.isPending || healthQuery.isFetching || healthQuery.isLoading)
  const nodeCount = nodesQuery.data?.length ?? 0
  // "No model" only when the model list loaded and has none installed; a list
  // that is still loading or failed to load says nothing about models.
  const hasModel =
    !modelsQuery.isSuccess ||
    (modelsQuery.data ?? []).some((m) => m.installed || (m.installed_on?.length ?? 0) > 0)

  const runningVersion = displayVersion(healthQuery.data?.version)

  const statusLabel = !serviceOk
    ? healthPending
      ? t('status.starting')
      : t('status.unavailable')
    : !hasModel
      ? t('status.noModel')
      : t('status.ready')

  const statusTone = !serviceOk
    ? healthPending
      ? 'bg-info/15 text-info'
      : 'bg-warning/15 text-warning'
    : !hasModel
      ? 'bg-warning/15 text-warning'
      : 'bg-success/15 text-success'

  const statusDot = !serviceOk
    ? healthPending
      ? 'bg-info animate-pulse'
      : 'bg-warning animate-pulse'
    : !hasModel
      ? 'bg-warning'
      : 'bg-success'

  const systemItems = systemNav.filter(
    (item) => !('advanced' in item && item.advanced) || advancedMode,
  )

  return (
    // App chrome, not complementary content: the brand and status are the
    // page header, the links are the Main nav, and subsystem status is the footer.
    <div ref={ref} id="app-sidebar" data-open={open} className="app-sidebar flex h-full min-h-0 w-[15.5rem] shrink-0 flex-col overflow-hidden border-e border-line/60 bg-sidebar">
      <header className="px-4 pb-3 pt-4">
        <div className="flex items-center gap-2.5">
          <YggdrasilMark size={36} lore />
          <a href="/" className="brand flex min-w-0 flex-1 items-center gap-2.5 no-underline">
            <div className="min-w-0 flex-1 leading-none">
              <p className="truncate font-display text-xl font-semibold tracking-tight text-ink">
                Yggdrasil
              </p>
              {/* Status sits under the title so it never runs into the bell; a long
                  label truncates (the hint is in title) and the version wraps below. */}
              <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1">
                <span
                  className={['status-chip min-w-0 max-w-full', statusTone].join(' ')}
                  title={
                    !serviceOk
                      ? t('status.unavailableHint')
                      : !hasModel
                        ? t('status.noModelHint')
                        : t('status.readyHint')
                  }
                >
                  <span className={['h-1.5 w-1.5 shrink-0 rounded-full', statusDot].join(' ')} aria-hidden />
                  <span className="truncate">{statusLabel}</span>
                </span>
                <p
                  className="min-w-0 max-w-full truncate text-[11px] leading-none text-ink-faint"
                  title={runningVersion || t('tagline')}
                >
                  {runningVersion || t('tagline')}
                </p>
              </div>
            </div>
          </a>
          <NotificationBell />
        </div>
      </header>

      <nav className="flex flex-1 flex-col gap-4 overflow-y-auto px-2.5 pb-3" aria-label={t('nav.label')}>
        <div>
          <p className="label-caps mb-1 px-2.5">{t('nav.main')}</p>
          <div className="flex flex-col gap-0.5">
            {mainNav.map(({ to, label }) => (
              <NavItem key={to} to={to} label={t(label)} />
            ))}
          </div>
        </div>

        <div>
          <p className="label-caps mb-1 px-2.5">{t('nav.system')}</p>
          <div className="flex flex-col gap-0.5">
            {systemItems.map(({ to, label }) => (
              <NavItem key={to} to={to} label={t(label)} />
            ))}
            <NavItem to="/settings" label={t('nav.settings')} />
          </div>
        </div>
      </nav>

      {/* Plain labels; the realm behind each is in its tooltip. */}
      <footer className="mt-auto space-y-1.5 border-t border-line/50 px-4 py-3">
        <p className="label-caps mb-2 text-[10px] text-ink-faint">{t('subsystems.title')}</p>
        <div
          className="flex items-center justify-between gap-2 text-[11px] text-ink-faint"
          title={`Bifrost · ${t('subsystems.bifrostHint')}`}
        >
          <span className="flex items-center gap-1.5">
            <span className="h-1 w-1 rounded-full bg-bifrost" aria-hidden />
            {t('nav.computers')}
          </span>
          <span className="tabular-nums">
            {!nodesQuery.isSuccess ? '—' : nodeCount === 0 ? t('subsystems.computersNone') : t('subsystems.connected', { count: nodeCount })}
          </span>
        </div>
        <div
          className="flex items-center justify-between gap-2 text-[11px] text-ink-faint"
          title={t('subsystems.nornHint')}
        >
          <span className="flex items-center gap-1.5">
            <span className="h-1 w-1 rounded-full bg-norn" aria-hidden />
            {t('subsystems.placement')}
          </span>
          <span>{t('subsystems.nornAutomatic')}</span>
        </div>
        <div
          className="flex items-center justify-between gap-2 text-[11px] text-ink-faint"
          title={`Mimir · ${t('subsystems.mimirHint')}`}
        >
          <span className="flex items-center gap-1.5">
            <span className="h-1 w-1 rounded-full bg-mimir" aria-hidden />
            {t('nav.knowledge')}
          </span>
          <span>{t('subsystems.mimirReady')}</span>
        </div>
      </footer>
    </div>
  )
}
