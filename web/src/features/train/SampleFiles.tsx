import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '@/lib/api'
import { saveText } from '@/lib/desktopBridge'
import type { SampleFile } from '@/types/api'

async function download(file: SampleFile) {
  // The desktop app's web view can't download; its shell saves the file.
  if ((await saveText(file.filename, file.content)) !== null) return
  const url = URL.createObjectURL(new Blob([file.content], { type: 'text/plain' }))
  const a = document.createElement('a')
  a.href = url
  a.download = file.filename
  a.click()
  URL.revokeObjectURL(url)
}

/** Example material in each format, to read or download as a starting point. */
export function SampleFiles({ open = false }: { open?: boolean }) {
  const { t } = useTranslation('train')
  const samples = useQuery({ queryKey: ['training', 'samples'], queryFn: () => api.trainingSamples(), staleTime: Infinity })
  const [shown, setShown] = useState<string | null>(null)
  const [saveError, setSaveError] = useState<string | null>(null)
  return (
    <details className="text-sm" open={open}>
      <summary className="cursor-pointer text-ink">{t('samples.see')}</summary>
      <p className="mt-2 text-ink-muted">{t('samples.description')}</p>
      {saveError && <p className="mt-2 text-xs text-danger">{saveError}</p>}
      <ul className="mt-2 space-y-2">
        {(samples.data ?? []).map((f) => {
          const lines = f.content.split('\n')
          return (
            <li key={f.filename} className="rounded-lg bg-raised p-3">
              <div className="flex flex-wrap items-center gap-2">
                <span className="font-medium text-ink">{f.name}</span>
                <span className="mono-id">{f.filename}</span>
                <span className="ms-auto flex gap-1.5">
                  <button type="button" className="btn-secondary btn-sm" onClick={() => setShown(shown === f.filename ? null : f.filename)}>
                    {shown === f.filename ? t('samples.hide') : t('samples.preview')}
                  </button>
                  <button
                    type="button"
                    className="btn-secondary btn-sm"
                    onClick={() => {
                      setSaveError(null)
                      download(f).catch((err) => setSaveError(err instanceof Error ? err.message : String(err)))
                    }}
                  >
                    {t('samples.download')}
                  </button>
                </span>
              </div>
              <p className="mt-1 text-xs text-ink-muted">{f.description}</p>
              {shown === f.filename && (
                <pre className="log-panel mt-2 max-h-64 overflow-auto whitespace-pre text-xs">
                  {lines.slice(0, 12).join('\n')}
                  {lines.length > 13 ? `\n${t('samples.moreLines', { count: lines.length - 13 })}` : ''}
                </pre>
              )}
            </li>
          )
        })}
      </ul>
    </details>
  )
}
