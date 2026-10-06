import { useTranslation } from 'react-i18next'
import { HowItWorks, stepIcons } from '@/components/ui/HowItWorks'
import { IDEAS } from './ideas'

/**
 * What an automation is, in three steps: when it runs, what Toskar does, and
 * when you hear about it. Shown on an empty page, and behind "How it works".
 */
export function AutomationsIntro({ mascot = false }: { mascot?: boolean }) {
  const { t } = useTranslation('automations')
  return (
    <HowItWorks
      id="automations-intro-steps"
      title={t('intro.title')}
      tone="bg-norn/15 text-norn"
      mascot={mascot}
      steps={[
        { icon: stepIcons.clock, title: t('intro.steps.when.title'), body: t('intro.steps.when.body') },
        { icon: stepIcons.spark, title: t('intro.steps.what.title'), body: t('intro.steps.what.body') },
        { icon: stepIcons.bell, title: t('intro.steps.tell.title'), body: t('intro.steps.tell.body') },
      ]}
    />
  )
}

/** Ready-made automations; choosing one opens the form filled in from it. */
export function IdeaGallery({ onUse, compact = false }: { onUse: (request: string) => void; compact?: boolean }) {
  const { t } = useTranslation('automations')
  return (
    <section className="space-y-3" aria-labelledby="automations-ideas-title">
      <h2 id="automations-ideas-title" className={compact ? 'label-caps' : 'section-title'}>
        {t('ideas.title')}
      </h2>
      <ul className={compact ? 'grid gap-2' : 'grid gap-3 sm:grid-cols-2 xl:grid-cols-3'}>
        {IDEAS.map(({ id }) => {
          const title = t(`ideas.${id}.title`)
          return (
            <li key={id}>
              <button
                type="button"
                className="selectable flex h-full w-full flex-col items-start gap-1"
                onClick={() => onUse(t(`ideas.${id}.request`))}
              >
                <span className="selectable-title text-sm font-semibold text-ink">{title}</span>
                <span className="text-sm text-ink-muted">{t(`ideas.${id}.body`)}</span>
                {compact ? null : (
                  <span className="mt-1 text-xs leading-relaxed text-ink-faint">{t(`ideas.${id}.request`)}</span>
                )}
                <span className="mt-auto pt-2 text-xs font-semibold text-primary-active">
                  {t('ideas.use')}{' '}
                  <span className="inline-block rtl:-scale-x-100" aria-hidden>
                    →
                  </span>
                </span>
              </button>
            </li>
          )
        })}
      </ul>
    </section>
  )
}
