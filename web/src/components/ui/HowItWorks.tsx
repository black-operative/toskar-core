import { Ratatoskr } from '@/components/ui/Ratatoskr'

export type HowItWorksStep = {
  /** An SVG path on a 16×16 grid, drawn as a stroke. */
  icon: string
  title: string
  body: string
}

/**
 * A page's idea in a few numbered steps, for people who are new to it: shown
 * on an empty page and behind a "How it works" button. `tone` colours the
 * step icons with the page's realm (for example `bg-norn/15 text-norn`).
 */
export function HowItWorks({
  id,
  title,
  steps,
  tone,
  mascot = false,
}: {
  id: string
  title: string
  steps: HowItWorksStep[]
  tone: string
  mascot?: boolean
}) {
  return (
    <section id={id} className="card-outline space-y-4" aria-labelledby={`${id}-title`}>
      <div className="flex items-center gap-3">
        {mascot ? <Ratatoskr state="idle" size={64} className="shrink-0" /> : null}
        <h2 id={`${id}-title`} className="section-title">
          {title}
        </h2>
      </div>
      <ol className="grid gap-3 md:grid-cols-3">
        {steps.map((step, index) => (
          <li key={step.title} className="flex gap-3 rounded-lg bg-raised/40 p-3">
            <span className={['flex h-8 w-8 shrink-0 items-center justify-center rounded-lg', tone].join(' ')} aria-hidden>
              <svg viewBox="0 0 16 16" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth={1.4} strokeLinecap="round" strokeLinejoin="round">
                <path d={step.icon} />
              </svg>
            </span>
            <div className="min-w-0">
              <p className="text-sm font-semibold text-ink">
                <span className="text-ink-faint">{index + 1}. </span>
                {step.title}
              </p>
              <p className="mt-0.5 text-sm leading-relaxed text-ink-muted">{step.body}</p>
            </div>
          </li>
        ))}
      </ol>
    </section>
  )
}

/** Step icons shared by pages that explain themselves. */
export const stepIcons = {
  clock: 'M8 4.5V8l2.5 1.5M14 8A6 6 0 1 1 2 8a6 6 0 0 1 12 0Z',
  spark: 'M8 1.5v3M8 11.5v3M1.5 8h3M11.5 8h3M3.4 3.4l2.1 2.1M10.5 10.5l2.1 2.1M3.4 12.6l2.1-2.1M10.5 5.5l2.1-2.1',
  bell: 'M4 11.5V7a4 4 0 1 1 8 0v4.5l1 1H3l1-1ZM6.5 14h3',
  plug: 'M6 1.5v3M10 1.5v3M4 4.5h8v3a4 4 0 0 1-8 0v-3ZM8 11.5v3',
  person: 'M8 7.5a2.5 2.5 0 1 0 0-5 2.5 2.5 0 0 0 0 5ZM3 14a5 5 0 0 1 10 0',
  chat: 'M2.5 3.5h11v7h-6l-3 2.5v-2.5h-2Z',
  download: 'M8 2.5v7.5M4.5 7 8 10.5 11.5 7M3 13.5h10',
}
