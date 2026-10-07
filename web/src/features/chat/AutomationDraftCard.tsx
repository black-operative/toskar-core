import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { api } from '@/lib/api'
import type { AutomationDraft } from '@/types/api'
import { notificationLabel, scheduleLabel } from '@/features/automations/parseRequest'

/**
 * An automation the answer drafted (#204): what it does, when, and when it
 * notifies. Nothing is scheduled until the person presses Create, and the
 * automation keeps this chat. Once created, the card links to it, also
 * after the chat is opened again.
 */
export function AutomationDraftCard({ draft, conversationId }: { draft: AutomationDraft; conversationId: string }) {
  const { t } = useTranslation('automations')
  const queryClient = useQueryClient()
  const automations = useQuery({ queryKey: ['automations'], queryFn: api.listAutomations })
  const create = useMutation({
    mutationFn: () =>
      api.createAutomation({
        name: draft.name,
        prompt: draft.prompt,
        schedule: draft.schedule,
        notification: draft.notification,
        profile_id: draft.profile_id || 'general-assistant',
        model_id: 'auto',
        response_language: 'account',
        conversation_id: conversationId,
        draft_id: draft.id,
      }),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['automations'] }),
  })
  const created = create.data ?? automations.data?.find((item) => item.draft_id === draft.id)

  return (
    <div className="mt-3 rounded-xl border border-line/70 bg-surface px-3 py-2.5 text-sm">
      <p className="text-xs text-ink-faint">{t('draft.label')}</p>
      <p className="mt-0.5 font-medium text-ink">{draft.name}</p>
      <dl className="mt-2 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-xs">
        <dt className="text-ink-faint">{t('draft.when')}</dt>
        <dd className="text-ink">{scheduleLabel(draft.schedule)}</dd>
        <dt className="text-ink-faint">{t('draft.notify')}</dt>
        <dd className="text-ink">{notificationLabel(draft.notification)}</dd>
        <dt className="text-ink-faint">{t('draft.task')}</dt>
        <dd dir="auto" className="whitespace-pre-wrap text-ink-muted">
          {draft.prompt}
        </dd>
      </dl>
      {draft.notes?.length ? (
        <ul className="mt-2 space-y-0.5 text-xs text-ink-faint">
          {draft.notes.map((note) => (
            <li key={note}>{note}</li>
          ))}
        </ul>
      ) : null}
      <div className="mt-3 flex flex-wrap items-center gap-x-3 gap-y-2">
        {created ? (
          <>
            <span className="text-xs font-medium text-ink">✓ {t('draft.created')}</span>
            <Link to={`/automations?id=${encodeURIComponent(created.id)}`} className="text-xs text-primary underline-offset-2 hover:underline">
              {t('draft.open')}
            </Link>
          </>
        ) : (
          <>
            <button type="button" className="btn-primary px-3 py-1.5 text-xs" disabled={create.isPending} onClick={() => create.mutate()}>
              {create.isPending ? t('draft.creating') : t('draft.create')}
            </button>
            <span className="min-w-0 flex-1 text-xs text-ink-faint">{t('draft.hint')}</span>
          </>
        )}
      </div>
      {create.isError ? (
        <p className="mt-2 text-xs text-danger">
          {t('draft.failed')} {create.error instanceof Error ? create.error.message : ''}
        </p>
      ) : null}
    </div>
  )
}
