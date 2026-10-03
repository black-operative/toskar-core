import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, ApiError } from '@/lib/api'
import { formatNumber, formatRelativeTime } from '@/i18n/format'
import type { JoinToken, JoinTokenCreated } from '@/types/api'
import { rovingKeyDown } from '@/lib/roving'

type Kind = 'installed' | 'install' | 'windows'

const KINDS: Kind[] = ['installed', 'install', 'windows']

function commandFor(created: JoinTokenCreated, kind: Kind): string {
  switch (kind) {
    case 'install':
      return created.install_command
    case 'windows':
      return created.windows_command
  }
  return created.command
}

/** The time left until a moment, as m:ss, ticking each second. */
function useCountdown(until: string | undefined): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (!until) return
    const id = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(id)
  }, [until])
  return until ? Math.max(0, new Date(until).getTime() - now) : 0
}

function clock(ms: number): string {
  const s = Math.ceil(ms / 1000)
  return `${formatNumber(Math.floor(s / 60))}:${String(s % 60).padStart(2, '0')}`
}

/**
 * Add a computer by command (#40): a one-time join command to run on the
 * new computer, over SSH or at its keyboard, with a countdown, revoke, and
 * the recent commands.
 */
export function JoinByCommand({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation('computers')
  const queryClient = useQueryClient()
  const [created, setCreated] = useState<JoinTokenCreated | null>(null)
  const [kind, setKind] = useState<Kind>('installed')
  const [copied, setCopied] = useState(false)

  const tokens = useQuery({
    queryKey: ['join-tokens'],
    queryFn: () => api.listJoinTokens(),
    // While a command is waiting to be used, watch for the computer joining.
    refetchInterval: created ? 3000 : false,
  })
  const current: JoinToken | undefined = created ? (tokens.data?.find((x) => x.id === created.id) ?? created) : undefined
  const left = useCountdown(current?.status === 'active' ? created?.expires_at : undefined)
  const status = current?.status === 'active' && created && left === 0 ? 'expired' : current?.status

  useEffect(() => {
    if (status === 'used') void queryClient.invalidateQueries({ queryKey: ['nodes'] })
  }, [status, queryClient])

  const create = useMutation({
    mutationFn: () => api.createJoinToken(),
    onSuccess: (next) => {
      setCreated(next ?? null)
      setCopied(false)
      void queryClient.invalidateQueries({ queryKey: ['join-tokens'] })
    },
  })
  const revoke = useMutation({
    mutationFn: (id: string) => api.revokeJoinToken(id),
    onSettled: () => void queryClient.invalidateQueries({ queryKey: ['join-tokens'] }),
  })
  const error = create.error ?? revoke.error
  const command = created ? commandFor(created, kind) : ''
  const recent = (tokens.data ?? []).filter((x) => x.id !== created?.id)

  return (
    <section className="card space-y-4 animate-fade" aria-labelledby="join-by-command-title">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 id="join-by-command-title" className="font-display text-lg font-semibold text-ink">
            {t('join.title')}
          </h2>
          <p className="mt-1 text-sm text-ink-muted">{t('join.description')}</p>
        </div>
        <button type="button" className="btn-secondary px-3 py-1.5 text-xs" onClick={onClose}>
          {t('join.close')}
        </button>
      </div>

      {error ? (
        <p role="alert" className="rounded-lg bg-danger/10 px-3 py-2 text-sm text-danger">
          {error instanceof ApiError ? error.message : String(error)}
        </p>
      ) : null}

      {!created ? (
        <div className="space-y-2">
          <button type="button" className="btn-primary" disabled={create.isPending} onClick={() => create.mutate()}>
            {create.isPending ? t('join.making') : t('join.make')}
          </button>
          <p className="text-xs text-ink-muted">{t('join.makeHint')}</p>
        </div>
      ) : status === 'used' ? (
        <div className="rounded-xl bg-success/10 px-4 py-3">
          <p className="font-medium text-success">{t('join.joined', { name: current?.used_by ?? '' })}</p>
          <button type="button" className="btn-secondary mt-3 px-3 py-1.5 text-xs" onClick={() => create.mutate()}>
            {t('join.another')}
          </button>
        </div>
      ) : (
        <div className="space-y-3">
          <div role="tablist" aria-label={t('join.kindLabel')} className="flex flex-wrap gap-1.5" onKeyDown={rovingKeyDown}>
            {KINDS.map((k) => (
              <button
                key={k}
                type="button"
                role="tab"
                aria-selected={kind === k}
                tabIndex={kind === k ? 0 : -1}
                className={['status-chip transition', kind === k ? 'bg-primary/20 text-ink ring-1 ring-primary' : 'bg-raised/80 text-ink-muted hover:text-ink'].join(' ')}
                onClick={() => {
                  setKind(k)
                  setCopied(false)
                }}
              >
                {t(`join.kinds.${k}`)}
              </button>
            ))}
          </div>
          <p className="text-xs text-ink-muted">{t(`join.kindHints.${kind}`)}</p>
          <div className="relative">
            <pre className="overflow-x-auto whitespace-pre-wrap break-all rounded-lg bg-raised/70 p-3 pe-20 font-mono text-xs text-ink" aria-label={t('join.commandLabel')}>
              {command}
            </pre>
            <button
              type="button"
              className="btn-secondary absolute end-2 top-2 px-2.5 py-1 text-xs"
              disabled={status !== 'active'}
              onClick={() => void navigator.clipboard.writeText(command).then(() => setCopied(true))}
            >
              {copied ? t('join.copied') : t('join.copy')}
            </button>
          </div>
          <div className="flex flex-wrap items-center justify-between gap-2 text-sm">
            <p className={status === 'active' ? 'text-ink-muted' : 'text-danger'} aria-live="polite">
              {status === 'active' ? t('join.expiresIn', { time: clock(left) }) : t(`join.status.${status ?? 'expired'}`)}
              {status === 'active' ? <span className="text-ink-faint"> · {t('join.oneUse')}</span> : null}
            </p>
            <div className="flex gap-2">
              {status === 'active' ? (
                <button type="button" className="btn-secondary px-3 py-1.5 text-xs" disabled={revoke.isPending} onClick={() => revoke.mutate(created.id)}>
                  {t('join.revoke')}
                </button>
              ) : null}
              <button type="button" className="btn-secondary px-3 py-1.5 text-xs" disabled={create.isPending} onClick={() => create.mutate()}>
                {t('join.newCommand')}
              </button>
            </div>
          </div>
          <p className="text-xs text-ink-faint">{t('join.fingerprint', { fingerprint: created.fingerprint })}</p>
        </div>
      )}

      {recent.length > 0 ? (
        <details className="text-sm">
          <summary className="cursor-pointer text-ink-muted">{t('join.recent', { count: recent.length })}</summary>
          <ul className="mt-2 space-y-1.5">
            {recent.map((x) => (
              <li key={x.id} className="flex flex-wrap items-center justify-between gap-2 rounded-lg bg-raised/50 px-3 py-2">
                <span className="text-xs text-ink-muted">
                  <span className="font-mono text-ink">{x.id}</span> · {formatRelativeTime(x.created_at)} ·{' '}
                  {x.status === 'used' && x.used_by ? t('join.usedBy', { name: x.used_by }) : t(`join.status.${x.status}`)}
                </span>
                {x.status === 'active' ? (
                  <button type="button" className="text-xs text-danger hover:underline" disabled={revoke.isPending} onClick={() => revoke.mutate(x.id)}>
                    {t('join.revoke')}
                  </button>
                ) : null}
              </li>
            ))}
          </ul>
        </details>
      ) : null}
    </section>
  )
}
