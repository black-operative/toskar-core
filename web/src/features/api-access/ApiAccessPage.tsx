import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { Trans, useTranslation } from 'react-i18next'
import i18n from '@/i18n'
import { LoadingSpinner } from '@/components/ui/LoadingSpinner'
import { api, forgetApiKey, rememberApiKey, storedApiKey } from '@/lib/api'
import { useDialog } from '@/lib/useDialog'
import { formatLastUsed } from '@/features/models/modelPresentation'
import { useUIStore } from '@/stores/uiStore'
import type { APIKeyRecord } from '@/types/api'
import { KeyPermissions } from './KeyPermissions'
import { RealmKicker } from '@/components/ui/Realm'
import { ShareWithApps } from './ShareWithApps'
import { ConnectPhone } from '@/features/nodes/ConnectPhone'
import { formatDate } from '@/i18n/format'

type ProbeState = 'checking' | 'ok' | 'fail'

type ApiProbeResult = {
  healthOk: boolean
  openaiOk: boolean
  openaiStatus: number | null
  detail: string
}

async function probeLocalApi(lanEnabled: boolean): Promise<ApiProbeResult> {
  let healthOk: boolean
  try {
    const health = await api.getHealth()
    healthOk = health?.status === 'ok'
    if (!healthOk) {
      return {
        healthOk: false,
        openaiOk: false,
        openaiStatus: null,
        detail: i18n.t('apiAccess:probe.notReady'),
      }
    }
  } catch {
    return {
      healthOk: false,
      openaiOk: false,
      openaiStatus: null,
      detail: i18n.t('apiAccess:probe.unreachable'),
    }
  }

  let openaiStatus: number | null
  try {
    const headers: Record<string, string> = { Accept: 'application/json' }
    const key = storedApiKey()
    if (key) headers.Authorization = `Bearer ${key}`
    const res = await fetch('/v1/models', {
      method: 'GET',
      headers,
    })
    openaiStatus = res.status
  } catch {
    return {
      healthOk,
      openaiOk: false,
      openaiStatus: null,
      detail: i18n.t('apiAccess:probe.openaiDown'),
    }
  }

  const openaiOk = lanEnabled
    ? openaiStatus === 200 || openaiStatus === 401
    : openaiStatus === 200

  let detail = i18n.t('apiAccess:probe.passed')
  if (!openaiOk) {
    detail =
      openaiStatus != null
        ? i18n.t('apiAccess:probe.openaiStatus', { status: openaiStatus })
        : i18n.t('apiAccess:probe.openaiNoResponse')
  } else if (lanEnabled && openaiStatus === 401) {
    detail = i18n.t('apiAccess:probe.needsKey')
  }

  return { healthOk, openaiOk, openaiStatus, detail }
}

function lanUrlFromNodeAddress(address: string | undefined, port: number): string | null {
  if (!address) return null
  const host = address.split(':')[0]?.trim()
  if (!host || host === '127.0.0.1' || host === 'localhost' || host === '::1') {
    return null
  }
  return `http://${host}:${port}/v1`
}

function listensBeyondLoopback(host: string): boolean {
  const normalized = host.trim().toLowerCase().replace(/^\[|\]$/g, '')
  return normalized !== '' && normalized !== 'localhost' && normalized !== '127.0.0.1' && normalized !== '::1'
}

function maskPrefix(prefix: string): string {
  const tip = prefix.slice(-4) || prefix
  return `••••••••••${tip}`
}

export function ApiAccessPage() {
  const { t } = useTranslation('apiAccess')
  const queryClient = useQueryClient()
  const advancedMode = useUIStore((s) => s.advancedMode)
  const [newKeyName, setNewKeyName] = useState('')
  const [revealedSecret, setRevealedSecret] = useState<string | null>(null)
  const [revealedKeyId, setRevealedKeyId] = useState<string | null>(null)
  const [copiedField, setCopiedField] = useState<string | null>(null)
  const [probeTick, setProbeTick] = useState(0)
  const [lanConfirmOpen, setLanConfirmOpen] = useState(false)
  const lanDialogRef = useDialog(lanConfirmOpen, () => setLanConfirmOpen(false))
  const [browserHasKey, setBrowserHasKey] = useState(() => Boolean(storedApiKey()))
  const [dialogKeyName, setDialogKeyName] = useState(() => t('lan.defaultKeyName'))
  const [docsOpen, setDocsOpen] = useState(false)
  const [showCreate, setShowCreate] = useState(false)
  const [phoneOpen, setPhoneOpen] = useState(false)

  const settingsQuery = useQuery({
    queryKey: ['settings'],
    queryFn: () => api.getSettings(),
    retry: false,
  })

  const keysQuery = useQuery({
    queryKey: ['api-keys'],
    queryFn: () => api.listApiKeys(),
    retry: false,
  })

  const nodesQuery = useQuery({
    queryKey: ['nodes'],
    queryFn: () => api.getNodes(),
    retry: false,
    staleTime: 30_000,
  })

  const lanEnabled = settingsQuery.data?.lan_api_enabled ?? false
  const port = settingsQuery.data?.api_port ?? 7331
  const bindHost = settingsQuery.data?.api_host ?? '127.0.0.1'

  const localEndpoint = `http://localhost:${port}/v1`
  const friendlyLocal = `localhost:${port}`

  const lanEndpoint = useMemo(() => {
    const local = (nodesQuery.data ?? []).find((n) => n.is_local)
    return lanUrlFromNodeAddress(local?.address, port)
  }, [nodesQuery.data, port])

  const probeQuery = useQuery({
    queryKey: ['api-access-probe', lanEnabled, probeTick],
    queryFn: () => probeLocalApi(lanEnabled),
    retry: false,
    refetchInterval: 8_000,
  })

  const updateSettingsMutation = useMutation({
    mutationFn: (enabled: boolean) =>
      api.updateSettings({ lan_api_enabled: enabled }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['settings'] })
      setProbeTick((n) => n + 1)
      setLanConfirmOpen(false)
    },
  })

  const createKeyMutation = useMutation({
    mutationFn: (name: string) => api.createApiKey(name),
    onSuccess: (result) => {
      if (result?.secret) {
        setRevealedSecret(result.secret)
        setRevealedKeyId(result.key?.id ?? null)
        rememberApiKey(result.secret, result.key?.id)
        setBrowserHasKey(true)
      }
      setNewKeyName('')
      setShowCreate(false)
      queryClient.invalidateQueries({ queryKey: ['api-keys'] })
    },
  })

  const rotateKeyMutation = useMutation({
    mutationFn: (id: string) => api.rotateApiKey(id),
    onSuccess: (result) => {
      if (result?.secret) {
        setRevealedSecret(result.secret)
        setRevealedKeyId(result.key?.id ?? null)
        rememberApiKey(result.secret, result.key?.id)
        setBrowserHasKey(true)
      }
      queryClient.invalidateQueries({ queryKey: ['api-keys'] })
    },
  })

  const deleteKeyMutation = useMutation({
    mutationFn: (id: string) => api.deleteApiKey(id),
    onSuccess: (_data, id) => {
      if (revealedKeyId === id) {
        setRevealedSecret(null)
        setRevealedKeyId(null)
      }
      forgetApiKey(id)
      setBrowserHasKey(Boolean(storedApiKey()))
      queryClient.invalidateQueries({ queryKey: ['api-keys'] })
    },
  })

  const settings = settingsQuery.data
  const keys = keysQuery.data ?? []
  const allActive = keys.filter((k) => !k.revoked)
  // A phone's key from Connect a phone is listed under Phones (#216).
  const phones = allActive.filter((k) => k.kind === 'device')
  const activeKeys = allActive.filter((k) => k.kind !== 'device')
  const probe = probeQuery.data
  const probeState: ProbeState = probeQuery.isLoading
    ? 'checking'
    : probe && probe.healthOk && probe.openaiOk
      ? 'ok'
      : 'fail'

  const serviceLabel =
    probeState === 'ok' ? t('service.running') : probeState === 'fail' ? t('service.notResponding') : t('service.checking')
  const accessLabel = lanEnabled ? t('service.localNetwork') : t('service.thisComputer')
  const authRequired = lanEnabled || listensBeyondLoopback(bindHost)

  const copyText = async (field: string, value: string) => {
    try {
      await navigator.clipboard.writeText(value)
      setCopiedField(field)
      setTimeout(() => setCopiedField(null), 2000)
    } catch {
      // ignore
    }
  }

  const runTest = () => {
    setProbeTick((n) => n + 1)
    void queryClient.invalidateQueries({ queryKey: ['api-access-probe'] })
  }

  const requestLanEnable = (enabled: boolean) => {
    if (enabled) {
      setLanConfirmOpen(true)
      return
    }
    updateSettingsMutation.mutate(false)
  }

  return (
    <div className="mx-auto w-full max-w-2xl min-w-0 space-y-6">
      <header className="page-header">
        <RealmKicker />
        <h1 className="page-title">{t('page.title')}</h1>
        <p className="page-subtitle">{t('page.subtitle')}</p>
      </header>

      {(settingsQuery.isLoading || keysQuery.isLoading) && (
        <LoadingSpinner label={t('page.loading')} />
      )}

      <section className="card space-y-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h2 className="section-title">{t('service.title')}</h2>
            <dl className="mt-3 space-y-2 text-sm">
              <div className="flex items-center gap-2">
                <dt className="text-ink-muted">{t('service.api')}</dt>
                <dd className="flex items-center gap-2 font-medium text-ink">
                  <span
                    className={[
                      'h-2 w-2 rounded-full',
                      probeState === 'ok'
                        ? 'bg-success'
                        : probeState === 'fail'
                          ? 'bg-danger'
                          : 'animate-pulse bg-warning',
                    ].join(' ')}
                    aria-hidden
                  />
                  {serviceLabel}
                </dd>
              </div>
              <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
                <dt className="text-ink-muted">{t('service.access')}</dt>
                <dd className="font-medium text-ink">{accessLabel}</dd>
              </div>
              <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
                <dt className="text-ink-muted">{t('service.auth')}</dt>
                <dd className="font-medium text-ink">{authRequired ? t('service.keyRequired') : t('service.notRequired')}</dd>
              </div>
            </dl>
            {probeQuery.isError || probeState === 'fail' ? (
              <p className="mt-2 text-sm text-danger">
                {probeQuery.isError
                  ? t('probe.checkFailed')
                  : (probe?.detail ?? t('probe.failed'))}
              </p>
            ) : probe?.detail && probeState === 'ok' && lanEnabled ? (
              <p className="mt-2 text-xs text-ink-muted">{probe.detail}</p>
            ) : null}
          </div>
          <button
            type="button"
            className="btn-secondary btn-sm shrink-0"
            onClick={runTest}
            disabled={probeQuery.isFetching}
          >
            {probeQuery.isFetching ? t('service.testing') : t('service.test')}
          </button>
        </div>

        <div className="space-y-2">
          <p className="text-xs font-medium uppercase tracking-wide text-ink-faint">{t('service.endpoint')}</p>
          <CopyField
            value={localEndpoint}
            copied={copiedField === 'local-endpoint'}
            onCopy={() => void copyText('local-endpoint', localEndpoint)}
          />
          <p className="text-xs text-ink-muted">
            <Trans
              t={t}
              i18nKey="service.localAddress"
              values={{ address: friendlyLocal }}
              components={{ mono: <span className="font-mono text-ink" /> }}
            />
          </p>
          {lanEnabled && (
            <div className="space-y-1 pt-1">
              <p className="text-xs font-medium uppercase tracking-wide text-ink-faint">{t('service.lanEndpoint')}</p>
              {lanEndpoint ? (
                <CopyField
                  value={lanEndpoint}
                  copied={copiedField === 'lan-endpoint'}
                  onCopy={() => void copyText('lan-endpoint', lanEndpoint)}
                />
              ) : (
                <p className="text-sm text-ink-muted">
                  <Trans
                    t={t}
                    i18nKey="service.lanHint"
                    values={{ port }}
                    components={{ mono: <span className="font-mono text-ink" /> }}
                  />
                </p>
              )}
            </div>
          )}
        </div>

        <button
          type="button"
          className="text-sm font-medium text-primary underline-offset-2 hover:underline"
          onClick={() => setDocsOpen((o) => !o)}
          aria-expanded={docsOpen}
        >
          {docsOpen ? t('service.hideDocs') : t('service.docs')}
        </button>
        {docsOpen && (
          <div className="rounded-lg bg-raised/60 px-4 py-3 text-sm text-ink-muted">
            <p>
              <Trans
                t={t}
                i18nKey="service.docsBody"
                components={{
                  mono: <span className="font-mono text-ink" />,
                  header: <span className="font-mono text-ink">Authorization: Bearer &lt;api-key&gt;</span>,
                }}
              />
            </p>
            <pre className="mt-3 overflow-x-auto rounded-md bg-canvas px-3 py-2 font-mono text-[11px] text-ink">
              {`curl ${localEndpoint}/models \\
  -H "Authorization: Bearer YOUR_KEY"`}
            </pre>
          </div>
        )}
      </section>

      <ShareWithApps />

      <section className="card space-y-4">
        <div className="flex items-start justify-between gap-4">
          <div>
            <h2 className="section-title">{t('lan.title')}</h2>
            <p className="mt-1 text-sm leading-relaxed text-ink-muted">{lanEnabled ? t('lan.on') : t('lan.off')}</p>
          </div>
          <Toggle
            label={t('lan.title')}
            checked={lanEnabled}
            disabled={updateSettingsMutation.isPending}
            onChange={() => requestLanEnable(!lanEnabled)}
          />
        </div>

        {lanEnabled && allActive.length === 0 && (
          <div className="rounded-lg border border-warning/30 bg-warning/10 px-4 py-3 text-sm text-ink">
            {t('lan.noKeys')}
          </div>
        )}

        {lanEnabled && (
          <p className="text-xs text-ink-faint">{t('lan.notes')}</p>
        )}
      </section>

      {phoneOpen ? <ConnectPhone onClose={() => setPhoneOpen(false)} /> : null}

      <section className="card space-y-4">
        <div className="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h2 className="section-title">{t('phones.title')}</h2>
            <p className="mt-1 text-sm text-ink-muted">{t('phones.description')}</p>
          </div>
          {!phoneOpen && (
            <button type="button" className="btn-primary btn-sm" onClick={() => setPhoneOpen(true)}>
              {t('phone.open', { ns: 'computers' })}
            </button>
          )}
        </div>
        {phones.length === 0 && !keysQuery.isLoading ? (
          <p className="text-sm text-ink-muted">{t('phones.none')}</p>
        ) : (
          <ul className="divide-y divide-line">
            {phones.map((phone) => (
              <li key={phone.id} className="flex flex-wrap items-center justify-between gap-3 py-3">
                <div className="min-w-0">
                  <p className="font-medium text-ink">{phone.name}</p>
                  <p className="mt-0.5 text-xs text-ink-muted">
                    {t('phones.meta', {
                      connected: formatDate(phone.created_at, { month: 'short', day: 'numeric' }),
                      lastUsed: formatLastUsed(phone.last_used_at),
                    })}
                  </p>
                </div>
                <button
                  type="button"
                  className="btn-danger btn-sm"
                  disabled={deleteKeyMutation.isPending}
                  onClick={() => {
                    if (window.confirm(t('phones.confirmDisconnect', { name: phone.name }))) {
                      deleteKeyMutation.mutate(phone.id)
                    }
                  }}
                >
                  {t('phones.disconnect')}
                </button>
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className="card space-y-4">
        <div className="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h2 className="section-title">{t('keys.title')}</h2>
            <p className="mt-1 text-sm text-ink-muted">{t('keys.description')}</p>
          </div>
          {!showCreate && (
            <button
              type="button"
              className="btn-primary btn-sm"
              onClick={() => setShowCreate(true)}
            >
              {t('keys.create')}
            </button>
          )}
        </div>

        {showCreate && (
          <div className="space-y-3 rounded-lg border border-line bg-raised/40 px-4 py-3">
            <div>
              <h3 className="text-sm font-semibold text-ink">{t('keys.create')}</h3>
              <p className="mt-1 text-sm text-ink-muted">{t('keys.createHint')}</p>
            </div>
            <input
              type="text"
              value={newKeyName}
              onChange={(e) => setNewKeyName(e.target.value)}
              placeholder={t('keys.namePlaceholder')}
              className="field w-full"
              autoFocus
            />
            <div className="flex gap-2">
              <button
                type="button"
                className="btn-primary btn-sm"
                disabled={createKeyMutation.isPending || !newKeyName.trim()}
                onClick={() => createKeyMutation.mutate(newKeyName.trim())}
              >
                {createKeyMutation.isPending ? t('keys.creating') : t('keys.createKey')}
              </button>
              <button
                type="button"
                className="btn-secondary btn-sm"
                onClick={() => {
                  setShowCreate(false)
                  setNewKeyName('')
                }}
              >
                {t('keys.cancel')}
              </button>
            </div>
          </div>
        )}

        {revealedSecret && (
          <div className="rounded-lg border border-primary/30 bg-primary-soft px-4 py-3">
            <p className="text-xs font-medium uppercase tracking-wide text-ink-muted">
              {t('keys.copyNow')}
            </p>
            <code className="mt-2 block break-all font-mono text-sm text-ink">
              {revealedSecret}
            </code>
            <button
              type="button"
              className="btn-secondary btn-sm mt-3"
              onClick={() => void copyText('secret', revealedSecret)}
            >
              {copiedField === 'secret' ? t('keys.copiedBang') : t('keys.copySecret')}
            </button>
          </div>
        )}

        {activeKeys.length === 0 && !keysQuery.isLoading && (
          <p className="text-sm text-ink-muted">{t('keys.none')}</p>
        )}

        {activeKeys.length > 0 && (
          <ul className="divide-y divide-line">
            {activeKeys.map((key: APIKeyRecord) => (
              <li key={key.id} className="flex flex-wrap items-start justify-between gap-3 py-3">
                <div className="min-w-0">
                  <p className="font-medium text-ink">{key.name}</p>
                  <p className="mt-0.5 text-xs text-ink-muted">
                    {t('keys.meta', {
                      created: formatDate(key.created_at, { month: 'short', day: 'numeric' }),
                      lastUsed: formatLastUsed(key.last_used_at),
                    })}
                  </p>
                  <p className="mt-1 font-mono text-xs text-ink-faint">
                    {maskPrefix(key.prefix)}
                  </p>
                </div>
                <div className="flex flex-wrap gap-2">
                  {revealedKeyId === key.id && revealedSecret ? (
                    <button
                      type="button"
                      className="btn-secondary btn-sm"
                      onClick={() => void copyText(`key-${key.id}`, revealedSecret)}
                    >
                      {copiedField === `key-${key.id}` ? t('keys.copiedBang') : t('keys.copy')}
                    </button>
                  ) : null}
                  <button
                    type="button"
                    className="btn-secondary btn-sm"
                    disabled={rotateKeyMutation.isPending}
                    onClick={() => {
                      if (window.confirm(t('keys.confirmRotate', { name: key.name }))) {
                        rotateKeyMutation.mutate(key.id)
                      }
                    }}
                  >
                    {t('keys.rotate')}
                  </button>
                  <button
                    type="button"
                    className="btn-danger btn-sm"
                    disabled={deleteKeyMutation.isPending}
                    onClick={() => {
                      if (window.confirm(t('keys.confirmRevoke', { name: key.name }))) {
                        deleteKeyMutation.mutate(key.id)
                      }
                    }}
                  >
                    {t('keys.revoke')}
                  </button>
                </div>
                <KeyPermissions apiKey={key} />
              </li>
            ))}
          </ul>
        )}
      </section>

      {advancedMode && settings && (
        <section className="card space-y-3">
          <h2 className="section-title">{t('advanced.title')}</h2>
          <dl className="space-y-2 text-sm">
            <div className="flex justify-between gap-3">
              <dt className="text-ink-muted">{t('advanced.bind')}</dt>
              <dd className="font-mono text-ink">
                {bindHost}:{port}
              </dd>
            </div>
            <div className="flex justify-between gap-3">
              <dt className="text-ink-muted">{t('advanced.port')}</dt>
              <dd className="font-mono text-ink">{port}</dd>
            </div>
            <div className="flex justify-between gap-3">
              <dt className="text-ink-muted">{t('advanced.host')}</dt>
              <dd className="font-mono text-ink">{settings.api_host}</dd>
            </div>
          </dl>
        </section>
      )}

      {lanConfirmOpen && (
        <div
          ref={lanDialogRef}
          className="fixed inset-0 z-40 flex items-center justify-center scrim p-4"
          role="dialog"
          aria-modal="true"
          aria-labelledby="lan-confirm-title"
        >
          <div className="w-full max-w-md rounded-panel border border-line bg-surface p-5 shadow-panel">
            <h2 id="lan-confirm-title" className="font-display text-lg font-semibold text-ink">
              {t('lan.confirmTitle')}
            </h2>
            <p className="mt-2 text-sm leading-relaxed text-ink-muted">{t('lan.confirmBody')}</p>
            <p className="mt-2 text-sm leading-relaxed text-ink-muted">{t('lan.confirmHttp')}</p>
            {!browserHasKey && (
              <div className="mt-4 space-y-2">
                <p className="text-sm text-ink">{t('lan.browserKey')}</p>
                <label className="block text-sm font-medium text-ink" htmlFor="lan-key-name">
                  {t('lan.keyName')}
                </label>
                <input
                  id="lan-key-name"
                  type="text"
                  value={dialogKeyName}
                  onChange={(e) => setDialogKeyName(e.target.value)}
                  className="field w-full"
                />
                <button
                  type="button"
                  className="btn-secondary"
                  disabled={createKeyMutation.isPending || !dialogKeyName.trim()}
                  onClick={() => createKeyMutation.mutate(dialogKeyName.trim())}
                >
                  {createKeyMutation.isPending ? t('keys.creating') : t('keys.createKey')}
                </button>
              </div>
            )}
            {revealedSecret && (
              <p className="mt-3 break-all font-mono text-xs text-ink">{revealedSecret}</p>
            )}
            {updateSettingsMutation.isError && (
              <p className="mt-3 text-sm text-warning">
                {updateSettingsMutation.error instanceof Error
                  ? updateSettingsMutation.error.message
                  : t('lan.enableFailed')}
              </p>
            )}
            <div className="mt-5 flex justify-end gap-2">
              <button
                type="button"
                className="btn-secondary"
                data-autofocus
                onClick={() => setLanConfirmOpen(false)}
                disabled={updateSettingsMutation.isPending}
              >
                {t('lan.cancel')}
              </button>
              <button
                type="button"
                className="btn-primary"
                disabled={
                  updateSettingsMutation.isPending || activeKeys.length === 0 || !browserHasKey
                }
                onClick={() => updateSettingsMutation.mutate(true)}
              >
                {updateSettingsMutation.isPending ? t('lan.enabling') : t('lan.allow')}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

function CopyField({
  value,
  copied,
  onCopy,
}: {
  value: string
  copied: boolean
  onCopy: () => void
}) {
  const { t } = useTranslation('apiAccess')
  return (
    <div className="flex min-w-0 items-stretch gap-2">
      <code className="field min-w-0 flex-1 truncate py-2 font-mono text-xs text-ink">
        {value}
      </code>
      <button type="button" className="btn-secondary btn-sm shrink-0" onClick={onCopy}>
        {copied ? t('service.copied') : t('service.copyUrl')}
      </button>
    </div>
  )
}

function Toggle({
  label,
  checked,
  disabled,
  onChange,
}: {
  label: string
  checked: boolean
  disabled?: boolean
  onChange: () => void
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      disabled={disabled}
      onClick={onChange}
      className={[
        'relative inline-flex h-7 w-12 shrink-0 rounded-full transition disabled:opacity-50',
        checked ? 'bg-primary' : 'bg-raised',
      ].join(' ')}
    >
      <span
        className={[
          'absolute top-0.5 h-6 w-6 rounded-full bg-[#EEF2F6] shadow transition',
          checked ? 'start-[22px]' : 'start-0.5',
        ].join(' ')}
      />
    </button>
  )
}
