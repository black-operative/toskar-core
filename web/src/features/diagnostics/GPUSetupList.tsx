import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { GPUProblem } from '@/types/api'

function Command({ command }: { command: string }) {
  const { t } = useTranslation('diagnostics')
  const [copied, setCopied] = useState(false)
  return (
    <div className="relative">
      <pre className="overflow-x-auto whitespace-pre-wrap break-all rounded-lg bg-raised/70 p-3 pe-20 font-mono text-xs text-ink">
        {command}
      </pre>
      <button
        type="button"
        className="btn-secondary absolute end-2 top-2 px-2.5 py-1 text-xs"
        onClick={() => void navigator.clipboard?.writeText(command).then(() => setCopied(true))}
      >
        {copied ? t('rows.gpu.copied') : t('rows.gpu.copy')}
      </button>
    </div>
  )
}

/**
 * Each piece this computer still needs for Toskar to use its GPU, with the
 * command that fixes it here or where to download it (#317).
 */
export function GPUSetupList({ problems }: { problems: GPUProblem[] }) {
  const { t } = useTranslation('diagnostics')
  return (
    <ul className="space-y-3">
      {problems.map((p) => (
        <li key={p.code} className="space-y-1.5">
          <p className="text-sm text-ink-muted">{t(`rows.gpu.problems.${p.code}`)}</p>
          {p.command ? <Command command={p.command} /> : null}
          {p.url ? (
            <a href={p.url} target="_blank" rel="noreferrer" className="inline-block text-sm text-primary hover:underline">
              {t('rows.gpu.download')}
            </a>
          ) : null}
        </li>
      ))}
    </ul>
  )
}
