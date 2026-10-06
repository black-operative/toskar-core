import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '@/lib/api'
import { MAX_UPLOAD_BYTES, readUpload, UPLOAD_ACCEPT } from '@/lib/upload'
import type { ClassifyResult, MaterialUse, SpecializedAIView, TrainingMaterial } from '@/types/api'
import { ConceptCards } from '../ConceptCards'
import { SampleFiles } from '../SampleFiles'
import { errorText, MATERIAL_USES, materialUseDescription, materialUseLabel } from '../display'


export function UseBadge({ use }: { use: MaterialUse }) {
  const tone =
    use === 'training'
      ? 'bg-primary-soft text-primary-active'
      : use === 'knowledge'
        ? 'bg-mimir/15 text-mimir'
        : 'bg-accent-soft text-accent'
  return <span className={['status-chip', tone].join(' ')}>{materialUseLabel(use)}</span>
}

export function MaterialStep({ view, onNext }: { view: SpecializedAIView; onNext: () => void }) {
  const { t } = useTranslation('train')
  const [mode, setMode] = useState<'file' | 'chats'>('file')
  return (
    <div className="space-y-4">
      <ConceptCards compact />
      <div className="card space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h3 className="section-title">{t('material.add')}</h3>
          <div className="flex gap-1.5">
            <button type="button" className={mode === 'file' ? 'btn-primary btn-sm' : 'btn-secondary btn-sm'} onClick={() => setMode('file')}>
              {t('material.filePaste')}
            </button>
            <button type="button" className={mode === 'chats' ? 'btn-primary btn-sm' : 'btn-secondary btn-sm'} onClick={() => setMode('chats')}>
              {t('material.chats')}
            </button>
          </div>
        </div>
        {mode === 'file' ? <AddFile aiID={view.id} /> : <AddChats aiID={view.id} />}
        <SampleFiles open={Boolean(view.example)} />
      </div>
      <MaterialList view={view} />
      <button type="button" className="btn-primary" disabled={view.materials.length === 0} onClick={onNext}>
        {t('material.continue')}
      </button>
    </div>
  )
}

function AddFile({ aiID }: { aiID: string }) {
  const { t } = useTranslation('train')
  const queryClient = useQueryClient()
  const [filename, setFilename] = useState('')
  const [text, setText] = useState('')
  // A spreadsheet upload replaces the text box; its content is sent as base64.
  const [binary, setBinary] = useState<string | null>(null)
  const [use, setUse] = useState<MaterialUse | null>(null)
  const [preview, setPreview] = useState<ClassifyResult | null>(null)
  const [readError, setReadError] = useState('')

  const effectiveName = filename || 'pasted.txt'
  const classify = useMutation({
    mutationFn: (choice?: MaterialUse) => api.classifyMaterial(effectiveName, text, choice, binary ?? undefined),
    onSuccess: (res) => {
      if (!res) return
      setPreview(res)
      setUse(res.use)
    },
  })
  const add = useMutation({
    mutationFn: () =>
      api.addMaterial(aiID, { filename: effectiveName, text, use: use ?? undefined, content_base64: binary ?? undefined }),
    onSuccess: () => {
      setText('')
      setBinary(null)
      setFilename('')
      setPreview(null)
      setUse(null)
      void queryClient.invalidateQueries({ queryKey: ['training'] })
    },
  })

  // Re-check the recommendation after the content settles.
  useEffect(() => {
    if (!text.trim() && !binary) {
      setPreview(null)
      return
    }
    const t = setTimeout(() => classify.mutate(undefined), 400)
    return () => clearTimeout(t)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [text, filename, binary])

  async function readFile(file: File) {
    setReadError('')
    if (file.size > MAX_UPLOAD_BYTES) {
      setReadError(t('material.tooLarge'))
      return
    }
    const upload = await readUpload(file)
    setFilename(upload.filename)
    setText(upload.text ?? '')
    setBinary(upload.contentBase64 ?? null)
  }

  const rec = preview?.recommendation
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <label className="btn-secondary btn-sm cursor-pointer">
          {t('material.chooseFile')}
          <input type="file" accept={UPLOAD_ACCEPT} className="sr-only" onChange={(e) => e.target.files?.[0] && void readFile(e.target.files[0])} />
        </label>
        <span className="text-xs text-ink-faint">
          {filename ? filename : t('material.pasteHint')}
        </span>
      </div>
      {binary ? (
        <p className="rounded-lg bg-raised p-3 text-sm text-ink-muted">
          {t('material.ready', { filename })}{' '}
          {filename.toLowerCase().endsWith('.pdf') ? t('material.readsPdf') : t('material.readsSheet')}{' '}
          <button type="button" className="underline" onClick={() => { setBinary(null); setFilename('') }}>
            {t('material.clear')}
          </button>
        </p>
      ) : (
      <textarea
        className="field min-h-36 w-full font-mono text-xs"
        value={text}
        onChange={(e) => setText(e.target.value)}
        placeholder={t('material.placeholder')}
        aria-label={t('material.label')}
      />
      )}
      {readError && <p className="text-sm text-danger">{readError}</p>}
      {rec && (
        <div className="rounded-lg bg-raised p-3 text-sm">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-ink">{t('material.recommends')}</span>
            <UseBadge use={rec.use} />
            {rec.example_count > 0 && <span className="text-xs text-ink-muted">{t('material.examplesFound', { count: rec.example_count })}</span>}
          </div>
          <ul className="mt-2 list-disc space-y-0.5 ps-5 text-ink-muted">
            {rec.reasons.map((r) => (
              <li key={r}>{r}</li>
            ))}
          </ul>
          <fieldset className="mt-3 space-y-1.5">
            <legend className="text-xs font-medium text-ink">{t('material.useFor')}</legend>
            {MATERIAL_USES.map((u) => (
              <label key={u} className="flex items-start gap-2">
                <input
                  type="radio"
                  name="use"
                  value={u}
                  aria-label={materialUseLabel(u)}
                  className="mt-1"
                  checked={use === u}
                  onChange={() => {
                    setUse(u)
                    classify.mutate(u)
                  }}
                />
                <span>
                  <span className="text-ink">{materialUseLabel(u)}</span>
                  {u === rec.use && <span className="text-xs text-ink-faint">{t('material.recommendedTag')}</span>}
                  <span className="block text-xs text-ink-muted">{materialUseDescription(u)}</span>
                </span>
              </label>
            ))}
          </fieldset>
          {preview?.warning && <p className="mt-2 rounded-md bg-warning/10 p-2 text-warning">{preview.warning}</p>}
          {preview?.error && <p className="mt-2 rounded-md bg-danger/10 p-2 text-danger">{preview.error}</p>}
        </div>
      )}
      {add.error && <p className="text-sm text-danger">{errorText(add.error)}</p>}
      <button
        type="button"
        className="btn-primary"
        disabled={(!text.trim() && !binary) || !preview || Boolean(preview.error) || add.isPending}
        onClick={() => add.mutate()}
      >
        {add.isPending ? t('material.adding') : t('material.addMaterial')}
      </button>
    </div>
  )
}

function AddChats({ aiID }: { aiID: string }) {
  const { t } = useTranslation('train')
  const queryClient = useQueryClient()
  const conversations = useQuery({ queryKey: ['conversations'], queryFn: () => api.getConversations() })
  const [picked, setPicked] = useState<string[]>([])
  const add = useMutation({
    mutationFn: () => api.addConversations(aiID, picked),
    onSuccess: () => {
      setPicked([])
      void queryClient.invalidateQueries({ queryKey: ['training'] })
    },
  })
  const list = conversations.data ?? []
  return (
    <div className="space-y-3">
      <p className="text-sm text-ink-muted">{t('material.chatsHint')}</p>
      {list.length === 0 && <p className="text-sm text-ink-faint">{t('material.noChats')}</p>}
      <ul className="max-h-64 space-y-1 overflow-y-auto">
        {list.map((c) => (
          <li key={c.id}>
            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={picked.includes(c.id)}
                onChange={(e) => setPicked(e.target.checked ? [...picked, c.id] : picked.filter((id) => id !== c.id))}
              />
              <span className="text-ink">{c.title || t('material.untitledChat')}</span>
            </label>
          </li>
        ))}
      </ul>
      {add.error && <p className="text-sm text-danger">{errorText(add.error)}</p>}
      <button type="button" className="btn-primary" disabled={picked.length === 0 || add.isPending} onClick={() => add.mutate()}>
        {picked.length ? t('material.addChats', { count: picked.length }) : t('material.addChatsNone')}
      </button>
    </div>
  )
}

function MaterialList({ view }: { view: SpecializedAIView }) {
  const { t } = useTranslation('train')
  const queryClient = useQueryClient()
  const remove = useMutation({
    mutationFn: (m: TrainingMaterial) => api.deleteMaterial(view.id, m.id),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['training'] }),
  })
  if (view.materials.length === 0) return null
  return (
    <div className="card space-y-2">
      <h3 className="section-title">{t('material.list')}</h3>
      <ul className="divide-y divide-line/60">
        {view.materials.map((m) => (
          <li key={m.id} className="flex flex-wrap items-start gap-3 py-2">
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-2">
                <span className="font-medium text-ink">{m.name}</span>
                <UseBadge use={m.use} />
                {m.use !== m.recommended.use && (
                  <span className="text-xs text-ink-faint">{t('material.recommendedUse', { use: materialUseLabel(m.recommended.use) })}</span>
                )}
              </div>
              <p className="mt-0.5 text-xs text-ink-muted">
                {m.example_count > 0 && t('material.examples', { count: m.example_count })}
                {m.example_count > 0 && m.knowledge_source_id && ' · '}
                {m.knowledge_source_id && t('material.connected')}
              </p>
              {m.warning && <p className="mt-1 text-xs text-warning">{m.warning}</p>}
            </div>
            <button
              type="button"
              className="btn-secondary btn-sm"
              disabled={remove.isPending}
              onClick={() => {
                if (window.confirm(t('material.confirmRemove', { name: m.name }))) remove.mutate(m)
              }}
            >
              {t('material.remove')}
            </button>
          </li>
        ))}
      </ul>
    </div>
  )
}
