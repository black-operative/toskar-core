import { useTranslation } from 'react-i18next'
import { Ratatoskr } from '@/components/ui/Ratatoskr'
import { IDEAS } from './ideas'

// The three parts of every automation, in the order the form asks for them.
const STEPS = ['when', 'what', 'tell'] as const

const stepIcons: Record<(typeof STEPS)[number], string> = {
  // A clock.
  when: 'M8 4.5V8l2.5 1.5M14 8A6 6 0 1 1 2 8a6 6 0 0 1 12 0Z',
  // A spark: the AI doing the work.
  what: 'M8 1.5v3M8 11.5v3M1.5 8h3M11.5 8h3M3.4 3.4l2.1 2.1M10.5 10.5l2.1 2.1M3.4 12.6l2.1-2.1M10.5 5.5l2.1-2.1',
  // A bell.
  tell: 'M4 11.5V7a4 4 0 1 1 8 0v4.5l1 1H3l1-1ZM6.5 14h3',
}

/**
 * What an automation is, in three steps: when it runs, what Toskar does, and
 * when you hear about it. Shown on an empty page, and behind "How it works".
 */
export function AutomationsIntro({ mascot = false }: { mascot?: boolean }) {
  const { t } = useTranslation('automations')
  return (
    <section className="card-outline space-y-4" aria-labelledby="automations-intro-title">
      <div className="flex items-center gap-3">
        {mascot ? <Ratatoskr state="idle" size={64} className="shrink-0" /> : null}
        <h2 id="automations-intro-title" className="section-title">
          {t('intro.title')}
        </h2>
      </div>
      <ol className="grid gap-3 md:grid-cols-3">
        {STEPS.map((step, index) => (
          <li key={step} className="flex gap-3 rounded-lg bg-raised/40 p-3">
            <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-norn/15 text-norn" aria-hidden>
              <svg viewBox="0 0 16 16" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth={1.4} strokeLinecap="round" strokeLinejoin="round">
                <path d={stepIcons[step]} />
              </svg>
            </span>
            <div className="min-w-0">
              <p className="text-sm font-semibold text-ink">
                <span className="text-ink-faint">{index + 1}. </span>
                {t(`intro.steps.${step}.title`)}
              </p>
              <p className="mt-0.5 text-sm leading-relaxed text-ink-muted">{t(`intro.steps.${step}.body`)}</p>
            </div>
          </li>
        ))}
      </ol>
    </section>
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
