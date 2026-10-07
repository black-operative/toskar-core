import { useTranslation } from 'react-i18next'
import { Toggle } from '@/components/ui/Toggle'
import { localTimeZone } from '@/features/automations/parseRequest'
import type { SettingsPatch } from '@/types/api'

/**
 * One summary of every automation's results a day, in a chat you can reply
 * to (#204): on at 8:00 AM in this device's time zone, at a time you choose.
 */
export function AutomationDigestSetting({ value, disabled, onChange }: { value: string; disabled?: boolean; onChange: (patch: SettingsPatch) => void }) {
  const { t } = useTranslation('settings')
  return (
    <div className="space-y-2">
      <div className="flex items-start justify-between gap-4">
        <div>
          <p className="text-sm font-medium text-ink">{t('notifications.digest')}</p>
          <p className="mt-0.5 text-xs text-ink-muted">{t('notifications.digestHint')}</p>
        </div>
        <Toggle
          label={t('notifications.digest')}
          checked={Boolean(value)}
          disabled={disabled}
          onChange={() => onChange(value ? { automation_digest: '' } : { automation_digest: '08:00', automation_digest_zone: localTimeZone() })}
        />
      </div>
      {value ? (
        <label className="flex items-center gap-3 text-sm">
          <span className="text-ink-muted">{t('notifications.digestTime')}</span>
          <input
            className="field w-36"
            type="time"
            value={value}
            disabled={disabled}
            onChange={(event) => {
              if (event.target.value) onChange({ automation_digest: event.target.value, automation_digest_zone: localTimeZone() })
            }}
          />
        </label>
      ) : null}
    </div>
  )
}
