import { useTranslation } from 'react-i18next'
import { useEffect, useRef, useState } from 'react'
import { api } from '@/lib/api'
import type { FileRef } from '@/types/api'
import { AudioChip } from './FileChips'
import { speakableText } from './speakableText'

/** Reads an answer aloud on this computer and plays it. */
export function ReadAloudButton({ text, conversationId }: { text: string; conversationId?: string }) {
  const { t } = useTranslation('chat')
  const [audio, setAudio] = useState<FileRef | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  if (audio) return <AudioChip file={audio} autoPlay />
  return (
    <span className="inline-flex items-center gap-2">
      <button
        type="button"
        className="inline-flex items-center gap-1.5 rounded-full border border-line/70 bg-surface px-2.5 py-1 text-xs text-ink-muted transition hover:border-primary/50 hover:text-ink disabled:opacity-60"
        disabled={busy}
        title={t('readAloud.hint')}
        onClick={async () => {
          setBusy(true)
          setError(null)
          try {
            const res = await api.readAloud(speakableText(text), conversationId)
            if (res) setAudio(res.artifact)
          } catch (err) {
            setError(err instanceof Error ? err.message : t('readAloud.failed'))
          } finally {
            setBusy(false)
          }
        }}
      >
        <svg viewBox="0 0 16 16" className="h-3.5 w-3.5" fill="none" stroke="currentColor" strokeWidth={1.4} aria-hidden>
          <path d="M2.5 6h2.5L8.5 3v10L5 10H2.5z" strokeLinejoin="round" />
          <path d="M11 5.5a3.5 3.5 0 0 1 0 5M12.8 3.5a6 6 0 0 1 0 9" strokeLinecap="round" />
        </svg>
        {busy ? t('readAloud.busy') : t('readAloud.button')}
      </button>
      {error ? <span className="text-xs text-danger">{error}</span> : null}
    </span>
  )
}

/**
 * Reads an answer aloud with the device's own voices, where this computer
 * can't run speech itself, such as the Mac App Store app (#279).
 */
export function SystemReadAloudButton({ text }: { text: string }) {
  const { t, i18n } = useTranslation('chat')
  const [speaking, setSpeaking] = useState(false)
  // The utterance this button started, while it is the one being read.
  const current = useRef<SpeechSynthesisUtterance | null>(null)

  // Stop reading when the answer leaves the screen, unless another answer took over.
  useEffect(() => () => {
    if (current.current) window.speechSynthesis.cancel()
  }, [])

  return (
    <button
      type="button"
      className="inline-flex items-center gap-1.5 rounded-full border border-line/70 bg-surface px-2.5 py-1 text-xs text-ink-muted transition hover:border-primary/50 hover:text-ink"
      title={t('readAloud.systemHint')}
      aria-pressed={speaking}
      onClick={() => {
        const synth = window.speechSynthesis
        // One answer is read at a time.
        synth.cancel()
        if (speaking) {
          current.current = null
          setSpeaking(false)
          return
        }
        const utterance = new SpeechSynthesisUtterance(speakableText(text))
        utterance.lang = i18n.resolvedLanguage ?? i18n.language
        const done = () => {
          if (current.current === utterance) current.current = null
          setSpeaking(false)
        }
        utterance.onend = done
        utterance.onerror = done
        current.current = utterance
        setSpeaking(true)
        synth.speak(utterance)
      }}
    >
      <svg viewBox="0 0 16 16" className="h-3.5 w-3.5" fill="none" stroke="currentColor" strokeWidth={1.4} aria-hidden>
        {speaking ? (
          <rect x="4" y="4" width="8" height="8" rx="1" />
        ) : (
          <>
            <path d="M2.5 6h2.5L8.5 3v10L5 10H2.5z" strokeLinejoin="round" />
            <path d="M11 5.5a3.5 3.5 0 0 1 0 5M12.8 3.5a6 6 0 0 1 0 9" strokeLinecap="round" />
          </>
        )}
      </svg>
      {speaking ? t('readAloud.stop') : t('readAloud.button')}
    </button>
  )
}
