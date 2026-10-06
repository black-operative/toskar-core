import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { Trans, useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { api } from '@/lib/api'
import { errorText } from '@/features/train/display'

/**
 * Chooses which Mimir sources a profile or specialized AI searches. It can
 * attach an existing source or connect a new file or folder in place.
 */
export function KnowledgePicker({
  selected,
  onChange,
  disabled = false,
}: {
  selected: string[]
  onChange: (ids: string[]) => void
  disabled?: boolean
}) {
  const { t } = useTranslation('knowledge')
  const queryClient = useQueryClient()
  const sources = useQuery({ queryKey: ['knowledge'], queryFn: () => api.listKnowledge() })
  const [path, setPath] = useState('')
  const connect = useMutation({
    mutationFn: () => api.createKnowledge({ kind: 'path', path }),
    onSuccess: (src) => {
      setPath('')
      void queryClient.invalidateQueries({ queryKey: ['knowledge'] })
      if (src && src.status !== 'failed') onChange([...selected, src.id])
    },
  })

  const all = sources.data ?? []
  const attached = selected.map((id) => all.find((s) => s.id === id)).filter((s) => s != null)
  const available = all.filter((s) => !selected.includes(s.id))

  return (
    <div className="space-y-2">
      {attached.length === 0 ? (
        <p className="text-sm text-ink-muted">{t('picker.none')}</p>
      ) : (
        <ul className="space-y-1">
          {attached.map((s) => (
            <li key={s.id} className="flex items-center gap-2 text-sm">
              <span className="min-w-0 flex-1 truncate text-ink" title={s.path ?? s.filename}>
                {s.name} <span className="text-xs text-ink-faint">· {t('row.passages', { count: s.chunk_count })}</span>
              </span>
              <button
                type="button"
                className="btn-secondary px-2 py-0.5 text-xs"
                disabled={disabled}
                aria-label={t('picker.disconnect', { name: s.name })}
                onClick={() => onChange(selected.filter((id) => id !== s.id))}
              >
                {t('picker.remove')}
              </button>
            </li>
          ))}
        </ul>
      )}
      {available.length > 0 && (
        <select
          className="field w-full text-sm"
          value=""
          disabled={disabled}
          aria-label={t('picker.connectExisting')}
          onChange={(e) => e.target.value && onChange([...selected, e.target.value])}
        >
          <option value="">{t('picker.connectExistingOption')}</option>
          {available.map((s) => (
            <option key={s.id} value={s.id}>
              {s.name}
            </option>
          ))}
        </select>
      )}
      <form
        className="flex gap-2"
        onSubmit={(e) => {
          e.preventDefault()
          if (path.trim()) connect.mutate()
        }}
      >
        <input
          className="field min-w-0 flex-1 font-mono text-xs"
          value={path}
          disabled={disabled}
          onChange={(e) => setPath(e.target.value)}
          placeholder={t('picker.pathPlaceholder')}
          aria-label={t('picker.path')}
        />
        <button type="submit" className="btn-secondary px-3 py-1 text-xs" disabled={disabled || !path.trim() || connect.isPending}>
          {connect.isPending ? t('picker.indexing') : t('picker.connect')}
        </button>
      </form>
      {connect.error && <p className="text-xs text-danger">{errorText(connect.error)}</p>}
      {connect.data?.status === 'failed' && <p className="text-xs text-danger">{connect.data.error}</p>}
      <p className="text-xs text-ink-faint">
        <Trans t={t} i18nKey="picker.manage" components={{ go: <Link to="/knowledge" className="underline" /> }} />
      </p>
    </div>
  )
}
