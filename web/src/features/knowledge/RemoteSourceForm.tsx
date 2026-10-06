import { useMutation } from '@tanstack/react-query'
import { useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '@/lib/api'
import type { KnowledgeRemoteInput, KnowledgeSource } from '@/types/api'
import { errorText } from '@/features/train/display'

type Driver = NonNullable<KnowledgeRemoteInput['driver']>

// Each choice's label is knowledge:remote.refreshChoices.<key> in the catalog.
const refreshChoices = [
  { minutes: 5, key: 'm5' },
  { minutes: 15, key: 'm15' },
  { minutes: 60, key: 'h1' },
  { minutes: 360, key: 'h6' },
  { minutes: 1440, key: 'd1' },
]

// RemoteSourceForm connects a database query or a web API as knowledge.
// Passwords and tokens go to the daemon's secrets directory and are never
// shown again.
export function RemoteSourceForm({
  kind,
  beforeConnect,
  onAdded,
}: {
  kind: 'database' | 'api'
  /** Shown above the Connect button, such as which profiles will use it. */
  beforeConnect?: ReactNode
  onAdded: (source?: KnowledgeSource | null) => void
}) {
  const { t } = useTranslation('knowledge')
  const [name, setName] = useState('')
  const [driver, setDriver] = useState<Driver>('sqlite')
  const [database, setDatabase] = useState('')
  const [connection, setConnection] = useState('')
  const [query, setQuery] = useState('')
  const [url, setUrl] = useState('')
  const [items, setItems] = useState('')
  const [headerName, setHeaderName] = useState('Authorization')
  const [headerValue, setHeaderValue] = useState('')
  const [refresh, setRefresh] = useState(60)

  const remote = (): KnowledgeRemoteInput =>
    kind === 'database'
      ? {
          driver,
          ...(driver === 'sqlite' ? { database } : { connection_string: connection }),
          query,
          refresh_minutes: refresh,
        }
      : {
          url,
          items: items || undefined,
          headers: headerValue ? { [headerName]: headerValue } : undefined,
          refresh_minutes: refresh,
        }
  const add = useMutation({
    mutationFn: () => api.createKnowledge({ kind, name: name || undefined, remote: remote() }),
    // A source whose first fetch failed is still added, with its error, so
    // the list refreshes either way.
    onSuccess: (src) => {
      if (src?.status !== 'failed') {
        setConnection('')
        setHeaderValue('')
      }
      onAdded(src)
    },
  })
  const ready =
    kind === 'database' ? query.trim() !== '' && (driver === 'sqlite' ? database.trim() !== '' : connection.trim() !== '') : url.trim() !== ''

  return (
    <>
      {kind === 'database' ? (
        <>
          <label className="block space-y-1">
            <span className="text-sm text-ink">{t('remote.database')}</span>
            <select className="field w-full" value={driver} onChange={(e) => setDriver(e.target.value as Driver)}>
              <option value="sqlite">{t('remote.sqlite')}</option>
              <option value="postgres">PostgreSQL</option>
              <option value="mysql">MySQL</option>
            </select>
          </label>
          {driver === 'sqlite' ? (
            <label className="block space-y-1">
              <span className="text-sm text-ink">{t('remote.file')}</span>
              <input className="field w-full font-mono text-xs" value={database} onChange={(e) => setDatabase(e.target.value)} placeholder="~/shop/inventory.db" />
            </label>
          ) : (
            <label className="block space-y-1">
              <span className="text-sm text-ink">{t('remote.connection')}</span>
              <input
                className="field w-full font-mono text-xs"
                type="password"
                autoComplete="off"
                value={connection}
                onChange={(e) => setConnection(e.target.value)}
                placeholder={driver === 'postgres' ? 'postgres://reader:password@db.local/shop' : 'reader:password@tcp(db.local:3306)/shop'}
              />
              <span className="block text-xs text-ink-faint">{t('remote.connectionHint')}</span>
            </label>
          )}
          <label className="block space-y-1">
            <span className="text-sm text-ink">{t('remote.query')}</span>
            <textarea
              className="field min-h-20 w-full font-mono text-xs"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              // eslint-disable-next-line i18next/no-literal-string -- an example of what to type, not prose
              placeholder="SELECT sku, name, price, in_stock FROM products WHERE active"
              aria-label={t('remote.query')}
            />
            <span className="block text-xs text-ink-faint">{t('remote.queryHint')}</span>
          </label>
        </>
      ) : (
        <>
          <label className="block space-y-1">
            <span className="text-sm text-ink">{t('remote.url')}</span>
            <input className="field w-full font-mono text-xs" value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://shop.example.com/api/products" />
            <span className="block text-xs text-ink-faint">{t('remote.urlHint')}</span>
          </label>
          <label className="block space-y-1">
            <span className="text-sm text-ink">{t('remote.items')}</span>
            <input className="field w-full font-mono text-xs" value={items} onChange={(e) => setItems(e.target.value)} placeholder="data.products" />
          </label>
          <div className="grid grid-cols-[minmax(0,2fr)_minmax(0,3fr)] gap-2">
            <label className="block space-y-1">
              <span className="text-sm text-ink">{t('remote.header')}</span>
              <input className="field w-full font-mono text-xs" value={headerName} onChange={(e) => setHeaderName(e.target.value)} />
            </label>
            <label className="block space-y-1">
              <span className="text-sm text-ink">{t('remote.value')}</span>
              <input
                className="field w-full font-mono text-xs"
                type="password"
                autoComplete="off"
                value={headerValue}
                onChange={(e) => setHeaderValue(e.target.value)}
                // eslint-disable-next-line i18next/no-literal-string -- an example of what to type, not prose
                placeholder="Bearer …"
              />
            </label>
          </div>
        </>
      )}
      <label className="block space-y-1">
        <span className="text-sm text-ink">{t('remote.refresh')}</span>
        <select className="field w-full" value={refresh} onChange={(e) => setRefresh(Number(e.target.value))}>
          {refreshChoices.map((c) => (
            <option key={c.minutes} value={c.minutes}>
              {t(`remote.refreshChoices.${c.key}`)}
            </option>
          ))}
        </select>
      </label>
      <label className="block space-y-1">
        <span className="text-sm text-ink">{t('remote.name')}</span>
        <input className="field w-full" value={name} onChange={(e) => setName(e.target.value)} />
      </label>
      {beforeConnect}
      {add.error && <p className="text-sm text-danger">{errorText(add.error)}</p>}
      {add.data?.status === 'failed' && <p className="text-sm text-danger">{add.data.error}</p>}
      <button type="button" className="btn-primary" disabled={add.isPending || !ready} onClick={() => add.mutate()}>
        {add.isPending ? t('remote.connecting') : t('remote.connect')}
      </button>
    </>
  )
}
