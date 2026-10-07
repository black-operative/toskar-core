import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { Trans, useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { api, ApiError } from '@/lib/api'
import type { DevicePairing } from '@/types/api'
import { clock, useCountdown } from './countdown'

/** Toskar for iPhone on the App Store. */
export const IPHONE_APP_URL = 'https://apps.apple.com/app/id6817062966'

/** The code as two groups of three, easier to read across a room. */
function grouped(code: string): string {
  return code.length === 6 ? `${code.slice(0, 3)} ${code.slice(3)}` : code
}

/**
 * Connect a device (#216): a 6-digit code to type on the phone, which then
 * gets a key of its own. Turns on local network access first when it's off,
 * since the phone reaches this computer over the network.
 */
export function ConnectPhone({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation('computers')
  const queryClient = useQueryClient()
  const [shown, setShown] = useState<DevicePairing | null>(null)

  const settings = useQuery({ queryKey: ['settings'], queryFn: () => api.getSettings(), retry: false })
  const lanOn = settings.data?.lan_api_enabled ?? false
  const computer = settings.data?.node_name || t('phone.thisComputer')
  // The phone lists this computer under On this network only while it
  // announces itself (Find other computers); otherwise it's the address.
  const listed = settings.data?.discovery_enabled ?? false

  const status = useQuery({
    queryKey: ['device-pairing'],
    queryFn: () => api.getDevicePairing(),
    enabled: shown !== null,
    // While the code is up, watch for the phone connecting.
    refetchInterval: (q) => (q.state.data?.state === 'waiting' || q.state.data === undefined ? 2000 : false),
  })
  const left = useCountdown(shown?.expires_at)
  const state = status.data?.state ?? shown?.state
  const waiting = state === 'waiting' && left > 0
  const address = status.data?.address ?? shown?.address
  const connected = state === 'connected'

  const start = useMutation({
    mutationFn: () => api.startDevicePairing(!lanOn),
    onSuccess: (next) => {
      setShown(next ?? null)
      queryClient.setQueryData(['device-pairing'], next)
      void queryClient.invalidateQueries({ queryKey: ['settings'] })
    },
  })
  const cancel = useMutation({ mutationFn: () => api.cancelDevicePairing() })

  useEffect(() => {
    if (connected) void queryClient.invalidateQueries({ queryKey: ['api-keys'] })
  }, [connected, queryClient])

  // Closing the panel stops showing a code no phone used.
  const waitingRef = useRef(false)
  waitingRef.current = waiting
  useEffect(() => () => {
    if (waitingRef.current) void api.cancelDevicePairing().catch(() => {})
  }, [])

  const error = start.error ?? cancel.error
  const close = () => {
    if (waiting) cancel.mutate()
    onClose()
  }

  return (
    <section className="card space-y-4 animate-fade" aria-labelledby="connect-phone-title">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 id="connect-phone-title" className="font-display text-lg font-semibold text-ink">
            {t('phone.title')}
          </h2>
          <p className="mt-1 text-sm text-ink-muted">{t('phone.description')}</p>
        </div>
        <button type="button" className="btn-secondary px-3 py-1.5 text-xs" onClick={close}>
          {t('join.close')}
        </button>
      </div>

      {error ? (
        <p role="alert" className="rounded-lg bg-danger/10 px-3 py-2 text-sm text-danger">
          {error instanceof ApiError ? error.message : String(error)}
        </p>
      ) : null}

      {!shown ? (
        <div className="space-y-3">
          {settings.data && !lanOn ? <p className="text-sm text-ink-muted">{t('phone.lanOff')}</p> : null}
          <button type="button" className="btn-primary" disabled={start.isPending || settings.isLoading} onClick={() => start.mutate()}>
            {start.isPending ? t('phone.showing') : lanOn ? t('phone.show') : t('phone.allowAndShow')}
          </button>
          <p className="text-xs text-ink-muted">
            <Trans t={t} i18nKey="phone.getApp" components={{ store: <a href={IPHONE_APP_URL} target="_blank" rel="noopener noreferrer" className="text-primary hover:underline" /> }} />
          </p>
        </div>
      ) : connected ? (
        <div className="rounded-xl bg-success/10 px-4 py-3">
          <p className="font-medium text-success" role="status">
            {t('phone.connected', { name: status.data?.device?.name ?? '' })}
          </p>
          <p className="mt-1 text-sm text-ink-muted">
            <Trans t={t} i18nKey="phone.manage" components={{ access: <Link to="/api-access" className="text-primary hover:underline" /> }} />
          </p>
          <button type="button" className="btn-secondary mt-3 px-3 py-1.5 text-xs" disabled={start.isPending} onClick={() => start.mutate()}>
            {t('phone.another')}
          </button>
        </div>
      ) : waiting ? (
        <div className="space-y-4">
          <p className="text-center font-mono text-4xl font-semibold tracking-[0.2em] text-ink tabular-nums" aria-label={t('phone.codeLabel', { code: shown.code?.split('').join(' ') ?? '' })}>
            {grouped(shown.code ?? '')}
          </p>
          <ol className="list-decimal space-y-1 ps-5 text-sm text-ink">
            <li>
              <Trans t={t} i18nKey="phone.steps.open" components={{ store: <a href={IPHONE_APP_URL} target="_blank" rel="noopener noreferrer" className="text-primary hover:underline" /> }} />
            </li>
            {listed || !address ? (
              <li>{t('phone.steps.choose', { name: computer })}</li>
            ) : (
              <li>
                {t('phone.steps.typeAddress')} <span className="font-mono">{address}</span>
              </li>
            )}
            <li>{t('phone.steps.enter')}</li>
          </ol>
          {listed && address ? (
            <p className="text-xs text-ink-muted">
              {t('phone.address')} <span className="font-mono text-ink">{address}</span>
            </p>
          ) : null}
          {shown.reachable === false ? <p className="text-xs text-warning">{t('phone.unreachable')}</p> : null}
          <div className="flex flex-wrap items-center justify-between gap-2 text-sm">
            <p className="text-ink-muted" aria-live="polite">
              {t('join.expiresIn', { time: clock(left) })}
              <span className="text-ink-faint"> · {t('join.oneUse')}</span>
            </p>
            <div className="flex gap-2">
              <button type="button" className="btn-secondary px-3 py-1.5 text-xs" disabled={start.isPending} onClick={() => start.mutate()}>
                {t('phone.newCode')}
              </button>
            </div>
          </div>
          <p className="text-xs text-ink-faint">{t('phone.http')}</p>
        </div>
      ) : (
        <div className="space-y-2">
          <p className="text-sm text-danger" role="status">
            {state === 'cancelled' ? t('phone.cancelled') : t('phone.expired')}
          </p>
          <button type="button" className="btn-primary" disabled={start.isPending} onClick={() => start.mutate()}>
            {t('phone.newCode')}
          </button>
        </div>
      )}
    </section>
  )
}
