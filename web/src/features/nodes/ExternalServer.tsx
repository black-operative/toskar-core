import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '@/lib/api'

const KEY = ['external-server']

/**
 * An OpenAI-compatible server whose models can be chosen for a chat
 * (#111). Auto never picks them, and chats with them leave this computer.
 */
export function ExternalServer() {
  const { t } = useTranslation('settings')
  const queryClient = useQueryClient()
  const server = useQuery({ queryKey: KEY, queryFn: () => api.getExternalServer(), retry: false })
  // null shows the stored URL until the person edits it.
  const [edited, setEdited] = useState<string | null>(null)
  const [key, setKey] = useState('')
  const [error, setError] = useState('')
  const url = edited ?? server.data?.base_url ?? ''

  const save = useMutation({
    mutationFn: (body: { base_url: string; api_key?: string; clear_key?: boolean }) => api.setExternalServer(body),
    onSuccess: (next) => {
      setKey('')
      setEdited(null)
      setError('')
      queryClient.setQueryData(KEY, next)
      void queryClient.invalidateQueries({ queryKey: ['models'] })
    },
    onError: (err) => setError(err instanceof Error ? err.message : String(err)),
  })
  const submit = (e: FormEvent) => {
    e.preventDefault()
    save.mutate({ base_url: url.trim(), ...(key.trim() ? { api_key: key.trim() } : {}) })
  }
  const data = server.data

  return (
    <section className="card space-y-4" aria-labelledby="external-server-title">
      <div>
        <h2 id="external-server-title" className="section-title">
          {t('external.title')}
        </h2>
        <p className="mt-1 text-sm text-ink-muted">{t('external.description')}</p>
      </div>
      <form className="space-y-3" onSubmit={submit}>
        <label className="block text-sm">
          <span className="text-ink-muted">{t('external.url')}</span>
          <input className="field mt-1 w-full" type="url" placeholder="https://api.openai.com/v1" value={url} onChange={(e) => setEdited(e.target.value)} />
        </label>
        <label className="block text-sm">
          <span className="text-ink-muted">{t('external.key')}</span>
          <input
            className="field mt-1 w-full"
            type="password"
            autoComplete="off"
            placeholder={data?.has_key ? t('external.keyStored') : t('external.keyOptional')}
            value={key}
            onChange={(e) => setKey(e.target.value)}
          />
        </label>
        <div className="flex flex-wrap gap-2">
          <button type="submit" className="btn-primary px-3 py-1.5 text-xs" disabled={save.isPending}>
            {save.isPending ? t('external.saving') : t('external.save')}
          </button>
          {data?.has_key ? (
            <button type="button" className="btn-secondary px-3 py-1.5 text-xs" disabled={save.isPending} onClick={() => save.mutate({ base_url: url.trim(), clear_key: true })}>
              {t('external.forgetKey')}
            </button>
          ) : null}
        </div>
      </form>
      {error ? (
        <p role="alert" className="text-sm text-danger">
          {error}
        </p>
      ) : data?.base_url ? (
        data.error ? (
          <p role="alert" className="text-sm text-danger">
            {data.error}
          </p>
        ) : (
          <p className="text-sm text-ink-muted">{t('external.models', { count: data.models.length })}</p>
        )
      ) : null}
      <p className="text-xs text-ink-faint">{t('external.privacy')}</p>
    </section>
  )
}
