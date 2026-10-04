import { useId } from 'react'
import { useTranslation } from 'react-i18next'
import { LoreButton } from '@/components/ui/LoreButton'
import { logoLore } from '@/lib/lore'

/**
 * The Yggdrasil mark, drawn inline so it follows the theme. Geometry is copied
 * from docs/brand/logo/toskar-mark.svg and toskar-mark-small.svg; teal
 * and gold come from the interface tokens. At 32 px and below it switches to
 * the small mark, which stays legible at favicon sizes.
 */

const TEAL = 'rgb(var(--rgb-primary))'
const GOLD = 'rgb(var(--rgb-accent))'

/** Pads: [cx, cy, clearance radius, ring radius, gold core radius]. */
type Pad = [number, number, number, number, number]

const FULL_PADS: Pad[] = [
  [50, 46, 5.2, 7.3, 3.9],
  [50, 86, 6.0, 8.3, 4.7],
  [23.0, 33.0, 4.2, 6.0, 2.9],
  [12.0, 22.0, 4.2, 6.0, 2.9],
  [23.5, 86.5, 4.4, 6.3, 3.1],
  [77.0, 33.0, 4.2, 6.0, 2.9],
  [88.0, 22.0, 4.2, 6.0, 2.9],
  [76.5, 86.5, 4.4, 6.3, 3.1],
]

const FULL_TRACES: [string, number][] = [
  ['M46.0 86 V40 L18 6', 6.2],
  ['M54.0 86 V40 L82 6', 6.2],
  ['M46.0 56 L23 33.0', 3.3],
  ['M23 33.0 L13 23.0', 3.3],
  ['M46.0 64 L25 85.0', 3.3],
  ['M54.0 56 L77 33.0', 3.3],
  ['M77 33.0 L87 23.0', 3.3],
  ['M54.0 64 L75 85.0', 3.3],
]

const SMALL_PADS: Pad[] = [
  [50, 45, 7.0, 9.8, 5.6],
  [50, 87, 7.8, 10.8, 6.4],
  [26, 82, 6.4, 9.0, 5],
  [74, 82, 6.4, 9.0, 5],
]

const SMALL_TRACES: [string, number][] = [
  ['M50 86 V41 L20 7', 11],
  ['M50 86 V41 L80 7', 11],
  ['M50 62 L30 80', 6],
  ['M50 62 L70 80', 6],
]

/** At this size and below, the small mark is drawn. */
export const SMALL_MARK_MAX_PX = 32

export function YggdrasilMark({
  size = 36,
  className = '',
  title,
  lore = false,
}: {
  size?: number
  className?: string
  title?: string
  /** Clicking the mark opens a short lore entry about the World Tree. */
  lore?: boolean
}) {
  // Several marks can be on screen at once; each needs its own mask and clip ids.
  const { t } = useTranslation('lore')
  const id = useId().replace(/[^a-zA-Z0-9_-]/g, '')
  const small = size <= SMALL_MARK_MAX_PX
  const pads = small ? SMALL_PADS : FULL_PADS
  const traces = small ? SMALL_TRACES : FULL_TRACES
  const clip = `ygg-c-${id}`
  const mask = `ygg-k-${id}`
  const mark = (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      viewBox="0 0 100 100"
      width={size}
      height={size}
      className={['yggdrasil-mark block shrink-0', className].filter(Boolean).join(' ')}
      style={{ transformOrigin: 'center' }}
      role={title ? 'img' : undefined}
      aria-hidden={title ? undefined : 'true'}
      aria-label={title}
      focusable="false"
      data-mark={small ? 'small' : 'full'}
    >
      {title ? <title>{title}</title> : null}
      <defs>
        <clipPath id={clip}>
          <rect x="0" y="9" width="100" height="100" />
        </clipPath>
        <mask id={mask} maskUnits="userSpaceOnUse" x="0" y="0" width="100" height="100">
          <rect width="100" height="100" fill="#fff" />
          <g fill="#000">
            {pads.map(([cx, cy, r]) => (
              <circle key={`k${cx},${cy}`} cx={cx} cy={cy} r={r} />
            ))}
          </g>
        </mask>
      </defs>
      <g mask={`url(#${mask})`}>
        <g clipPath={`url(#${clip})`} fill="none" stroke={TEAL} strokeLinecap="round" strokeLinejoin="round">
          {traces.map(([d, w]) => (
            <path key={d} d={d} strokeWidth={w} />
          ))}
        </g>
        <g fill={TEAL}>
          {pads.map(([cx, cy, , r]) => (
            <circle key={`r${cx},${cy}`} cx={cx} cy={cy} r={r} />
          ))}
        </g>
      </g>
      <g fill={GOLD}>
        {pads.map(([cx, cy, , , r]) => (
          <circle key={`g${cx},${cy}`} cx={cx} cy={cy} r={r} />
        ))}
      </g>
    </svg>
  )
  if (!lore) return mark
  return (
    <LoreButton lore={logoLore()} label={t('logo.label')} className="inline-flex shrink-0">
      {mark}
    </LoreButton>
  )
}
