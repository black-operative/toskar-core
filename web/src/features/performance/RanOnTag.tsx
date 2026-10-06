import { useTranslation } from 'react-i18next'
import { ranOnDetail } from './performanceFormat'

/**
 * Whether a reply or benchmark ran on a GPU or the CPU (#317), as a small
 * tag with the backend and device on hover; nothing when it isn't known,
 * such as for replies from before Toskar recorded it.
 */
export function RanOnTag({ backend, device }: { backend?: string; device?: string }) {
  const { t } = useTranslation(['performance', 'common'])
  if (!backend) return null
  const gpu = backend !== 'cpu'
  const detail = ranOnDetail(backend, device, t)
  return (
    <span
      className={['status-chip', gpu ? 'bg-success/15 text-success' : 'bg-raised text-ink-muted'].join(' ')}
      title={detail}
    >
      {gpu ? t('ranOn.gpu') : t('ranOn.cpu')}
      <span className="sr-only"> ({detail})</span>
    </span>
  )
}
