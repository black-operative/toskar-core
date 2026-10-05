import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import i18n from '@/i18n'
import { api } from '@/lib/api'
import type { Connector } from '@/types/api'

const KEY = ['connectors'] as const

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : i18n.t('services:generic')
}

/**
 * Connected services (spec §32): GitHub, Home Assistant, and so on. Tokens
 * are sent once to this computer's Yggdrasil service and stored outside the
 * database. They are never shown again or given to a model; tools use them
 * when they run.
 */
export function ConnectedServices() {
  const { t } = useTranslation('services')
  const query = useQuery({ queryKey: KEY, queryFn: () => api.listConnectors(), retry: false })
  const services = query.data ?? []
  return (
    <section className="card space-y-4">
      <div>
        <h2 className="section-title">{t('title')}</h2>
        <p className="mt-1 text-sm text-ink-muted">{t('description')}</p>
      </div>
      {query.isError && <p className="text-sm text-danger">{errorText(query.error)}</p>}
      {services.map((service) => (
        <ServiceRow key={service.id} service={service} />
      ))}
    </section>
  )
}

function ServiceRow({ service }: { service: Connector }) {
  const { t } = useTranslation('services')
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState(false)
  const [values, setValues] = useState<Record<string, string>>({})
  const [error, setError] = useState('')
  const refresh = () => queryClient.invalidateQueries({ queryKey: KEY })

  const connect = useMutation({
    mutationFn: () => api.connectService(service.id, values),
    onSuccess: () => {
      setEditing(false)
      setValues({})
      setError('')
      void refresh()
    },
    onError: (err) => setError(errorText(err)),
  })
  const check = useMutation({
    mutationFn: () => api.checkConnector(service.id),
    onSettled: () => void refresh(),
    onError: (err) => setError(errorText(err)),
  })
  const disconnect = useMutation({
    mutationFn: () => api.disconnectService(service.id),
    onSuccess: () => {
      setError('')
      void refresh()
    },
    onError: (err) => setError(errorText(err)),
  })

  const statusLine = service.connected
    ? service.status === 'error'
      ? t('needsAttention', { error: service.error ?? t('lastCheckFailed') })
      : t('connectedAs', { account: service.account || service.name })
    : t('notConnected')

  return (
    <div className="rounded-lg border border-line/60 p-3">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="min-w-0">
          <p className="text-sm font-medium text-ink">{service.name}</p>
          <p className="text-xs text-ink-muted">{service.description}</p>
          <p className={['mt-1 text-xs', service.status === 'error' ? 'text-danger' : 'text-ink-faint'].join(' ')}>
            {statusLine}
          </p>
        </div>
        <div className="flex shrink-0 gap-2">
          {service.connected && !editing && (
            <>
              <button type="button" className="btn-secondary px-3 py-1.5 text-xs" disabled={check.isPending} onClick={() => check.mutate()}>
                {check.isPending ? t('checking') : t('check')}
              </button>
              <button type="button" className="btn-secondary px-3 py-1.5 text-xs" onClick={() => setEditing(true)}>
                {t('change')}
              </button>
              <button
                type="button"
                className="btn-secondary px-3 py-1.5 text-xs"
                disabled={disconnect.isPending}
                onClick={() => disconnect.mutate()}
              >
                {t('disconnect')}
              </button>
            </>
          )}
          {!service.connected && !editing && (
            <button type="button" className="btn-primary px-3 py-1.5 text-xs" onClick={() => setEditing(true)}>
              {t('connect')}
            </button>
          )}
        </div>
      </div>

      {editing && (
        <form
          className="mt-3 space-y-3"
          onSubmit={(e) => {
            e.preventDefault()
            connect.mutate()
          }}
        >
          <p className="rounded-md bg-info/10 px-2.5 py-2 text-xs leading-relaxed text-ink">{service.scopes}</p>
          {service.fields.map((field) => {
            const stored = service.values?.[field.key]
            return (
              <label key={field.key} className="block text-sm">
                <span className="text-ink-muted">
                  {field.label}
                  {field.optional && <span className="text-ink-faint">{t('optional')}</span>}
                </span>
                <input
                  className="field mt-1 w-full"
                  type={field.secret ? 'password' : 'text'}
                  autoComplete="off"
                  spellCheck={false}
                  placeholder={field.secret && stored ? t('stored', { value: stored }) : field.placeholder}
                  defaultValue={field.secret ? '' : stored ?? ''}
                  onChange={(e) => setValues((v) => ({ ...v, [field.key]: e.target.value }))}
                />
                {field.help && <span className="mt-1 block text-xs text-ink-faint">{field.help}</span>}
              </label>
            )
          })}
          <div className="flex gap-2">
            <button type="submit" className="btn-primary px-3 py-1.5 text-xs" disabled={connect.isPending}>
              {connect.isPending ? t('checking') : service.connected ? t('save') : t('connect')}
            </button>
            <button
              type="button"
              className="btn-secondary px-3 py-1.5 text-xs"
              onClick={() => {
                setEditing(false)
                setValues({})
                setError('')
              }}
            >
              {t('cancel')}
            </button>
          </div>
        </form>
      )}
      {error && <p className="mt-2 text-xs text-danger">{error}</p>}
      {service.connected && service.tools.length > 0 && (
        <p className="mt-2 text-xs text-ink-faint">
          {t('tools', {
            tools: service.tools
              .map((tool) => (tool.default_policy === 'ask' ? t('asksFirst', { name: tool.name }) : tool.name))
              .join(', '),
          })}
        </p>
      )}
    </div>
  )
}
