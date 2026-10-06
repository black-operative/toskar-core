import { useEffect, useState } from 'react'
import { formatNumber } from '@/i18n/format'

/** The time left until a moment, as m:ss, ticking each second. */
export function useCountdown(until: string | undefined): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (!until) return
    const id = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(id)
  }, [until])
  return until ? Math.max(0, new Date(until).getTime() - now) : 0
}

export function clock(ms: number): string {
  const s = Math.ceil(ms / 1000)
  return `${formatNumber(Math.floor(s / 60))}:${String(s % 60).padStart(2, '0')}`
}
