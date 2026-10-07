import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, getApiBase } from '@/lib/api'

/**
 * An automation's webhook link (#204). Another service starts the
 * automation by calling it, so the link is its password: it's shown once,
 * when it's made, and making a new one stops the old one.
 */
export function WebhookLink({ automationId, hookSet }: { automationId: string; hookSet: boolean }) {
  const { t } = useTranslation('automations')
  const queryClient = useQueryClient()
  const [copied, setCopied] = useState(false)
  const make = useMutation({
    mutationFn: () => api.makeAutomationHook(automationId),
    onSuccess: () => {
      setCopied(false)
      void queryClient.invalidateQueries({ queryKey: ['automation', automationId] })
    },
  })
  const link = make.data ? `${getApiBase() || window.location.origin}${make.data.path}` : ''
  return (
    <div className="space-y-2">
      <p className="label-caps">{t('webhook.title')}</p>
      {link ? (
        <>
          <div className="flex items-center gap-2">
            <input className="field w-full font-mono text-xs" readOnly value={link} aria-label={t('webhook.title')} onFocus={(event) => event.target.select()} />
            <button
              type="button"
              className="btn-secondary btn-sm shrink-0"
              onClick={() => {
                void navigator.clipboard?.writeText(link).then(() => setCopied(true))
              }}
            >
              {copied ? t('webhook.copied') : t('webhook.copy')}
            </button>
          </div>
          <p className="text-xs text-ink-muted">{t('webhook.shownOnce')}</p>
          <p className="text-xs text-ink-faint">{t('webhook.howTo')}</p>
        </>
      ) : (
        <>
          <p className="text-sm text-ink-muted">{hookSet ? t('webhook.hasLink') : t('webhook.noLink')}</p>
          <button type="button" className="btn-secondary btn-sm" disabled={make.isPending} onClick={() => make.mutate()}>
            {hookSet ? t('webhook.makeNew') : t('webhook.make')}
          </button>
          {hookSet ? <p className="text-xs text-ink-faint">{t('webhook.replaces')}</p> : null}
        </>
      )}
      {make.isError ? <p className="text-xs text-danger">{make.error instanceof Error ? make.error.message : ''}</p> : null}
    </div>
  )
}
