interface SkeletonProps {
  /** What is loading, for screen readers ("Loading memories…"). */
  label: string
  /**
   * The shape of what is coming: rows (a list of cards), cards (a grid of
   * cards), or chat (a conversation).
   */
  shape?: 'rows' | 'cards' | 'chat'
  count?: number
}

function Bar({ className }: { className: string }) {
  return <span className={['block rounded bg-raised motion-safe:animate-pulse', className].join(' ')} />
}

/**
 * A placeholder in the shape of the content that is loading, so the page
 * doesn't jump when it arrives. Screen readers hear the label once; the
 * shapes are hidden from them. Spinners stay for work in progress, such as
 * a search or a download.
 */
export function Skeleton({ label, shape = 'rows', count = 3 }: SkeletonProps) {
  const items = Array.from({ length: count }, (_, i) => i)
  return (
    <div role="status" aria-busy="true">
      <span className="sr-only">{label}</span>
      {shape === 'chat' ? (
        <div className="space-y-4" aria-hidden>
          {items.map((i) => (
            <div key={i} className={['flex', i % 2 === 0 ? 'justify-end' : 'justify-start'].join(' ')}>
              <div className="w-2/3 max-w-md space-y-2 rounded-2xl bg-surface p-4">
                <Bar className="h-3 w-11/12" />
                <Bar className="h-3 w-3/4" />
                {i % 2 === 1 ? <Bar className="h-3 w-1/2" /> : null}
              </div>
            </div>
          ))}
        </div>
      ) : (
        <ul className={shape === 'cards' ? 'grid gap-4 md:grid-cols-2 xl:grid-cols-3' : 'space-y-2'} aria-hidden>
          {items.map((i) => (
            <li key={i} className="card space-y-3">
              <Bar className="h-4 w-2/5" />
              <Bar className="h-3 w-4/5" />
              {shape === 'cards' ? (
                <span className="flex gap-2">
                  <Bar className="h-5 w-14" />
                  <Bar className="h-5 w-14" />
                </span>
              ) : null}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
