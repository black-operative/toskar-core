import { useEffect, useState } from 'react'
import { Outlet, useLocation } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { PageErrorBoundary } from '@/components/layout/PageErrorBoundary'
import { Sidebar } from '@/components/layout/Sidebar'
import { YggdrasilMark } from '@/components/ui/YggdrasilMark'
import { useDialog } from '@/lib/useDialog'

// Each page's name in the window title: common:nav.<key>.
const pageTitles: Record<string, string> = {
  '/chat': 'nav.chat',
  '/automations': 'nav.automations',
  '/models': 'nav.models',
  '/train': 'nav.train',
  '/knowledge': 'nav.knowledge',
  '/memory': 'nav.memory',
  '/nodes': 'nav.computers',
  '/performance': 'nav.performance',
  '/diagnostics': 'nav.diagnostics',
  '/profiles': 'nav.profiles',
  '/tools': 'nav.tools',
  '/api-access': 'nav.apiAccess',
  '/settings': 'nav.settings',
}

/**
 * App chrome: sidebar + main page region.
 * Overflow is owned here — pages scroll inside `.page-scroll`, not the OS window.
 * Below md the sidebar is a drawer opened from a top bar (see .app-sidebar).
 */
export function AppLayout() {
  const { t } = useTranslation()
  const { pathname } = useLocation()
  const titleKey = pageTitles[pathname]
  const [menuOpen, setMenuOpen] = useState(false)
  // The open drawer is modal: focus starts on the current page's link and
  // stays in the drawer, Escape closes it, and focus returns to the button.
  const drawerRef = useDialog<HTMLDivElement>(menuOpen, () => setMenuOpen(false), {
    initialFocus: 'nav a[aria-current="page"]',
  })

  // Each page names itself in the window title, for history, tabs, and screen readers.
  useEffect(() => {
    document.title = titleKey ? `${t(titleKey)} · Toskar` : 'Toskar'
  }, [titleKey, t])

  // Choosing a page closes the drawer.
  useEffect(() => setMenuOpen(false), [pathname])

  return (
    <div className="app-shell">
      <Sidebar ref={drawerRef} open={menuOpen} />
      {menuOpen ? (
        <div className="fixed inset-0 z-30 bg-black/40 md:hidden" aria-hidden onClick={() => setMenuOpen(false)} />
      ) : null}
      <main inert={menuOpen} className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
        <div className="flex items-center gap-2 border-b border-line/60 bg-sidebar px-2 py-1.5 md:hidden">
          <button
            type="button"
            className="inline-flex h-11 w-11 items-center justify-center rounded-lg text-ink hover:bg-raised/60"
            aria-label={t('nav.open')}
            aria-controls="app-sidebar"
            aria-expanded={menuOpen}
            onClick={() => setMenuOpen((open) => !open)}
          >
            <svg viewBox="0 0 20 20" className="h-5 w-5" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" aria-hidden>
              <path d="M3 5h14M3 10h14M3 15h14" />
            </svg>
          </button>
          <YggdrasilMark size={24} />
          <span className="truncate font-display text-base font-semibold text-ink">{titleKey ? t(titleKey) : 'Toskar'}</span>
        </div>
        <div className="page-scroll px-page-x py-5 sm:py-6">
          <PageErrorBoundary key={pathname}>
            <Outlet />
          </PageErrorBoundary>
        </div>
      </main>
    </div>
  )
}
