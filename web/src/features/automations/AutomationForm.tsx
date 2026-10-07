import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { AIProfile, Automation, AutomationInput, AutomationSchedule, Model, ParsedAutomation, ToolRecord } from '@/types/api'
import i18n from '@/i18n'
import { api } from '@/lib/api'
import { canChat } from '@/features/models/modelPresentation'
import type { AutomationPreview } from '@/types/api'
import { useUIStore } from '@/stores/uiStore'
import { answerLanguages, nameInItself } from '@/i18n/answerLanguages'
import { currencyName } from '@/i18n/format'
import { readNumber } from './number'
import {
  civilInputValue,
  civilToISO,
  localTimeZone,
  notificationLabel,
  resultProse,
  scheduleLabel,
  visibleTask,
  weekdayName,
} from './parseRequest'
import { IDEAS } from './ideas'

// The notify choices, in order; each is automations:form.notifyChoices.<mode> in the catalog.
const NOTIFY_CHOICES: AutomationInput['notification']['mode'][] = ['condition', 'change', 'always', 'failure', 'none']

// Currencies offered for a price check. A request can name others (ISO 4217), and they are kept.
const CURRENCIES = ['USD', 'EUR', 'GBP', 'CHF', 'BRL', 'JPY', 'CNY', 'TWD', 'KRW']

interface AutomationFormProps {
  profiles: AIProfile[]
  models: Model[]
  tools: ToolRecord[]
  initial?: Automation | null
  seedDescription?: string
  /** The form was started from an idea, so say how to make it your own. */
  fromIdea?: boolean
  /** Offer the ideas as shortcuts; off when the page already shows them. */
  showIdeas?: boolean
  pending: boolean
  error?: string
  onCancel: () => void
  onSubmit: (input: AutomationInput) => void
}

export function AutomationForm({ profiles, models, tools, initial, seedDescription = '', fromIdea = false, showIdeas = true, pending, error, onCancel, onSubmit }: AutomationFormProps) {
  const { t } = useTranslation('automations')
  const advanced = useUIStore((state) => state.advancedMode) && new URLSearchParams(window.location.search).get('simple') !== '1'
  const advancedRef = useRef<HTMLDetailsElement>(null)
  const describeRef = useRef<HTMLTextAreaElement>(null)
  const zone = initial?.schedule.time_zone || localTimeZone()
  const [description, setDescription] = useState('')
  const [notes, setNotes] = useState<string[]>([])
  const [parseError, setParseError] = useState('')
  const [parsing, setParsing] = useState(false)
  const [name, setName] = useState(initial?.name ?? '')
  const [task, setTask] = useState(visibleTask(initial?.prompt ?? ''))
  const [preview, setPreview] = useState<AutomationPreview | null>(null)
  const [previewError, setPreviewError] = useState('')
  const [testing, setTesting] = useState(false)
  const installed = models.filter((model) => model.installed && canChat(model))
  const [profileID, setProfileID] = useState(initial?.profile_id || profiles.find((p) => p.id === 'general-assistant')?.id || profiles[0]?.id || '')
  const [modelID, setModelID] = useState(initial?.model_id || installed[0]?.id || '')
  const [responseLanguage, setResponseLanguage] = useState(initial?.response_language || 'account')
  const [schedule, setSchedule] = useState<AutomationSchedule>(initial?.schedule ?? { kind: 'daily', time_zone: zone, hour: 8, minute: 0 })
  const [mode, setMode] = useState(initial?.notification.mode ?? 'always')
  const [conditionKind, setConditionKind] = useState(initial?.notification.condition?.kind ?? 'threshold')
  const [op, setOp] = useState(initial?.notification.condition?.op ?? 'below')
  const [value, setValue] = useState(String(initial?.notification.condition?.value ?? ''))
  const [currency, setCurrency] = useState(initial?.notification.condition?.currency ?? 'USD')
  const [selectedTools, setSelectedTools] = useState<string[]>(initial?.tools ?? [])

  // The computer reads a request (#204), the same way for this form,
  // toskarctl, and chat; a model reads one its words can't.
  function fillFrom(parsed: ParsedAutomation) {
    setName(parsed.name)
    setTask(visibleTask(parsed.prompt))
    setSchedule(parsed.schedule)
    setMode(parsed.notification.mode)
    setConditionKind(parsed.notification.condition?.kind ?? 'threshold')
    setOp(parsed.notification.condition?.op ?? 'below')
    setValue(parsed.notification.condition?.value == null ? '' : String(parsed.notification.condition.value))
    if (parsed.notification.condition?.currency) setCurrency(parsed.notification.condition.currency)
    setNotes(parsed.notes ?? [])
    setParseError('')
  }

  const readRequest = (text: string, timeZone: string) =>
    api.parseAutomation(text, timeZone, i18n.resolvedLanguage ?? i18n.language)

  useEffect(() => {
    if (!seedDescription) return
    setDescription(seedDescription)
    let current = true
    setParsing(true)
    readRequest(seedDescription, zone)
      .then((parsed) => {
        if (current && parsed) fillFrom(parsed)
      })
      .catch((err: unknown) => {
        if (current) setParseError(err instanceof Error ? err.message : t('parse.unreadable'))
      })
      .finally(() => {
        if (current) setParsing(false)
      })
    return () => {
      current = false
    }
  }, [seedDescription, zone, t])

  useEffect(() => {
    if (profileID && profiles.some((profile) => profile.id === profileID)) return
    const next = profiles.find((profile) => profile.id === 'general-assistant')?.id || profiles[0]?.id
    if (next) setProfileID(next)
  }, [profiles, profileID])

  useEffect(() => {
    if (modelID) return
    const first = models.find((model) => model.installed && canChat(model))
    if (first) setModelID(first.id)
  }, [models, modelID])

  useEffect(() => {
    if (advancedRef.current) advancedRef.current.open = advanced
  }, [advanced])

  // A new automation starts at its first step; on a phone the form sits below
  // the list, so this also brings it into view.
  useEffect(() => {
    if (!initial) describeRef.current?.focus()
  }, [initial])

  // Tools a scheduled run may use are approved here, because nobody is
  // watching to answer later (spec §59). Tools that change things are listed
  // apart, so approving one is a deliberate choice.
  const lookTools = tools.filter((tool) => tool.enabled && tool.risk !== 'write')
  const changeTools = tools.filter((tool) => tool.enabled && tool.risk === 'write')

  async function applyDescription(text = description) {
    setParsing(true)
    try {
      const parsed = await readRequest(text, schedule.time_zone || zone)
      if (parsed) fillFrom(parsed)
    } catch (err) {
      setParseError(err instanceof Error ? err.message : t('parse.unreadable'))
    } finally {
      setParsing(false)
    }
  }

  function draft(notification = currentNotification()): AutomationInput {
    return {
      name: name.trim() || t('names.fallback'),
      prompt: visibleTask(task),
      profile_id: profileID,
      model_id: modelID,
      schedule,
      notification,
      tools: selectedTools,
      response_language: responseLanguage,
    }
  }

  function currentNotification(): AutomationInput['notification'] {
    if (mode !== 'condition') return { mode }
    if (conditionKind === 'threshold') {
      return { mode, condition: { kind: 'threshold', op, value: readNumber(value.trim()) ?? Number.NaN, currency } }
    }
    return { mode, condition: { kind: conditionKind } }
  }

  function submit() {
    if (!modelID || !task.trim()) return
    const notification = currentNotification()
    onSubmit({ ...draft(notification), name: name.trim() })
  }

  async function testDraft() {
    if (!modelID || !task.trim()) return
    setTesting(true)
    setPreview(null)
    setPreviewError('')
    try {
      setPreview(await api.previewAutomation(draft()))
    } catch (err) {
      setPreviewError(err instanceof Error ? err.message : t('form.testCouldNotRun'))
    } finally {
      setTesting(false)
    }
  }

  const summary = scheduleLabel(schedule)

  return (
    <form
      className="card space-y-4"
      onSubmit={(event) => {
        event.preventDefault()
        submit()
      }}
    >
      <div>
        <h2 className="section-title">{initial ? t('form.editTitle') : t('form.newTitle')}</h2>
        <p className="mt-1 text-sm text-ink-muted">{t('form.intro')}</p>
      </div>

      {/* Step 1: say it in words; Toskar fills in step 2 from them. */}
      <div className="space-y-2.5 rounded-lg bg-raised/40 p-3">
        <StepHeading number={1}>{t('form.stepDescribe')}</StepHeading>
        <label className="block space-y-1 text-sm">
          <span className="sr-only">{t('form.describe')}</span>
          <textarea
            ref={describeRef}
            className="field min-h-24 w-full"
            value={description}
            placeholder={t('form.describePlaceholder')}
            aria-describedby="automation-describe-hint"
            onChange={(event) => setDescription(event.target.value)}
          />
        </label>
        <p id="automation-describe-hint" className="text-xs leading-relaxed text-ink-muted">
          {fromIdea ? t('form.ideaHint') : t('form.describeHint')}
        </p>
        <div className="flex flex-wrap items-center gap-2">
          <button type="button" className="btn-secondary btn-sm" disabled={!description.trim() || parsing} onClick={() => void applyDescription()}>
            {parsing ? t('form.settingUp') : t('form.setUp')}
          </button>
        </div>
        {parseError && <p className="text-sm text-danger">{parseError}</p>}
        {notes.map((note) => (
          <p key={note} className="text-sm text-ink-muted">
            {note}
          </p>
        ))}
        {initial || !showIdeas ? null : (
          <div className="flex flex-wrap items-center gap-1.5 pt-1">
            <span className="text-xs text-ink-faint">{t('form.ideas')}</span>
            {IDEAS.map(({ id }) => (
              <button
                key={id}
                type="button"
                className="chat-suggestion"
                onClick={() => {
                  const request = t(`ideas.${id}.request`)
                  setDescription(request)
                  void applyDescription(request)
                }}
              >
                {t(`ideas.${id}.title`)}
              </button>
            ))}
          </div>
        )}
      </div>

      <StepHeading number={2}>{t('form.stepDetails')}</StepHeading>
      <label className="block space-y-1 text-sm">
        <span className="text-ink-muted">{t('form.name')}</span>
        <input className="field w-full" value={name} onChange={(event) => setName(event.target.value)} required />
      </label>
      <label className="block space-y-1 text-sm">
        <span className="text-ink-muted">{t('form.task')}</span>
        <textarea
          className="field min-h-28 w-full"
          value={task}
          aria-describedby="automation-task-hint"
          onChange={(event) => setTask(event.target.value)}
          required
        />
        <span id="automation-task-hint" className="block text-xs text-ink-faint">{t('form.taskHint')}</span>
      </label>
      <p className="text-sm text-ink-muted">{t('form.schedule')}</p>
      <div className="grid gap-3 sm:grid-cols-2">
        <label className="block space-y-1 text-sm">
          <span className="text-ink-muted">{t('form.repeats')}</span>
          <select
            className="field w-full"
            value={schedule.kind}
            onChange={(event) => setSchedule(changeKind(schedule, event.target.value as AutomationSchedule['kind']))}
          >
            <option value="daily">{t('form.kinds.daily')}</option>
            <option value="weekly">{t('form.kinds.weekly')}</option>
            <option value="interval">{t('form.kinds.interval')}</option>
            <option value="once">{t('form.kinds.once')}</option>
          </select>
        </label>
        <ScheduleFields schedule={schedule} onChange={setSchedule} />
      </div>
      <fieldset className="space-y-2">
        <legend className="text-sm text-ink-muted">{t('form.notifyMe')}</legend>
        {NOTIFY_CHOICES.map((choice) => (
          <label key={choice} className="flex items-center gap-2 text-sm text-ink">
            <input type="radio" name="notify" checked={mode === choice} onChange={() => setMode(choice)} />
            {t(`form.notifyChoices.${choice}`)}
          </label>
        ))}
        <p className="text-xs text-ink-faint">{t('form.noticesGo')}</p>
      </fieldset>
      {mode === 'condition' && (
        <div className="grid gap-3 sm:grid-cols-2">
          <label className="block space-y-1 text-sm">
            <span className="text-ink-muted">{t('form.condition')}</span>
            <select
              className="field w-full"
              value={conditionKind}
              onChange={(event) => setConditionKind(event.target.value as 'threshold' | 'available' | 'significant')}
            >
              <option value="threshold">{t('form.conditions.threshold')}</option>
              <option value="available">{t('form.conditions.available')}</option>
              <option value="significant">{t('form.conditions.significant')}</option>
            </select>
          </label>
          {conditionKind === 'threshold' && (
            <>
              <label className="block space-y-1 text-sm">
                <span className="text-ink-muted">{t('form.priceIs')}</span>
                <select className="field w-full" value={op} onChange={(event) => setOp(event.target.value as 'below' | 'above')}>
                  <option value="below">{t('form.below')}</option>
                  <option value="above">{t('form.above')}</option>
                </select>
              </label>
              <label className="block space-y-1 text-sm">
                <span className="text-ink-muted">{t('form.amount')}</span>
                <input className="field w-full" inputMode="decimal" value={value} onChange={(event) => setValue(event.target.value)} required />
              </label>
              <label className="block space-y-1 text-sm">
                <span className="text-ink-muted">{t('form.currency')}</span>
                <select className="field w-full" value={currency} onChange={(event) => setCurrency(event.target.value)}>
                  {(CURRENCIES.includes(currency) ? CURRENCIES : [currency, ...CURRENCIES]).map((code) => (
                    <option key={code} value={code}>
                      {currencyName(code)}
                    </option>
                  ))}
                </select>
              </label>
            </>
          )}
        </div>
      )}
      {installed.length === 0 && <p className="text-sm text-danger">{t('form.installModel')}</p>}
      <details ref={advancedRef} className="space-y-3">
        <summary className="cursor-pointer text-sm text-ink-muted">{t('form.advanced')}</summary>
        <label className="block space-y-1 text-sm">
          <span className="text-ink-muted">{t('form.profile')}</span>
          <select className="field w-full" value={profileID} onChange={(event) => setProfileID(event.target.value)}>
            {profiles.length === 0 && <option value="">{t('form.noProfiles')}</option>}
            {profiles.map((profile) => (
              <option key={profile.id} value={profile.id}>
                {profile.name}
              </option>
            ))}
          </select>
        </label>
        <label className="block space-y-1 text-sm">
          <span className="text-ink-muted">{t('form.model')}</span>
          <select className="field w-full" value={modelID} onChange={(event) => setModelID(event.target.value)} required>
            {installed.length === 0 && <option value="">{t('form.installFirst')}</option>}
            {installed.length > 0 && <option value="auto">{t('form.auto')}</option>}
            {installed.map((model) => (
              <option key={model.id} value={model.id}>
                {model.display_name || model.id}
              </option>
            ))}
          </select>
        </label>
        <label className="block space-y-1 text-sm">
          <span className="text-ink-muted">{t('form.responseLanguage')}</span>
          <select className="field w-full" value={responseLanguage} onChange={(event) => setResponseLanguage(event.target.value)}>
            <option value="account">{t('form.responseAccount')}</option>
            <option value="app">{t('form.responseApp')}</option>
            <option value="auto">{t('form.responseAuto')}</option>
            {[...answerLanguages, ...(['account', 'app', 'auto', ...answerLanguages].includes(responseLanguage) ? [] : [responseLanguage])].map((tag) => (
              <option key={tag} value={tag} lang={tag}>
                {nameInItself(tag)}
              </option>
            ))}
          </select>
        </label>
        <label className="block space-y-1 text-sm">
          <span className="text-ink-muted">{t('form.timeZone')}</span>
          <input
            className="field w-full"
            value={schedule.time_zone}
            onChange={(event) => setSchedule({ ...schedule, time_zone: event.target.value })}
            required
          />
        </label>
        {mode === 'condition' && (
          <p className="text-xs text-ink-faint">{t('form.conditionHint')}</p>
        )}
        <fieldset className="space-y-2">
          <legend className="text-sm text-ink-muted">{t('form.tools')}</legend>
          <p className="text-xs text-ink-faint">{t('form.toolsHint')}</p>
          <div className="grid gap-2 sm:grid-cols-2">
            {lookTools.map((tool) => (
              <label key={tool.id} className="flex items-start gap-2 text-sm text-ink">
                <input
                  type="checkbox"
                  className="mt-1"
                  checked={selectedTools.includes(tool.id)}
                  onChange={(event) => {
                    setSelectedTools((current) =>
                      event.target.checked ? [...current, tool.id] : current.filter((id) => id !== tool.id),
                    )
                  }}
                />
                <span>
                  {tool.name}
                  <span className="block text-xs text-ink-faint">{tool.description}</span>
                </span>
              </label>
            ))}
          </div>
          {changeTools.length > 0 && (
            <div className="rounded-lg border border-warning/30 bg-warning/5 p-3">
              <p className="text-xs font-medium text-ink">{t('form.changeTools')}</p>
              <p className="mb-2 text-xs text-ink-faint">{t('form.changeToolsHint')}</p>
            <div className="grid gap-2 sm:grid-cols-2">
              {changeTools.map((tool) => (
                <label key={tool.id} className="flex items-start gap-2 text-sm text-ink">
                  <input
                    type="checkbox"
                    className="mt-1"
                    checked={selectedTools.includes(tool.id)}
                    onChange={(event) => {
                      setSelectedTools((current) =>
                        event.target.checked ? [...current, tool.id] : current.filter((id) => id !== tool.id),
                      )
                    }}
                  />
                  <span>
                    {tool.name}
                    <span className="block text-xs text-ink-faint">{tool.description}</span>
                  </span>
                </label>
              ))}
            </div>
            </div>
          )}
        </fieldset>
      </details>
      {/* What will happen, in plain words, before it is created. */}
      <section className="rounded-lg border border-line/70 p-3" aria-labelledby="automation-recap">
        <h3 id="automation-recap" className="label-caps">{t('form.recap.title')}</h3>
        <dl className="mt-2 grid gap-x-4 gap-y-1.5 text-sm sm:grid-cols-[auto_1fr]">
          <dt className="text-ink-muted">{t('form.recap.when')}</dt>
          <dd className="text-ink">{summary}</dd>
          <dt className="text-ink-muted">{t('form.recap.does')}</dt>
          <dd className="line-clamp-3 whitespace-pre-wrap text-ink">{task.trim() || '—'}</dd>
          <dt className="text-ink-muted">{t('form.recap.tells')}</dt>
          <dd className="text-ink">{notificationLabel(previewNotification(mode, conditionKind, op, value, currency))}</dd>
        </dl>
      </section>
      {previewError && <p className="text-sm text-danger">{previewError}</p>}
      {preview && (
        <div className="rounded-lg bg-raised/50 p-3 text-sm">
          <p className="font-medium text-ink">{testing ? t('form.testing') : preview.error ? t('form.testFailed') : preview.would_notify ? t('form.wouldNotify') : t('form.wouldNotNotify')}</p>
          {resultProse(preview.result) && <p className="mt-2 whitespace-pre-wrap text-ink-muted">{resultProse(preview.result)}</p>}
          {preview.error && <p className="mt-2 text-danger">{preview.error}</p>}
        </div>
      )}
      {testing && !preview && <p className="text-sm text-ink-muted">{t('form.testing')}</p>}
      {error && <p className="text-sm text-danger">{error}</p>}
      <div className="flex flex-wrap items-center gap-2">
        <button type="submit" className="btn-primary" disabled={pending || testing || profiles.length === 0 || !modelID || !task.trim()}>
          {pending ? t('form.saving') : initial ? t('form.saveChanges') : t('form.create')}
        </button>
        <button
          type="button"
          className="btn-secondary"
          disabled={testing || pending || !modelID || !task.trim()}
          title={t('form.testHint')}
          aria-describedby="automation-test-hint"
          onClick={() => void testDraft()}
        >
          {testing ? t('form.testingShort') : t('form.testRun')}
        </button>
        <button type="button" className="ms-auto px-2 py-2 text-sm font-medium text-ink-muted hover:text-ink" onClick={onCancel}>
          {t('form.cancel')}
        </button>
      </div>
      <p id="automation-test-hint" className="text-xs text-ink-faint">{t('form.testHint')}</p>
    </form>
  )
}

/** A numbered step of the form. */
function StepHeading({ number, children }: { number: number; children: string }) {
  return (
    <h3 className="flex items-center gap-2 text-sm font-semibold text-ink">
      <span className="flex h-5 w-5 items-center justify-center rounded-full bg-primary-soft text-[11px] text-primary-active" aria-hidden>
        {number}
      </span>
      {children}
    </h3>
  )
}

function ScheduleFields({
  schedule,
  onChange,
}: {
  schedule: AutomationSchedule
  onChange: (schedule: AutomationSchedule) => void
}) {
  const { t } = useTranslation('automations')
  if (schedule.kind === 'interval') {
    const { amount, unit } = splitInterval(schedule.every_seconds ?? 6 * 3600)
    return (
      <div className="grid grid-cols-[repeat(auto-fit,minmax(8rem,1fr))] gap-3">
        <label className="block space-y-1 text-sm">
          <span className="text-ink-muted">{t('form.every')}</span>
          <input
            className="field w-full"
            type="number"
            min={1}
            value={amount}
            onChange={(event) => onChange({ ...schedule, every_seconds: Math.max(1, Number(event.target.value) || 1) * unitSeconds(unit) })}
          />
        </label>
        <label className="block space-y-1 text-sm">
          <span className="text-ink-muted">{t('form.unit')}</span>
          <select
            className="field w-full"
            value={unit}
            onChange={(event) => {
              const next = event.target.value as IntervalUnit
              onChange({ ...schedule, every_seconds: amount * unitSeconds(next) })
            }}
          >
            <option value="minutes">{t('form.units.minutes')}</option>
            <option value="hours">{t('form.units.hours')}</option>
            <option value="days">{t('form.units.days')}</option>
          </select>
        </label>
      </div>
    )
  }
  if (schedule.kind === 'once') {
    return (
      <label className="block space-y-1 text-sm">
        <span className="text-ink-muted">{t('form.when')}</span>
        <input
          className="field w-full"
          type="datetime-local"
          value={civilInputValue(schedule.at, schedule.time_zone)}
          onChange={(event) => onChange({ ...schedule, at: event.target.value ? civilToISO(event.target.value, schedule.time_zone) : undefined })}
          required
        />
      </label>
    )
  }
  return (
    <div className="grid grid-cols-[repeat(auto-fit,minmax(8rem,1fr))] gap-3">
      <label className="block space-y-1 text-sm">
        <span className="text-ink-muted">{t('form.time')}</span>
        <input
          className="field w-full"
          type="time"
          value={`${String(schedule.hour ?? 0).padStart(2, '0')}:${String(schedule.minute ?? 0).padStart(2, '0')}`}
          onChange={(event) => {
            const [hour, minute] = event.target.value.split(':').map(Number)
            onChange({ ...schedule, hour, minute })
          }}
          required
        />
      </label>
      {schedule.kind === 'weekly' && (
        <label className="block space-y-1 text-sm">
          <span className="text-ink-muted">{t('form.day')}</span>
          <select
            className="field w-full"
            value={schedule.weekday ?? 1}
            onChange={(event) => onChange({ ...schedule, weekday: Number(event.target.value) })}
          >
            {[0, 1, 2, 3, 4, 5, 6].map((index) => (
              <option key={index} value={index}>
                {weekdayName(index)}
              </option>
            ))}
          </select>
        </label>
      )}
    </div>
  )
}

type IntervalUnit = 'minutes' | 'hours' | 'days'

function unitSeconds(unit: IntervalUnit): number {
  if (unit === 'days') return 86400
  if (unit === 'hours') return 3600
  return 60
}

function splitInterval(seconds: number): { amount: number; unit: IntervalUnit } {
  if (seconds % 86400 === 0 && seconds >= 86400) return { amount: seconds / 86400, unit: 'days' }
  if (seconds % 3600 === 0 && seconds >= 3600) return { amount: seconds / 3600, unit: 'hours' }
  return { amount: Math.max(1, Math.round(seconds / 60)), unit: 'minutes' }
}

function changeKind(schedule: AutomationSchedule, kind: AutomationSchedule['kind']): AutomationSchedule {
  if (kind === 'once') return { kind, time_zone: schedule.time_zone, at: schedule.at }
  if (kind === 'interval') return { kind, time_zone: schedule.time_zone, every_seconds: schedule.every_seconds || 6 * 3600 }
  if (kind === 'weekly') {
    return { kind, time_zone: schedule.time_zone, hour: schedule.hour ?? 8, minute: schedule.minute ?? 0, weekday: schedule.weekday ?? 1 }
  }
  return { kind: 'daily', time_zone: schedule.time_zone, hour: schedule.hour ?? 8, minute: schedule.minute ?? 0 }
}

function previewNotification(
  mode: AutomationInput['notification']['mode'],
  kind: string,
  op: string,
  value: string,
  currency: string,
): AutomationInput['notification'] {
  if (mode !== 'condition') return { mode }
  if (kind === 'threshold') {
    return { mode, condition: { kind: 'threshold', op: op === 'above' ? 'above' : 'below', value: readNumber(value.trim()) || 0, currency } }
  }
  if (kind === 'available') return { mode, condition: { kind: 'available' } }
  return { mode, condition: { kind: 'significant' } }
}
