/** A small line of recent values, such as the last hour of a GPU's busy %. */
export function Sparkline({
  values,
  label,
  max,
  className = 'h-8 w-full',
}: {
  values: number[]
  label: string
  /** The top of the scale, such as 100 for a percentage; the values' own maximum otherwise. */
  max?: number
  className?: string
}) {
  if (values.length < 2) return null
  const width = 240
  const height = 32
  const top = max ?? Math.max(...values)
  const bottom = max != null ? 0 : Math.min(...values)
  const span = top - bottom || 1
  const points = values
    .map((v, i) => {
      const x = (i / (values.length - 1)) * width
      const y = height - 2 - ((Math.min(v, top) - bottom) / span) * (height - 4)
      return `${x.toFixed(1)},${y.toFixed(1)}`
    })
    .join(' ')
  return (
    <svg viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="none" className={className} role="img" aria-label={label}>
      <polyline points={points} fill="none" className="stroke-primary" strokeWidth={1.5} strokeLinejoin="round" vectorEffect="non-scaling-stroke" />
    </svg>
  )
}
