import { useTranslation } from 'react-i18next'
import { useDialog } from '@/lib/useDialog'

export function TightFitDialog({
  modelName,
  onCancel,
  onInstall,
}: {
  modelName: string
  onCancel: () => void
  onInstall: () => void
}) {
  const { t } = useTranslation('models')
  const ref = useDialog(true, onCancel)
  return (
    <div
      className="fixed inset-0 z-50 flex items-end justify-center bg-ink/40 p-4 sm:items-center"
      role="presentation"
      onClick={onCancel}
    >
      <div
        ref={ref}
        className="card w-full max-w-md space-y-4 border-s-4 border-danger shadow-panel"
        role="dialog"
        aria-modal="true"
        aria-labelledby="tight-fit-title"
        onClick={(event) => event.stopPropagation()}
      >
        <div className="flex gap-3">
          <span
            className="mt-0.5 inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-danger/15 text-sm font-semibold text-danger"
            aria-hidden
          >
            !
          </span>
          <div>
            <h2 id="tight-fit-title" className="font-display text-lg font-semibold text-ink">
              {t('tightFit.title')}
            </h2>
            <p className="mt-2 text-sm text-ink-muted">{t('tightFit.body', { model: modelName })}</p>
            <p className="mt-2 text-sm text-ink-muted">{t('tightFit.advice')}</p>
          </div>
        </div>
        <div className="flex flex-wrap justify-end gap-2">
          <button type="button" className="btn-secondary" data-autofocus onClick={onCancel}>
            {t('tightFit.cancel')}
          </button>
          <button type="button" className="btn-primary" onClick={onInstall}>
            {t('tightFit.install')}
          </button>
        </div>
      </div>
    </div>
  )
}
