import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { Trans, useTranslation } from 'react-i18next'
import i18n from '@/i18n'
import { api } from '@/lib/api'
import type { MCPShare } from '@/types/api'
import { rovingKeyDown } from '@/lib/roving'

type AppID = 'claude-desktop' | 'claude-code' | 'cursor' | 'vscode' | 'other'

const APPS: { id: AppID; name: string }[] = [
  { id: 'claude-desktop', name: 'Claude Desktop' },
  { id: 'claude-code', name: 'Claude Code' },
  { id: 'cursor', name: 'Cursor' },
  { id: 'vscode', name: 'VS Code' },
  { id: 'other', name: '' },
]

/** What to paste into each app, and where it goes. */
function setupFor(app: AppID, share: MCPShare): { where: string; text: string; language: string } {
  const KEY = i18n.t('apiAccess:share.keyPlaceholder')
  const auth = share.needs_key ? { Authorization: `Bearer ${KEY}` } : undefined
  const json = (v: unknown) => JSON.stringify(v, null, 2)
  switch (app) {
    case 'claude-desktop': {
      // yggctl dials the default address unless told otherwise.
      const base = share.url.replace(/\/mcp$/, '')
      const env: Record<string, string> = {}
      if (base !== 'http://127.0.0.1:7331') env.YGGDRASIL_URL = base
      if (share.needs_key) env.YGGDRASIL_API_KEY = KEY
      return {
        where: i18n.t('apiAccess:share.where.claudeDesktop'),
        language: 'json',
        text: json({
          mcpServers: {
            yggdrasil: {
              command: share.command,
              args: share.args,
              ...(Object.keys(env).length > 0 ? { env } : {}),
            },
          },
        }),
      }
    }
    case 'claude-code':
      return {
        where: i18n.t('apiAccess:share.where.claudeCode'),
        language: 'bash',
        text: `claude mcp add --transport http yggdrasil ${share.url}${share.needs_key ? ` --header "Authorization: Bearer ${KEY}"` : ''}`,
      }
    case 'cursor':
      return {
        where: i18n.t('apiAccess:share.where.cursor'),
        language: 'json',
        text: json({ mcpServers: { yggdrasil: { url: share.url, ...(auth ? { headers: auth } : {}) } } }),
      }
    case 'vscode':
      return {
        where: i18n.t('apiAccess:share.where.vscode'),
        language: 'json',
        text: json({ servers: { yggdrasil: { type: 'http', url: share.url, ...(auth ? { headers: auth } : {}) } } }),
      }
    default:
      return {
        where: i18n.t('apiAccess:share.where.other'),
        language: 'text',
        text:
          i18n.t('apiAccess:share.otherText', { url: share.url, command: `${share.command} ${share.args.join(' ')}` }) +
          (share.needs_key ? i18n.t('apiAccess:share.otherHeader', { key: KEY }) : ''),
      }
  }
}

/**
 * Yggdrasil as an MCP server: other AI apps can ask the local AI, list its
 * models, and search connected knowledge, within an API key's permissions.
 */
export function ShareWithApps() {
  const { t } = useTranslation('apiAccess')
  const share = useQuery({ queryKey: ['mcp-share'], queryFn: () => api.mcpShare(), retry: false })
  const [app, setApp] = useState<AppID>('claude-desktop')
  const [copied, setCopied] = useState(false)
  // A daemon without MCP sharing, or a malformed reply, hides the section
  // instead of failing the whole page.
  if (!share.data || typeof share.data.url !== 'string' || !Array.isArray(share.data.args)) return null
  const setup = setupFor(app, share.data)
  return (
    <section className="card space-y-4" aria-label={t('share.title')}>
      <div>
        <h2 className="section-title">{t('share.title')}</h2>
        <p className="mt-1 max-w-2xl text-sm text-ink-muted">{t('share.description')}</p>
      </div>
      <div role="tablist" className="flex flex-wrap gap-1.5" onKeyDown={rovingKeyDown}>
        {APPS.map((a) => (
          <button
            key={a.id}
            type="button"
            role="tab"
            aria-selected={app === a.id}
            tabIndex={app === a.id ? 0 : -1}
            className={app === a.id ? 'btn-primary px-3 py-1.5 text-xs' : 'btn-secondary px-3 py-1.5 text-xs'}
            onClick={() => {
              setApp(a.id)
              setCopied(false)
            }}
          >
            {a.id === 'other' ? t('share.other') : a.name}
          </button>
        ))}
      </div>
      <p className="text-sm text-ink-muted">{setup.where}</p>
      <div className="relative">
        <pre className="log-panel overflow-x-auto pe-20 text-xs" aria-label={t('share.settings', { language: setup.language })}>
          {setup.text}
        </pre>
        <button
          type="button"
          className="btn-secondary absolute end-2 top-2 px-2.5 py-1 text-xs"
          onClick={() => {
            void navigator.clipboard.writeText(setup.text).then(() => setCopied(true))
          }}
        >
          {copied ? t('share.copied') : t('share.copy')}
        </button>
      </div>
      {share.data.needs_key && (
        <p className="text-xs text-ink-faint">
          <Trans t={t} i18nKey="share.needsKey" components={{ key: <code>{t('share.keyPlaceholder')}</code> }} />
        </p>
      )}
    </section>
  )
}
