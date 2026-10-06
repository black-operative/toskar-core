import { useTranslation } from 'react-i18next'
import { useMutation, useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { LoadingSpinner } from '@/components/ui/LoadingSpinner'
import { api } from '@/lib/api'
import { formatBytes } from '@/lib/format'
import type { BrowseModel } from '@/types/api'

export function BrowseAllPanel({
  installTarget,
  onClose,
  onInstalled,
}: {
  /** Resolved from the page-level Models for: selector. */
  installTarget?: string
  onClose: () => void
  onInstalled: () => void
}) {
  const { t } = useTranslation('models')
  const [query, setQuery] = useState('gguf instruct')
  const [submitted, setSubmitted] = useState('gguf instruct')

  const browseQuery = useQuery({
    queryKey: ['models-browse', submitted],
    queryFn: () => api.browseModels(submitted, 30),
    retry: false,
  })

  const installMutation = useMutation({
    mutationFn: (item: BrowseModel) =>
      api.installModelFromURL({
        source_url: item.source_url,
        display_name: item.display_name,
        id: item.id,
        filename: item.filename,
        size_bytes: item.size_bytes,
        parameters: item.parameters,
        variant: item.variant,
        tags: item.tags,
        node_id: installTarget,
      }),
    onSuccess: () => onInstalled(),
  })

  return (
    <div className="space-y-4 rounded-xl border border-line bg-surface p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="section-title">{t('browse.title')}</h2>
          <p className="mt-1 text-sm text-ink-muted">{t('browse.description')}</p>
        </div>
        <button type="button" className="btn-secondary text-sm" onClick={onClose}>
          {t('browse.close')}
        </button>
      </div>

      <form
        className="flex flex-wrap gap-2"
        onSubmit={(e) => {
          e.preventDefault()
          setSubmitted(query.trim() || 'gguf')
        }}
      >
        <input
          className="field min-w-[220px] flex-1"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder={t('browse.placeholder')}
        />
        <button type="submit" className="btn-primary">
          {t('browse.search')}
        </button>
      </form>

      {browseQuery.isLoading && <LoadingSpinner label={t('browse.searching')} />}
      {browseQuery.isError && (
        <p className="text-sm text-danger">{t('browse.failed')}</p>
      )}

      <ul className="divide-y divide-line rounded-xl border border-line">
        {(browseQuery.data ?? []).map((item) => (
          <li
            key={item.id}
            className="flex min-w-0 flex-wrap items-center justify-between gap-3 px-4 py-3"
          >
            <div className="min-w-0">
              <p className="font-medium text-ink">{item.display_name}</p>
              <p className="mt-0.5 truncate text-xs text-ink-muted" title={item.repo_id}>
                {item.repo_id}
                {item.size_bytes ? ` · ${formatBytes(item.size_bytes)}` : ''}
                {item.variant ? ` · ${item.variant}` : ''}
              </p>
            </div>
            <button
              type="button"
              className="btn-primary btn-sm shrink-0"
              disabled={installMutation.isPending}
              onClick={() => installMutation.mutate(item)}
            >
              {t('browse.install')}
            </button>
          </li>
        ))}
      </ul>
      {installMutation.isError && (
        <p className="text-sm text-danger">
          {(installMutation.error as Error)?.message || t('browse.installFailed')}
        </p>
      )}
    </div>
  )
}
