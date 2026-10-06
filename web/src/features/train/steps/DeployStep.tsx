import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Trans, useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { Ratatoskr } from '@/components/ui/Ratatoskr'
import { api } from '@/lib/api'
import { useMascotState } from '@/lib/ratatoskr/useMascotState'
import type { SpecializedAIView } from '@/types/api'
import { errorText } from '../display'
import { ExportCard } from './ExportCard'
import { formatDate, formatDecimal } from '@/i18n/format'

export function DeployStep({ view }: { view: SpecializedAIView }) {
  const { t } = useTranslation('train')
  const queryClient = useQueryClient()
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['training'] })
  }
  const deploy = useMutation({ mutationFn: (rev: number) => api.deployRevision(view.id, rev), onSuccess: refresh })
  const undeploy = useMutation({ mutationFn: () => api.undeployAI(view.id), onSuccess: refresh })
  // Ratatoskr celebrates a deploy once, then stays beside it.
  const mascot = useMascotState({ react: [], celebrate: deploy.isSuccess ? deploy.submittedAt : undefined })

  return (
    <div className="space-y-4">
      <div className="card space-y-3">
        <div className="flex items-center gap-3">
          {deploy.isSuccess ? <Ratatoskr state={mascot} size={64} /> : null}
          <h3 className="section-title">{t('deploy.title')}</h3>
        </div>
        <p className="text-sm text-ink-muted">{t('deploy.description')}</p>
        {view.revisions.length === 0 && <p className="text-sm text-ink-muted">{t('deploy.first')}</p>}
        <ul className="divide-y divide-line/60">
          {view.revisions.map((r) => {
            const deployed = r.revision === view.deployed_revision
            return (
              <li key={r.revision} className="flex flex-wrap items-center gap-3 py-2 text-sm">
                <div className="min-w-0 flex-1">
                  <p className="text-ink">
                    {t('deploy.revision', { revision: r.revision })}
                    {deployed && <span className="status-chip ms-2 bg-success/15 text-success">{t('deploy.deployed')}</span>}
                  </p>
                  <p className="text-xs text-ink-muted">
                    {t('deploy.meta', {
                      examples: r.example_count,
                      method: r.hyper.method?.toUpperCase(),
                      date: formatDate(r.created_at),
                    })}
                    {r.final_val_loss != null && t('deploy.valLoss', { loss: formatDecimal(r.final_val_loss, 3) })}
                  </p>
                </div>
                {!r.evaluated ? (
                  <span className="text-xs text-ink-faint">{t('deploy.compareFirst')}</span>
                ) : deployed ? (
                  <button type="button" className="btn-secondary btn-sm" disabled={undeploy.isPending} onClick={() => undeploy.mutate()}>
                    {t('deploy.undeploy')}
                  </button>
                ) : (
                  <button type="button" className="btn-primary btn-sm" disabled={deploy.isPending} onClick={() => deploy.mutate(r.revision)}>
                    {view.deployed_revision ? t('deploy.switch') : t('deploy.deploy')}
                  </button>
                )}
              </li>
            )
          })}
        </ul>
        {(deploy.error || undeploy.error) && <p className="text-sm text-danger">{errorText(deploy.error ?? undeploy.error)}</p>}
      </div>

      {view.deployed_revision > 0 && (
        <div className="card space-y-3">
          <h3 className="section-title">{t('deploy.use')}</h3>
          <p className="text-sm text-ink-muted">
            <Trans
              t={t}
              i18nKey="deploy.useBody"
              values={{ name: view.name, id: view.model_id }}
              components={{
                name: <span className="text-ink" />,
                go: <Link to="/chat?new=1" className="underline" />,
                id: <span className="mono-id" />,
              }}
            />
          </p>
          {/* Focusable, so the command can be scrolled sideways from the keyboard. */}
          <pre className="log-panel overflow-x-auto text-xs" tabIndex={0}>{`curl http://127.0.0.1:7331/v1/chat/completions \\
  -H 'Content-Type: application/json' \\
  -d '{"model": "${view.model_id}", "messages": [{"role": "user", "content": "Hello"}]}'`}</pre>
          <p className="text-xs text-ink-faint">{t('deploy.runsHere')}</p>
        </div>
      )}

      <ExportCard view={view} />
    </div>
  )
}
