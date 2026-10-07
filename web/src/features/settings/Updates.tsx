import { useTranslation } from 'react-i18next'
import { Toggle } from '@/components/ui/Toggle'
import type { UpdatesStatus } from '@/types/api'

/**
 * Under the version in About: a newer Toskar, with what's new and where to
 * get it, from the daily check of toskar.ai (yeixio/toskar-apps#90).
 */
export function UpdateNotice({ updates }: { updates?: UpdatesStatus | null }) {
  const { t } = useTranslation('settings')
  if (updates?.available && updates.latest) {
    const { notes_url: notes, download_url: download, version } = updates.latest
    return (
      <p className="mt-2 rounded-lg bg-primary/10 px-3 py-2 text-sm text-ink">
        {t('about.updateAvailable', { version })}{' '}
        {notes ? (
          <a href={notes} target="_blank" rel="noopener noreferrer" className="text-primary underline-offset-2 hover:underline">
            {t('about.whatsNew')}
          </a>
        ) : null}
        {notes && download ? <span className="text-ink-faint"> · </span> : null}
        {download ? (
          <a href={download} target="_blank" rel="noopener noreferrer" className="font-medium text-primary underline-offset-2 hover:underline">
            {t('about.download')}
          </a>
        ) : null}
      </p>
    )
  }
  if (updates?.enabled && updates.latest) {
    return <p className="mt-0.5 text-xs text-ink-faint">{t('about.upToDate')}</p>
  }
  return null
}

/** The switch for the daily check, in builds that check (not the App Store or desktop app's copy). */
export function UpdateCheckSetting({
  updates,
  checked,
  disabled,
  onToggle,
}: {
  updates?: UpdatesStatus | null
  checked: boolean
  disabled?: boolean
  onToggle: () => void
}) {
  const { t } = useTranslation('settings')
  if (!updates?.supported) return null
  return (
    <div className="flex items-start justify-between gap-4 border-t border-line/50 pt-4">
      <div>
        <p className="text-sm font-medium text-ink">{t('about.checkUpdates')}</p>
        <p className="mt-1 text-xs text-ink-muted">{t('about.checkUpdatesHint')}</p>
      </div>
      <Toggle label={t('about.checkUpdates')} checked={checked} disabled={disabled} onChange={onToggle} />
    </div>
  )
}
