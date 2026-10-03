import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ActivityPanel } from './ActivityPanel'
import { BenchmarkPanel } from './BenchmarkPanel'
import { OverviewPanel } from './OverviewPanel'
import { RealmKicker } from '@/components/ui/Realm'
import { rovingKeyDown } from '@/lib/roving'

type Tab = 'overview' | 'benchmarks' | 'activity'

// The tabs, in order; their names are performance:tabs.<id> in the catalog.
const tabs: Tab[] = ['overview', 'benchmarks', 'activity']

export function PerformancePage() {
  const { t } = useTranslation('performance')
  const [tab, setTab] = useState<Tab>('overview')

  return (
    <div className="w-full min-w-0 space-y-6">
      <header className="page-header">
        <RealmKicker />
        <h1 className="page-title">{t('page.title')}</h1>
        <p className="page-subtitle">{t('page.subtitle')}</p>
      </header>

      <div className="flex flex-wrap gap-2" role="tablist" aria-label={t('page.views')} onKeyDown={rovingKeyDown}>
        {tabs.map((option) => (
          <button
            key={option}
            type="button"
            role="tab"
            aria-selected={tab === option}
            tabIndex={tab === option ? 0 : -1}
            onClick={() => setTab(option)}
            className={[
              'rounded-lg border px-4 py-2 text-sm font-medium transition',
              tab === option
                ? 'border-primary bg-primary-soft text-primary-active'
                : 'border-line bg-surface text-ink-muted hover:border-primary/40 hover:text-ink',
            ].join(' ')}
          >
            {t(`tabs.${option}`)}
          </button>
        ))}
      </div>

      {tab === 'overview' && <OverviewPanel />}
      {tab === 'benchmarks' && <BenchmarkPanel />}
      {tab === 'activity' && <ActivityPanel />}
    </div>
  )
}
