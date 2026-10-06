import { useTranslation } from 'react-i18next'
import type {
  CategoryWinner,
  FitLabel,
  Model,
  ModelDownloadProgressPayload,
  ModelFit,
} from '@/types/api'
import { ModelCard } from './ModelCard'
import { CATEGORY_SECTIONS, largerAlternative } from './modelPresentation'

export function DiscoverTab({
  models,
  fits,
  peerFits,
  winners,
  progress,
  search,
  onInstall,
  onBrowseAll,
  installingId,
}: {
  models: Model[]
  fits: Record<string, ModelFit>
  peerFits?: Record<string, { nodeName: string; label: FitLabel }[]>
  winners: CategoryWinner[]
  progress: Record<string, ModelDownloadProgressPayload>
  search: string
  onInstall: (id: string) => void
  onBrowseAll: () => void
  installingId?: string | null
}) {
  const { t } = useTranslation('models')
  const q = search.trim().toLowerCase()
  const filtered = models.filter((m) => {
    if (!q) return true
    return (
      m.display_name.toLowerCase().includes(q) ||
      m.id.toLowerCase().includes(q) ||
      m.summary?.toLowerCase().includes(q) ||
      m.tags?.some((t) => t.toLowerCase().includes(q))
    )
  })

  // One winner badge per category — keep gold rare.
  const winnerByModel = new Map<string, string>()
  const chosenByCommunity = new Set<string>()
  const seenCategories = new Set<string>()
  for (const w of winners) {
    if (seenCategories.has(w.category)) continue
    if (winnerByModel.has(w.model_id)) continue
    seenCategories.add(w.category)
    winnerByModel.set(w.model_id, w.label)
    if (w.community_chosen) chosenByCommunity.add(w.model_id)
  }

  const alternative = largerAlternative(models, fits)

  const recommended = winners
    .map((w) => filtered.find((m) => m.id === w.model_id))
    .filter(Boolean) as Model[]
  const seen = new Set<string>()
  const uniqueRecommended = recommended.filter((m) => {
    if (seen.has(m.id)) return false
    seen.add(m.id)
    return true
  })

  return (
    <div className="space-y-8">
      <section className="space-y-3">
        <div className="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h2 className="section-title">{t('discover.recommended')}</h2>
            <p className="mt-1 text-sm text-ink-muted">{t('discover.recommendedHint')}</p>
          </div>
          <button type="button" className="btn-secondary gap-1.5" onClick={onBrowseAll}>
            {t('discover.browseAll')}{' '}
            <span className="inline-block rtl:-scale-x-100" aria-hidden>
              →
            </span>
          </button>
        </div>
        {uniqueRecommended.length === 0 ? (
          <p className="text-sm text-ink-muted">{t('discover.noRecommendations')}</p>
        ) : (
          <ul className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
            {uniqueRecommended.map((model) => (
              <li key={`rec-${model.id}`}>
                <ModelCard
                  model={model}
                  fit={fits[model.id]}
                  peerFits={peerFits?.[model.id]}
                  winnerLabel={winnerByModel.get(model.id)}
                  chosenByCommunity={chosenByCommunity.has(model.id)}
                  progress={progress[model.id]}
                  installing={installingId === model.id}
                  onInstall={() => onInstall(model.id)}
                  alternative={alternative}
                  onInstallAlternative={onInstall}
                />
              </li>
            ))}
          </ul>
        )}
      </section>

      {CATEGORY_SECTIONS.map((section) => {
        const list = filtered.filter(
          (m) => section.match(m) && !uniqueRecommended.some((r) => r.id === m.id),
        )
        if (list.length === 0) return null
        return (
          <section key={section.id} className="space-y-3">
            <h2 className="section-title">{t(`categories.${section.id}`)}</h2>
            <ul className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
              {list.map((model) => (
                <li key={`${section.id}-${model.id}`}>
                  <ModelCard
                    model={model}
                    fit={fits[model.id]}
                  peerFits={peerFits?.[model.id]}
                    progress={progress[model.id]}
                    installing={installingId === model.id}
                    onInstall={() => onInstall(model.id)}
                    alternative={alternative}
                    onInstallAlternative={onInstall}
                  />
                </li>
              ))}
            </ul>
          </section>
        )
      })}

      {filtered.length === 0 && (
        <p className="text-sm text-ink-muted">{t('discover.noMatches')}</p>
      )}
    </div>
  )
}
