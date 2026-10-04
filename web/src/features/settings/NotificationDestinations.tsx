import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import i18n from '@/i18n'
import { api } from '@/lib/api'
import type {
  NotificationCategory,
  NotificationDestination,
  NotificationDestinationInput,
  NotificationSeverity,
  QuietHours,
} from '@/types/api'
import { CATEGORIES, categoryLabel } from './notificationLabels'

const KEY = ['notification-destinations'] as const

// The severity choices; each is notifications:destinations.severities.<value or "all">.
const SEVERITIES: ('' | NotificationSeverity)[] = ['', 'success', 'warning', 'error']

function severityLabel(value: '' | NotificationSeverity): string {
  return i18n.t(`notifications:destinations.severities.${value || 'all'}`)
}

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : i18n.t('notifications:destinations.generic')
}

/**
 * Where notifications go besides the app (Gjallarhorn §12–13, §24–26): email
 * through your own SMTP server, and signed webhooks, each with the
 * categories and severities it receives, and quiet hours. Passwords and
 * signing secrets stay on this computer and are never shown again.
 */
export function NotificationDestinations() {
  const { t } = useTranslation('notifications')
  const query = useQuery({ queryKey: KEY, queryFn: () => api.listNotificationDestinations(), retry: false })
  const [adding, setAdding] = useState<'' | NotificationDestination['kind']>('')
  const destinations = query.data ?? []
  return (
    <section className="card space-y-4">
      <div>
        <h2 className="section-title">{t('destinations.title')}</h2>
        <p className="mt-1 text-sm text-ink-muted">{t('destinations.description')}</p>
      </div>
      {query.isError && <p className="text-sm text-danger">{errorText(query.error)}</p>}
      {destinations.map((d) => (
        <DestinationRow key={d.id} destination={d} />
      ))}
      {adding ? (
        <DestinationForm kind={adding} onDone={() => setAdding('')} />
      ) : (
        <div className="flex flex-wrap gap-2">
          <button type="button" className="btn-secondary px-3 py-1.5 text-xs" onClick={() => setAdding('email')}>
            {t('destinations.addEmail')}
          </button>
          <button type="button" className="btn-secondary px-3 py-1.5 text-xs" onClick={() => setAdding('ntfy')}>
            {t('destinations.addPush')}
          </button>
          <button type="button" className="btn-secondary px-3 py-1.5 text-xs" onClick={() => setAdding('webhook')}>
            {t('destinations.addWebhook')}
          </button>
        </div>
      )}
      <QuietHoursForm />
    </section>
  )
}

function summary(d: NotificationDestination): string {
  const ntfy = {
    topic: d.ntfy?.topic ?? '',
    server: d.ntfy?.server.replace(/^https?:\/\//, '') ?? '',
  }
  const where =
    d.kind === 'email'
      ? (d.email?.to ?? []).join(', ')
      : d.kind === 'ntfy'
        ? i18n.t(d.ntfy?.content === 'private' ? 'notifications:destinations.ntfyWherePrivate' : 'notifications:destinations.ntfyWhere', ntfy)
        : (d.webhook?.url ?? '')
  const what = d.categories?.length
    ? d.categories.map(categoryLabel).join(', ')
    : i18n.t('notifications:destinations.allCategories')
  const level = severityLabel(d.min_severity ?? '')
  const summary = i18n.t('notifications:destinations.summary', { where, what, level })
  return d.digest?.at ? `${summary} ${i18n.t('notifications:destinations.digestSummary', { time: d.digest.at })}` : summary
}

function DestinationRow({ destination: d }: { destination: NotificationDestination }) {
  const { t } = useTranslation('notifications')
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState(false)
  const [result, setResult] = useState<{ ok: boolean; text: string } | null>(null)
  const [secret, setSecret] = useState('')
  const refresh = () => queryClient.invalidateQueries({ queryKey: KEY })

  const test = useMutation({
    mutationFn: () => api.testNotificationDestination(d.id),
    onSuccess: (r) =>
      setResult(r.ok ? { ok: true, text: t('destinations.sent') } : { ok: false, text: r.error ?? t('destinations.notSent') }),
    onError: (err) => setResult({ ok: false, text: errorText(err) }),
  })
  const toggle = useMutation({
    mutationFn: () => api.updateNotificationDestination(d.id, { enabled: !d.enabled }),
    onSettled: () => void refresh(),
  })
  const remove = useMutation({ mutationFn: () => api.deleteNotificationDestination(d.id), onSettled: () => void refresh() })
  const rotate = useMutation({
    mutationFn: () => api.rotateNotificationSecret(d.id),
    onSuccess: (r) => setSecret(r?.secret ?? ''),
  })

  if (editing) return <DestinationForm kind={d.kind} existing={d} onDone={() => setEditing(false)} />
  return (
    <div className="rounded-lg border border-line/60 p-3">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="min-w-0">
          <p className="text-sm font-medium text-ink">
            {d.name} <span className="text-xs font-normal text-ink-faint">{t(`destinations.kinds.${d.kind}`)}</span>
            {!d.enabled && <span className="ms-1 text-xs font-normal text-ink-faint">{t('destinations.off')}</span>}
          </p>
          <p className="mt-0.5 break-all text-xs text-ink-muted">{summary(d)}</p>
        </div>
        <div className="flex shrink-0 flex-wrap gap-2">
          <button type="button" className="btn-secondary px-3 py-1.5 text-xs" disabled={test.isPending} onClick={() => test.mutate()}>
            {test.isPending ? t('destinations.sending') : t('destinations.test')}
          </button>
          <button type="button" className="btn-secondary px-3 py-1.5 text-xs" onClick={() => setEditing(true)}>
            {t('destinations.change')}
          </button>
          <button type="button" className="btn-secondary px-3 py-1.5 text-xs" disabled={toggle.isPending} onClick={() => toggle.mutate()}>
            {d.enabled ? t('destinations.turnOff') : t('destinations.turnOn')}
          </button>
          {d.kind === 'webhook' && (
            <button
              type="button"
              className="btn-secondary px-3 py-1.5 text-xs"
              disabled={rotate.isPending}
              onClick={() => {
                if (window.confirm(t('destinations.confirmSecret'))) rotate.mutate()
              }}
            >
              {t('destinations.newSecret')}
            </button>
          )}
          <button
            type="button"
            className="btn-secondary px-3 py-1.5 text-xs"
            disabled={remove.isPending}
            onClick={() => {
              if (window.confirm(t('destinations.confirmRemove', { name: d.name }))) remove.mutate()
            }}
          >
            {t('destinations.remove')}
          </button>
        </div>
      </div>
      {result && <p className={['mt-2 text-xs', result.ok ? 'text-success' : 'text-danger'].join(' ')}>{result.text}</p>}
      {secret && <SecretOnce secret={secret} />}
    </div>
  )
}

function SecretOnce({ secret }: { secret: string }) {
  const { t } = useTranslation('notifications')
  return (
    <div className="mt-2 rounded-md bg-info/10 px-2.5 py-2 text-xs leading-relaxed text-ink">
      <p>{t('destinations.secretOnce')}</p>
      <code className="mt-1 block break-all font-mono">{secret}</code>
    </div>
  )
}

function DestinationForm({
  kind,
  existing,
  onDone,
}: {
  kind: NotificationDestination['kind']
  existing?: NotificationDestination
  onDone: () => void
}) {
  const { t } = useTranslation('notifications')
  const queryClient = useQueryClient()
  const [name, setName] = useState(existing?.name ?? t(`destinations.defaultNames.${kind}`))
  const [server, setServer] = useState(existing?.ntfy?.server ?? 'https://ntfy.sh')
  const [topic, setTopic] = useState(existing?.ntfy?.topic ?? randomTopic())
  const [content, setContent] = useState<'' | 'full' | 'private'>(existing?.ntfy?.content ?? '')
  const [openUrl, setOpenUrl] = useState(existing?.ntfy?.open_url ?? '')
  const [url, setUrl] = useState(existing?.webhook?.url ?? '')
  const [host, setHost] = useState(existing?.email?.host ?? '')
  const [port, setPort] = useState(String(existing?.email?.port ?? ''))
  const [tls, setTls] = useState(existing?.email?.tls ?? 'starttls')
  const [username, setUsername] = useState(existing?.email?.username ?? '')
  const [password, setPassword] = useState('')
  const [from, setFrom] = useState(existing?.email?.from ?? '')
  const [to, setTo] = useState((existing?.email?.to ?? []).join(', '))
  const [categories, setCategories] = useState<NotificationCategory[]>(existing?.categories ?? [])
  const [minSeverity, setMinSeverity] = useState<'' | NotificationSeverity>(existing?.min_severity ?? '')
  const [digestAt, setDigestAt] = useState(existing?.digest?.at ?? '')
  const [error, setError] = useState('')
  const [secret, setSecret] = useState('')

  const save = useMutation({
    mutationFn: async () => {
      const input: NotificationDestinationInput = { name, categories, min_severity: minSeverity }
      if (digestAt) input.digest = { at: digestAt, time_zone: Intl.DateTimeFormat().resolvedOptions().timeZone }
      else if (existing?.digest?.at) input.digest = { at: '' }
      if (kind === 'webhook') input.webhook = { url: url.trim() }
      else if (kind === 'ntfy') {
        input.ntfy = { server: server.trim(), topic: topic.trim(), content: content || undefined, open_url: openUrl.trim() || undefined }
        if (password) input.password = password
      } else {
        input.email = {
          host: host.trim(),
          port: Number(port) || 0,
          tls,
          username: username.trim() || undefined,
          from: from.trim(),
          to: to
            .split(',')
            .map((s) => s.trim())
            .filter(Boolean),
        }
        if (password) input.password = password
      }
      if (existing) {
        await api.updateNotificationDestination(existing.id, input)
        return ''
      }
      const created = await api.createNotificationDestination({ ...input, kind })
      return created?.secret ?? ''
    },
    onSuccess: (newSecret) => {
      void queryClient.invalidateQueries({ queryKey: KEY })
      if (newSecret) setSecret(newSecret)
      else onDone()
    },
    onError: (err) => setError(errorText(err)),
  })

  if (secret) {
    return (
      <div className="rounded-lg border border-line/60 p-3">
        <p className="text-sm font-medium text-ink">{t('destinations.added', { name })}</p>
        <SecretOnce secret={secret} />
        <button type="button" className="btn-primary mt-3 px-3 py-1.5 text-xs" onClick={onDone}>
          {t('destinations.done')}
        </button>
      </div>
    )
  }

  const field = (label: string, value: string, set: (v: string) => void, props: Record<string, unknown> = {}) => (
    <label className="block text-sm">
      <span className="text-ink-muted">{label}</span>
      <input className="field mt-1 w-full" value={value} onChange={(e) => set(e.target.value)} spellCheck={false} {...props} />
    </label>
  )

  return (
    <form
      className="space-y-3 rounded-lg border border-line/60 p-3"
      onSubmit={(e) => {
        e.preventDefault()
        setError('')
        save.mutate()
      }}
    >
      {field(t('destinations.name'), name, setName)}
      {kind === 'ntfy' ? (
        <>
          <p className="rounded-md bg-info/10 px-2.5 py-2 text-xs leading-relaxed text-ink">{t('destinations.ntfyIntro')}</p>
          <div className="grid gap-3 sm:grid-cols-2">
            {field(t('destinations.ntfyServer'), server, setServer, { placeholder: 'https://ntfy.sh' })}
            {field(t('destinations.topic'), topic, setTopic)}
          </div>
          {field(t('destinations.accessToken'), password, setPassword, {
            type: 'password',
            autoComplete: 'off',
            placeholder: existing?.has_secret ? t('destinations.stored') : t('destinations.onlyIfNeeded'),
          })}
          <label className="block text-sm">
            <span className="text-ink-muted">{t('destinations.whatToSend')}</span>
            <select className="field mt-1 w-full" value={content} onChange={(e) => setContent(e.target.value as typeof content)}>
              <option value="">{t('destinations.contentAuto')}</option>
              <option value="full">{t('destinations.contentFull')}</option>
              <option value="private">{t('destinations.contentPrivate')}</option>
            </select>
          </label>
          {field(t('destinations.openAt'), openUrl, setOpenUrl, { placeholder: 'http://192.168.1.10:7331' })}
          <p className="text-xs text-ink-faint">{t('destinations.ntfyPublic')}</p>
        </>
      ) : kind === 'webhook' ? (
        <>
          {field(t('destinations.address'), url, setUrl, { placeholder: 'https://example.com/yggdrasil' })}
          <p className="text-xs text-ink-faint">{t('destinations.webhookHint')}</p>
        </>
      ) : (
        <>
          <div className="grid gap-3 sm:grid-cols-[1fr_7rem_9rem]">
            {field(t('destinations.smtpServer'), host, setHost, { placeholder: 'smtp.example.com' })}
            {field(t('destinations.port'), port, setPort, { placeholder: tls === 'tls' ? '465' : '587', inputMode: 'numeric' })}
            <label className="block text-sm">
              <span className="text-ink-muted">{t('destinations.security')}</span>
              <select className="field mt-1 w-full" value={tls} onChange={(e) => setTls(e.target.value as typeof tls)}>
                <option value="starttls">STARTTLS</option>
                <option value="tls">TLS</option>
                <option value="none">{t('destinations.tlsNone')}</option>
              </select>
            </label>
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            {field(t('destinations.username'), username, setUsername, { autoComplete: 'off' })}
            {field(t('destinations.password'), password, setPassword, {
              type: 'password',
              autoComplete: 'new-password',
              placeholder: existing?.has_secret ? t('destinations.stored') : '',
            })}
          </div>
          {/* eslint-disable-next-line i18next/no-literal-string -- an example of what to type, not prose */}
          {field(t('destinations.from'), from, setFrom, { placeholder: 'Toskar <toskar@example.com>' })}
          {/* eslint-disable-next-line i18next/no-literal-string -- an example of what to type, not prose */}
          {field(t('destinations.to'), to, setTo, { placeholder: 'you@example.com, someone@example.com' })}
        </>
      )}
      <fieldset>
        <legend className="text-sm text-ink-muted">{t('destinations.categories')}</legend>
        <div className="mt-1 flex flex-wrap gap-x-4 gap-y-1">
          {CATEGORIES.map((c) => (
            <label key={c} className="flex items-center gap-1.5 text-xs text-ink">
              <input
                type="checkbox"
                checked={categories.includes(c)}
                onChange={(e) => setCategories((cur) => (e.target.checked ? [...cur, c] : cur.filter((x) => x !== c)))}
              />
              {categoryLabel(c)}
            </label>
          ))}
        </div>
      </fieldset>
      <label className="block text-sm">
        <span className="text-ink-muted">{t('destinations.send')}</span>
        <select className="field mt-1 w-full" value={minSeverity} onChange={(e) => setMinSeverity(e.target.value as typeof minSeverity)}>
          {SEVERITIES.map((s) => (
            <option key={s} value={s}>
              {severityLabel(s)}
            </option>
          ))}
        </select>
      </label>
      <label className="block text-sm">
        <span className="text-ink-muted">{t('destinations.when')}</span>
        <select className="field mt-1 w-full" value={digestAt ? 'digest' : 'now'} onChange={(e) => setDigestAt(e.target.value === 'digest' ? digestAt || '18:00' : '')}>
          <option value="now">{t('destinations.whenNow')}</option>
          <option value="digest">{t('destinations.whenDigest')}</option>
        </select>
      </label>
      {digestAt ? (
        <div>
          <label className="block text-sm">
            <span className="text-ink-muted">{t('destinations.digestAt')}</span>
            <input type="time" className="field mt-1 w-full" value={digestAt} required onChange={(e) => setDigestAt(e.target.value)} />
          </label>
          <p className="mt-1 text-xs text-ink-faint">{t('destinations.digestHint')}</p>
        </div>
      ) : null}
      {error && <p className="text-xs text-danger">{error}</p>}
      <div className="flex gap-2">
        <button type="submit" className="btn-primary px-3 py-1.5 text-xs" disabled={save.isPending}>
          {save.isPending ? t('destinations.saving') : existing ? t('destinations.save') : t('destinations.add')}
        </button>
        <button type="button" className="btn-secondary px-3 py-1.5 text-xs" onClick={onDone}>
          {t('destinations.cancel')}
        </button>
      </div>
    </form>
  )
}

const localZone = (() => {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
  } catch {
    return 'UTC'
  }
})()

function QuietHoursForm() {
  const { t } = useTranslation('notifications')
  const queryClient = useQueryClient()
  const query = useQuery({ queryKey: ['quiet-hours'], queryFn: () => api.getQuietHours(), retry: false })
  const [q, setQ] = useState<QuietHours | null>(null)
  const [error, setError] = useState('')
  useEffect(() => {
    if (query.data && !q) setQ({ ...query.data, time_zone: query.data.enabled ? query.data.time_zone : localZone })
  }, [query.data, q])
  const save = useMutation({
    mutationFn: (next: QuietHours) => api.setQuietHours(next),
    onSuccess: () => {
      setError('')
      void queryClient.invalidateQueries({ queryKey: ['quiet-hours'] })
    },
    onError: (err) => setError(errorText(err)),
  })
  if (!q) return null
  const update = (patch: Partial<QuietHours>) => {
    const next = { ...q, ...patch }
    setQ(next)
    save.mutate(next)
  }
  return (
    <div className="space-y-2 border-t border-line/60 pt-4">
      <label className="flex items-center justify-between gap-3">
        <span>
          <span className="block text-sm font-medium text-ink">{t('quiet.title')}</span>
          <span className="block text-xs text-ink-muted">{t('quiet.description')}</span>
        </span>
        <input type="checkbox" checked={q.enabled} onChange={(e) => update({ enabled: e.target.checked })} aria-label={t('quiet.title')} />
      </label>
      {q.enabled && (
        <div className="grid gap-3 sm:grid-cols-[7rem_7rem_1fr]">
          <label className="block text-sm">
            <span className="text-ink-muted">{t('quiet.from')}</span>
            <input className="field mt-1 w-full" type="time" value={q.start} onChange={(e) => update({ start: e.target.value })} />
          </label>
          <label className="block text-sm">
            <span className="text-ink-muted">{t('quiet.until')}</span>
            <input className="field mt-1 w-full" type="time" value={q.end} onChange={(e) => update({ end: e.target.value })} />
          </label>
          <label className="block text-sm">
            <span className="text-ink-muted">{t('quiet.during')}</span>
            <select className="field mt-1 w-full" value={q.allow} onChange={(e) => update({ allow: e.target.value as QuietHours['allow'] })}>
              <option value="errors">{t('quiet.errors')}</option>
              <option value="nothing">{t('quiet.nothing')}</option>
            </select>
          </label>
          <p className="text-xs text-ink-faint sm:col-span-3">{t('quiet.timeZone', { zone: q.time_zone })}</p>
        </div>
      )}
      {error && <p className="text-xs text-danger">{error}</p>}
    </div>
  )
}

/** A topic that is hard to guess, for the public ntfy server. */
function randomTopic(): string {
  const bytes = new Uint8Array(8)
  crypto.getRandomValues(bytes)
  return `yggdrasil-${Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')}`
}
