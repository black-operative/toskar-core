import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Trans, useTranslation } from 'react-i18next'
import { Toggle } from '@/components/ui/Toggle'
import { api } from '@/lib/api'
import { quitDesktopForRestart } from '@/lib/desktopBridge'
import { useUIStore } from '@/stores/uiStore'
import type { SettingsPatch } from '@/types/api'
import { ExternalServer } from './ExternalServer'

/**
 * How this computer reaches others (#203): finding computers on the local
 * network and, in advanced mode, an external OpenAI-compatible server. They
 * moved here from Settings, which keeps personal preferences. The text is in
 * the settings namespace, where it was written.
 */
export function NetworkSettings() {
  const { t } = useTranslation('settings')
  const queryClient = useQueryClient()
  const advancedMode = useUIStore((s) => s.advancedMode)
  const settingsQuery = useQuery({ queryKey: ['settings'], queryFn: () => api.getSettings(), retry: false })
  const settings = settingsQuery.data
  const patchMutation = useMutation({
    mutationFn: (patch: SettingsPatch) => api.updateSettings(patch),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['settings'] })
      queryClient.invalidateQueries({ queryKey: ['nodes'] })
    },
  })
  const busy = patchMutation.isPending || settingsQuery.isLoading
  const discoveryOn = settings?.discovery_enabled ?? true

  return (
    <section className="space-y-3" aria-labelledby="computers-network-title">
      <h2 id="computers-network-title" className="section-title">
        {t('groups.network')}
      </h2>
      <div className="card space-y-4">
        <div className="flex items-start justify-between gap-4">
          <div>
            <h3 className="font-medium text-ink">{t('discovery.title')}</h3>
            <p className="mt-1 text-sm text-ink-muted">{t('discovery.description')}</p>
          </div>
          <Toggle
            label={t('discovery.title')}
            checked={discoveryOn}
            disabled={busy}
            onChange={() => patchMutation.mutate({ discovery_enabled: !discoveryOn })}
          />
        </div>
        {settings?.discovery_needs_restart ? (
          <div className="rounded-lg border border-warning/30 bg-warning/10 px-4 py-3 text-sm text-ink">
            <p>{t('discovery.needsRestart')}</p>
            <button
              type="button"
              className="btn-primary mt-3 px-3 py-1.5 text-xs"
              onClick={async () => {
                const ok = await quitDesktopForRestart()
                if (!ok) {
                  window.alert(t('discovery.restartManually'))
                }
              }}
            >
              {t('discovery.restartNow')}
            </button>
          </div>
        ) : (
          <p className="text-xs text-ink-faint">
            <Trans
              t={t}
              i18nKey="discovery.status"
              values={{ state: discoveryOn ? t('onOff.on') : t('onOff.off') }}
              components={{ strong: <span className="font-medium text-ink" /> }}
            />
          </p>
        )}
      </div>
      {advancedMode ? <ExternalServer /> : null}
    </section>
  )
}
