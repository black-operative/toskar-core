import { useTranslation } from 'react-i18next'
import { useState } from 'react'
import type { Model, ModelFit, ModelDownloadProgressPayload } from '@/types/api'
import { formatBytes } from '@/lib/format'
import { SmallModelNote } from './SmallModelNote'
import { CommunityScore, RateButton } from './ratings'
import { useUIStore } from '@/stores/uiStore'
import {
  bestForLabel,
  bestMachineName,
  contextTokensLabel,
  fitLabelText,
  installAction,
  machineMemoryLabel,
  memoryLabel,
  modelToolAssessment,
  languageDetail,
  languageName,
  languageSummary,
  purposeChips,
  runtimeRangeLabel,
  speedLabel,
} from './modelPresentation'
import { formatPercent } from '@/i18n/format'

export function ModelCard({
  model,
  fit,
  peerFits,
  winnerLabel,
  chosenByCommunity,
  progress,
  installing,
  onInstall,
  alternative,
  onInstallAlternative,
}: {
  model: Model
  /** A larger model to suggest when this one is small. */
  alternative?: Model | null
  onInstallAlternative?: (id: string) => void
  fit?: ModelFit
  peerFits?: { nodeName: string; label: ModelFit['label'] }[]
  winnerLabel?: string
  /** Community ratings helped choose this model for its badge. */
  chosenByCommunity?: boolean
  progress?: ModelDownloadProgressPayload
  installing?: boolean
  onInstall: () => void
}) {
  const { t, i18n } = useTranslation('models')
  const advancedMode = useUIStore((s) => s.advancedMode)
  const [detailsOpen, setDetailsOpen] = useState(false)
  const isDownloading = model.status === 'downloading' || Boolean(progress)
  const chips = purposeChips(model)
  const tools = modelToolAssessment(model)
  const purpose = model.summary?.split(/[.!?]/)[0]?.trim() || bestForLabel(model)
  const install = installAction(fit)
  const speed = speedLabel(fit)
  const runtime = runtimeRangeLabel(fit)
  const machine = machineMemoryLabel(fit)
  const peers = (peerFits ?? []).filter((peer) => peer.nodeName)
  const languages = languageSummary(model, i18n.language)

  return (
    <article className="card flex h-full min-w-0 flex-col overflow-hidden transition duration-150">
      <div className="flex min-w-0 items-start justify-between gap-2">
        <h3 className="min-w-0 font-display text-lg font-semibold tracking-tight text-ink">
          {model.display_name}
        </h3>
        {winnerLabel ? (
          <span className="badge-preferred shrink-0">★ {winnerLabel}</span>
        ) : null}
      </div>

      <p className="mt-1 line-clamp-2 text-sm text-ink-muted">{purpose}</p>
      {winnerLabel && chosenByCommunity ? (
        <p className="mt-1 text-xs text-ink-muted">{t('ratings.chosenByCommunity')}</p>
      ) : null}

      <p className="mt-3 flex flex-wrap items-center gap-x-2 gap-y-1 text-sm text-ink-muted">
        <span className={['status-chip', fitTone(fit?.label)].join(' ')}>
          <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-current" aria-hidden />
          {fitLabelText(fit?.label) || t('fit.unknown')}
        </span>
        <span className="tabular-nums">{memoryLabel(model)}</span>
        {speed ? (
          <>
            <span className="text-ink-faint" aria-hidden>·</span>
            <span className="tabular-nums">{speed}</span>
          </>
        ) : null}
      </p>
      {fit?.reason ? <p className="mt-1 text-xs text-ink-muted">{fit.reason}</p> : null}
      {fit?.runtime_warning ? (
        <p className="mt-1 text-xs text-ink">{fit.runtime_warning}</p>
      ) : null}

      <CommunityScore modelId={model.id} />

      <div className="mt-2 flex flex-wrap gap-1.5">
        {chips.map((tag) => (
          <span key={tag} className="status-chip bg-raised/80 text-ink-muted">
            {tag}
          </span>
        ))}
      </div>
      <p className="mt-2 text-xs text-ink-muted" title={tools.detail}>
        {tools.summary}
      </p>
      {languages ? (
        <p className="mt-1 text-xs text-ink-muted" title={languages.title}>
          {languages.text}
        </p>
      ) : null}
      <SmallModelNote model={model} alternative={alternative} onInstallAlternative={onInstallAlternative} />

      {isDownloading && progress && (
        <div className="mt-3">
          <div className="h-2 overflow-hidden rounded-full bg-raised">
            <div
              className="h-full bg-primary transition-all duration-300"
              style={{ width: `${Math.min(progress.percent, 100)}%` }}
            />
          </div>
          <p className="mt-1 text-xs text-ink-muted">
            {t('install.progress', {
              percent: formatPercent(progress.percent / 100),
              done: formatBytes(progress.bytes_downloaded),
              total: formatBytes(progress.bytes_total),
            })}
          </p>
        </div>
      )}

      <div className="mt-auto flex flex-wrap items-center gap-2 pt-4">
        {!model.installed ? (
          <button
            type="button"
            className="btn-primary btn-sm"
            disabled={installing || isDownloading || install.disabled}
            onClick={onInstall}
          >
            {isDownloading ? t('install.downloading') : install.label}
          </button>
        ) : (
          <>
            <span className="status-chip h-8 bg-success/15 px-2.5 text-success">
              <svg viewBox="0 0 16 16" className="h-3.5 w-3.5" fill="none" stroke="currentColor" strokeWidth={1.8} strokeLinecap="round" strokeLinejoin="round" aria-hidden>
                <path d="m3.5 8.5 3 3 6-7" />
              </svg>
              {t('install.installed')}
            </span>
            <RateButton modelId={model.id} modelName={model.display_name} />
          </>
        )}
        <button
          type="button"
          className="btn-secondary btn-sm"
          aria-expanded={detailsOpen}
          onClick={() => setDetailsOpen((v) => !v)}
        >
          {detailsOpen ? t('card.hideDetails') : t('card.details')}
        </button>
      </div>

      {detailsOpen && (
        <dl className="mt-4 space-y-1 border-t border-line/50 pt-3 text-xs text-ink-muted">
          <Row label={t('card.modelSize')} value={memoryLabel(model)} />
          <Row label={t('card.quantization')} value={fit?.quantization || model.variant || t('card.unknown')} />
          {runtime ? <Row label={t('card.runtimeMemory')} value={runtime} /> : null}
          <Row
            label={t('card.context')}
            value={fit?.context_tokens ? contextTokensLabel(fit.context_tokens) : '—'}
          />
          {machine ? <Row label={t('card.machineMemory')} value={machine} /> : null}
          {fit?.available_memory_bytes ? (
            <Row label={t('card.available')} value={formatBytes(fit.available_memory_bytes)} />
          ) : null}
          <Row label={t('card.expected')} value={fitLabelText(fit?.label) || t('card.unknown')} />
          {fit?.gpu_note ? <Row label={t('card.gpu')} value={fit.gpu_note} /> : null}
          {fit?.recommendations && fit.recommendations.length > 0 ? (
            <div className="pt-2">
              <dt>{t('card.recommendations')}</dt>
              <dd className="mt-1 text-ink">
                <ul className="list-disc space-y-0.5 ps-4">
                  {fit.recommendations.map((item) => (
                    <li key={item}>{item}</li>
                  ))}
                </ul>
              </dd>
            </div>
          ) : null}
          {(model.languages ?? []).length > 0 ? (
            <div className="pt-2">
              <dt>{t('languages.title')}</dt>
              <dd className="mt-1 space-y-0.5 text-ink">
                {(model.languages ?? []).map((l) => (
                  <p key={l.language} className="flex justify-between gap-3">
                    <span>{languageName(l.language)}</span>
                    <span className="text-end text-ink-muted">{languageDetail(l)}</span>
                  </p>
                ))}
              </dd>
            </div>
          ) : null}
          {peers.length > 1 ? (
            <div className="pt-2">
              <dt>{t('card.onYourComputers')}</dt>
              <dd className="mt-1 space-y-0.5 text-end text-ink">
                {peers.map((peer) => (
                  <p key={`${peer.nodeName}-${peer.label}`}>
                    {peer.nodeName} · {fitLabelText(peer.label)}
                  </p>
                ))}
                <p>{t('card.bestMachine', { name: bestMachineName(peers) })}</p>
              </dd>
            </div>
          ) : null}
          {advancedMode && (
            <>
              <Row label={t('card.family')} value={model.family || '—'} />
              <Row label={t('card.parameters')} value={model.parameters || '—'} />
              <Row label={t('card.format')} value={model.source?.format || 'gguf'} />
              <Row label={t('card.runtime')} value={(model.runtime ?? ['llamacpp']).join(', ')} />
              <Row label={t('card.modelId')} value={model.id} />
            </>
          )}
        </dl>
      )}
    </article>
  )
}

/** The fit chip's colours: green when it runs well, amber when tight, red when it won't. */
function fitTone(label?: string): string {
  switch (label) {
    case 'excellent':
    case 'good':
      return 'bg-success/15 text-success'
    case 'tight':
      return 'bg-warning/15 text-warning'
    case 'heavy':
    case 'too_large':
    case 'unsupported':
      return 'bg-danger/15 text-danger'
    default:
      return 'bg-raised text-ink-muted'
  }
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex min-w-0 justify-between gap-3">
      <dt className="shrink-0">{label}</dt>
      <dd className="min-w-0 break-anywhere text-end text-ink" title={value}>
        {value}
      </dd>
    </div>
  )
}
