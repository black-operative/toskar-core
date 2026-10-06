import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { api } from '@/lib/api'
import { isDesktopShell, saveFromDaemon } from '@/lib/desktopBridge'
import { formatBytes } from '@/lib/format'
import type { SpecializedAIView } from '@/types/api'
import { errorText, exportRevision } from '../display'

// ExportCard merges a revision into one GGUF file that other GGUF tools can
// load without Yggdrasil.
export function ExportCard({ view }: { view: SpecializedAIView }) {
  const { t } = useTranslation('train')
  const revision = exportRevision(view)
  const queryClient = useQueryClient()
  const key = ['training', view.id, 'export', revision]
  const status = useQuery({
    queryKey: key,
    queryFn: () => api.exportStatus(view.id, revision),
    enabled: revision > 0,
    refetchInterval: (q) => (q.state.data?.state === 'exporting' ? 2000 : false),
  })
  const refresh = () => void queryClient.invalidateQueries({ queryKey: key })
  const start = useMutation({ mutationFn: () => api.startExport(view.id, revision), onSuccess: refresh })
  const remove = useMutation({ mutationFn: () => api.deleteExport(view.id, revision), onSuccess: refresh })
  // The desktop app's web view can't download; its shell streams the file to disk.
  const save = useMutation({
    mutationFn: (filename: string) => saveFromDaemon(filename, api.exportFilePath(view.id, revision)),
  })
  if (revision === 0) return null

  const st = status.data
  const error = start.error ?? remove.error ?? save.error ?? status.error
  return (
    <div className="card space-y-3">
      <h3 className="section-title">{t('export.title')}</h3>
      <p className="text-sm text-ink-muted">{t('export.description', { revision })}</p>
      {st?.state === 'exporting' && (
        <p className="text-sm text-ink" role="status">
          {t('export.exporting')} {st.size_bytes ? t('export.upTo', { size: formatBytes(st.size_bytes) }) : ''}
        </p>
      )}
      {st?.state === 'failed' && <p className="text-sm text-danger">{t('export.failed', { error: st.error })}</p>}
      {st?.state === 'ready' && (
        <div className="flex flex-wrap items-center gap-2 text-sm">
          <a
            className="btn-primary btn-sm"
            href={api.exportFileUrl(view.id, revision)}
            download={st.filename}
            aria-disabled={save.isPending}
            onClick={(e) => {
              if (!isDesktopShell()) return
              e.preventDefault()
              if (!save.isPending && st.filename) save.mutate(st.filename)
            }}
          >
            {save.isPending ? t('export.saving') : t('export.download', { filename: st.filename })}
          </a>
          <span className="text-xs text-ink-muted">{formatBytes(st.size_bytes)}</span>
          {save.data && <span className="text-xs text-ink-muted">{t('export.savedTo', { path: save.data })}</span>}
          <button type="button" className="btn-secondary btn-sm" disabled={remove.isPending} onClick={() => remove.mutate()}>
            {t('export.deleteFile')}
          </button>
        </div>
      )}
      {(st?.state === 'none' || st?.state === 'failed') && (
        <button type="button" className="btn-secondary btn-sm" disabled={start.isPending} onClick={() => start.mutate()}>
          {st.state === 'failed' ? t('export.tryAgain') : t('export.export')}
        </button>
      )}
      {st?.state === 'exporting' && (
        <button type="button" className="btn-secondary btn-sm" disabled={remove.isPending} onClick={() => remove.mutate()}>
          {t('export.cancel')}
        </button>
      )}
      {st?.state === 'ready' && st.instructions && (
        <details className="text-xs text-ink-muted">
          <summary className="cursor-pointer">{t('export.instructions')}</summary>
          <pre className="log-panel mt-2 whitespace-pre-wrap">{st.instructions}</pre>
        </details>
      )}
      {error && <p className="text-sm text-danger">{errorText(error)}</p>}
    </div>
  )
}
