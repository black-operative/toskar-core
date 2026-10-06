import { useTranslation } from 'react-i18next'
import i18n from '@/i18n'
import { useState, type ReactNode } from 'react'
import type { Citation, MessageMeta } from '@/types/api'
import { downloadArtifact } from '@/lib/api'
import { useUIStore } from '@/stores/uiStore'
import { RunDetails } from './RunDetails'
import { FileChip } from './FileChips'

function hostOf(url: string): string {
  try {
    return new URL(url).host.replace(/^www\./, '')
  } catch {
    return url
  }
}

/** One chip: a web page, a file, or a knowledge source with its passages. */
type SourceItem = { kind: string; label: string; url?: string; artifactId?: string; passages: Citation[] }

function groupSources(sources: Citation[]): SourceItem[] {
  const out: SourceItem[] = []
  for (const s of sources) {
    if (s.kind === 'memory') {
      const existing = out.find((i) => i.kind === 'memory')
      if (existing) {
        existing.passages.push(s)
        continue
      }
      out.push({ kind: 'memory', label: i18n.t('chat:answer.memory'), passages: [s] })
    } else if (s.kind === 'knowledge') {
      const name = s.source || s.title
      const existing = out.find((i) => i.kind === 'knowledge' && i.label === name)
      if (existing) {
        existing.passages.push(s)
        continue
      }
      out.push({ kind: 'knowledge', label: name, passages: [s] })
    } else if (s.kind === 'web') {
      out.push({ kind: 'web', label: s.title || hostOf(s.url ?? ''), url: s.url, passages: [s] })
    } else {
      out.push({ kind: s.kind, label: s.title, artifactId: s.artifact_id, passages: [s] })
    }
  }
  return out
}

const chipClass =
  'inline-flex max-w-[16rem] items-center gap-1.5 rounded-full border border-line/70 bg-surface px-2.5 py-1 text-xs text-ink-muted transition hover:border-primary/50 hover:text-ink'

function SourceChip({ item, index }: { item: SourceItem; index: number }) {
  const { t } = useTranslation('chat')
  const [open, setOpen] = useState(false)
  const badge = (
    <span className="tabular-nums text-ink-faint" aria-hidden>
      {index + 1}
    </span>
  )
  if (item.kind === 'web' && item.url) {
    return (
      <a href={item.url} target="_blank" rel="noopener noreferrer" className={chipClass} title={item.passages[0]?.snippet || item.url}>
        {badge}
        <span className="truncate">{item.label}</span>
        <span className="shrink-0 text-ink-faint">{hostOf(item.url)}</span>
      </a>
    )
  }
  // A chat file can be saved, like the Files chips (#281).
  if (item.kind === 'file' && item.artifactId) {
    return <FileSourceChip item={item} id={item.artifactId} badge={badge} />
  }
  const count = item.passages.length
  // A memory's text is its title; show it as the passage.
  const withText = item.passages
    .map((p) => (item.kind === 'memory' ? { ...p, snippet: p.title, title: t('answer.remembered') } : p))
    .filter((p) => p.snippet)
  return (
    <span className="relative">
      <button type="button" className={chipClass} aria-expanded={open} title={item.label} onClick={() => setOpen((v) => !v)}>
        {badge}
        {item.kind === 'knowledge' ? <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-mimir" aria-hidden /> : null}
        {item.kind === 'memory' ? <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-norn" aria-hidden /> : null}
        <span className="truncate">{item.label}</span>
        {count > 1 ? <span className="shrink-0 text-ink-faint">· {count}</span> : null}
      </button>
      {open && withText.length > 0 ? (
        <span className="absolute start-0 top-full z-20 mt-1 block max-h-72 w-80 overflow-y-auto rounded-lg border border-line/70 bg-surface p-3 text-xs text-ink-muted shadow-panel">
          {withText.map((p, i) => (
            <span key={i} className={i > 0 ? 'mt-2 block border-t border-line/50 pt-2' : 'block'}>
              <span className="mb-0.5 block font-medium text-ink">{p.title}</span>
              {p.snippet}
            </span>
          ))}
        </span>
      ) : null}
    </span>
  )
}

function FileSourceChip({ item, id, badge }: { item: SourceItem; id: string; badge: ReactNode }) {
  const { t } = useTranslation('chat')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  return (
    <button
      type="button"
      className={chipClass}
      disabled={busy}
      title={error ?? t('attachments.download', { name: item.label })}
      onClick={async () => {
        setBusy(true)
        setError(null)
        try {
          await downloadArtifact({ id, name: item.label })
        } catch (err) {
          setError(err instanceof Error ? err.message : t('attachments.downloadFailed'))
        } finally {
          setBusy(false)
        }
      }}
    >
      {badge}
      <svg viewBox="0 0 16 16" className="h-3 w-3 shrink-0" fill="none" stroke="currentColor" strokeWidth={1.5} aria-hidden>
        <path d="M8 2.5v7.5M4.5 6.5 8 10l3.5-3.5M3 13h10" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
      <span className="truncate">{item.label}</span>
      {error ? <span className="shrink-0 text-danger">{t('attachments.downloadFailed')}</span> : null}
    </button>
  )
}

/** Sources and a plain-language summary of the work behind an answer. */
export function AnswerDetails({ meta }: { meta?: MessageMeta }) {
  const { t } = useTranslation('chat')
  const [stepsOpen, setStepsOpen] = useState(false)
  const sources = groupSources(meta?.sources ?? [])
  const steps = meta?.steps ?? []
  const notice = meta?.notice?.trim()
  const files = (meta?.files ?? []).filter((f) => f.producer === 'assistant')
  const advancedMode = useUIStore((s) => s.advancedMode)
  const runId = advancedMode ? meta?.run_id : undefined
  if (sources.length === 0 && steps.length === 0 && !notice && files.length === 0 && !runId) return null
  return (
    <div className="mt-3 space-y-2 border-t border-line/50 pt-2.5">
      {notice ? (
        <p role="note" className="flex items-start gap-1.5 text-xs text-warning">
          <span aria-hidden>ⓘ</span>
          <span>{notice}</span>
        </p>
      ) : null}
      {files.length > 0 ? (
        <div>
          <p className="label-caps mb-1.5 text-[10px]">{t('answer.files')}</p>
          <div className="flex flex-wrap gap-1.5">
            {files.map((file) => (
              <FileChip key={file.id} file={file} />
            ))}
          </div>
        </div>
      ) : null}
      {sources.length > 0 ? (
        <div>
          <p className="label-caps mb-1.5 text-[10px]">{t('answer.sources')}</p>
          <div className="flex flex-wrap gap-1.5">
            {sources.map((item, i) => (
              <SourceChip key={`${item.kind}-${item.url ?? item.label}`} item={item} index={i} />
            ))}
          </div>
        </div>
      ) : null}
      {steps.length > 0 ? (
        <div className="text-xs text-ink-muted">
          <button
            type="button"
            className="underline-offset-2 hover:text-ink hover:underline"
            aria-expanded={stepsOpen}
            onClick={() => setStepsOpen((v) => !v)}
          >
            {stepsOpen ? '▾' : '▸'} {t('answer.steps', { count: steps.length })}
          </button>
          {stepsOpen ? (
            <ol className="mt-1.5 space-y-1 ps-4">
              {steps.map((s, i) => (
                <li key={i} className="list-decimal">
                  {s.text}
                </li>
              ))}
            </ol>
          ) : null}
        </div>
      ) : null}
      {runId ? <RunDetails runId={runId} /> : null}
    </div>
  )
}
