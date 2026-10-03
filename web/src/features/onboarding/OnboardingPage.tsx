import { useMutation, useQuery } from '@tanstack/react-query'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import { useNavigate } from 'react-router-dom'
import { LoadingSpinner } from '@/components/ui/LoadingSpinner'
import { Ratatoskr } from '@/components/ui/Ratatoskr'
import { useMascotState } from '@/lib/ratatoskr/useMascotState'
import { api } from '@/lib/api'
import { subscribeEvents } from '@/lib/events'
import { bytesToGb, formatBytes } from '@/lib/format'
import {
  buildProfileFromRecommendation,
  presetOptions,
  purposeToApiQuery,
} from '@/lib/profilePresets'
import { useUIStore } from '@/stores/uiStore'
import type { ModelDownloadProgressPayload, Purpose, Recommendation } from '@/types/api'
import { formatPercent } from '@/i18n/format'

type OnboardingStep = 'setup' | 'recommend' | 'installing' | 'ready'

type InstallPhase =
  | 'runtime'
  | 'models'
  | 'profile'
  | 'conversation'
  | 'done'

export function OnboardingPage() {
  const { t } = useTranslation('onboarding')
  const navigate = useNavigate()
  const setOnboardingComplete = useUIStore((s) => s.setOnboardingComplete)
  const setActiveProfileId = useUIStore((s) => s.setActiveProfileId)

  const [step, setStep] = useState<OnboardingStep>('setup')
  const [selectedPurpose, setSelectedPurpose] = useState<Purpose>('general')
  const [recommendation, setRecommendation] = useState<Recommendation | null>(null)
  const [installError, setInstallError] = useState<string | null>(null)
  const [phase, setPhase] = useState<InstallPhase>('runtime')
  const [phaseDetail, setPhaseDetail] = useState('')
  const [downloadProgress, setDownloadProgress] = useState<Record<string, number>>({})
  const [readyConversationId, setReadyConversationId] = useState<string | null>(null)

  const hardwareQuery = useQuery({
    queryKey: ['hardware'],
    queryFn: () => api.getHardware(),
    retry: false,
  })

  const recommendMutation = useMutation({
    mutationFn: () => api.recommendModels(purposeToApiQuery(selectedPurpose)),
    onSuccess: (rec) => {
      if (rec) {
        setRecommendation(rec)
        setStep('recommend')
      }
    },
    onError: (error: Error) => {
      setInstallError(error.message || t('recommendFailed'))
    },
  })

  useEffect(() => {
    if (step !== 'installing') {
      return
    }
    return subscribeEvents({
      onEvent: (event) => {
        if (event.type === 'model.download.progress') {
          const payload = event.payload as ModelDownloadProgressPayload | undefined
          if (!payload?.model_id) {
            return
          }
          setDownloadProgress((current) => ({
            ...current,
            [payload.model_id]: payload.percent ?? 0,
          }))
          setPhaseDetail(t('install.downloading', { model: payload.model_id, percent: formatPercent((payload.percent ?? 0) / 100) }))
        }
        if (event.type === 'model.download.completed') {
          const modelId = event.payload?.model_id as string | undefined
          if (modelId) {
            setDownloadProgress((current) => ({ ...current, [modelId]: 100 }))
          }
        }
        if (event.type === 'model.download.failed') {
          const message = (event.payload?.error as string) || t('install.downloadFailed')
          setInstallError(message)
        }
      },
    })
  }, [step, t])

  const installMutation = useMutation({
    mutationFn: async (rec: Recommendation) => {
      setInstallError(null)
      setStep('installing')
      setDownloadProgress({})

      setPhase('runtime')
      setPhaseDetail(t('install.runtime'))
      await api.installRuntime('llamacpp')

      setPhase('models')
      const uniqueModels = uniqueById(rec.models)
      for (const model of uniqueModels) {
        setPhaseDetail(t('install.model', { model: model.display_name }))
        await api.installModel(model.id, { wait: true })
        setDownloadProgress((current) => ({ ...current, [model.id]: 100 }))
      }

      setPhase('profile')
      setPhaseDetail(t('install.profile'))
      const profileBody = buildProfileFromRecommendation(selectedPurpose, rec)
      const profile = await api.createProfile(profileBody)
      if (!profile?.id) {
        throw new Error(t('install.noProfileId'))
      }
      setActiveProfileId(profile.id)

      setPhase('conversation')
      setPhaseDetail(t('install.conversation'))
      const conversation = await api.createConversation({
        title: t('install.chatTitle', { profile: profile.name }),
        profile_id: profile.id,
      })

      setPhase('done')
      return { profileId: profile.id, conversationId: conversation?.id ?? null }
    },
    onSuccess: (result) => {
      setReadyConversationId(result.conversationId)
      setStep('ready')
    },
    onError: (error: Error) => {
      setInstallError(error.message || t('install.failed'))
      setStep('recommend')
    },
  })

  const handleContinue = () => {
    setInstallError(null)
    if (selectedPurpose === 'custom') {
      setOnboardingComplete(true)
      navigate('/models')
      return
    }
    recommendMutation.mutate()
  }

  const handleSkipInstall = () => {
    setOnboardingComplete(true)
    navigate('/chat')
  }

  const handleStartChatting = () => {
    setOnboardingComplete(true)
    if (readyConversationId) {
      navigate(`/chat?c=${readyConversationId}`)
    } else {
      navigate('/chat')
    }
  }

  // Ratatoskr waves through setup and celebrates once when it is ready.
  const mascot = useMascotState({ base: step === 'ready' ? 'idle' : 'greet', react: [], celebrate: step === 'ready' })

  const hardware = hardwareQuery.data
  const acceleratorLabels =
    hardware?.accelerators?.map((a) => a.model).filter(Boolean) ?? []

  const overallProgress = useMemo(() => {
    if (!recommendation || step !== 'installing') {
      return 0
    }
    if (phase === 'runtime') {
      return 5
    }
    if (phase === 'models') {
      const ids = uniqueById(recommendation.models).map((m) => m.id)
      if (ids.length === 0) {
        return 40
      }
      const sum = ids.reduce((acc, id) => acc + (downloadProgress[id] ?? 0), 0)
      return 10 + Math.round((sum / ids.length) * 0.75)
    }
    if (phase === 'profile') {
      return 90
    }
    if (phase === 'conversation') {
      return 96
    }
    return 100
  }, [recommendation, step, phase, downloadProgress])

  return (
    <main className="mx-auto flex h-full min-h-0 w-full max-w-3xl flex-col justify-center overflow-y-auto px-6 py-12">
      <header className="mb-10 text-center">
        <Ratatoskr state={mascot} size={160} className="mx-auto mb-3" />
        <p className="mb-3 text-sm font-medium uppercase tracking-[0.18em] text-accent">
          {t('header.welcome')}
        </p>
        <h1 className="font-display text-5xl font-semibold tracking-tight text-ink md:text-6xl">
          Yggdrasil
        </h1>
        <p className="mx-auto mt-4 max-w-xl text-lg leading-relaxed text-ink-muted">
          {t('header.lede')}
        </p>
      </header>

      {step === 'setup' && (
        <>
          <section className="card mb-6">
            <h2 className="font-display text-xl font-semibold text-ink">{t('hardware.title')}</h2>
            <p className="mt-1 text-sm text-ink-muted">{t('hardware.description')}</p>

            <div className="mt-4">
              {hardwareQuery.isLoading && <LoadingSpinner label={t('hardware.detecting')} />}
              {hardware ? (
                <dl className="grid gap-3 sm:grid-cols-2">
                  {hardware.cpu?.model && (
                    <div>
                      <dt className="text-xs uppercase tracking-wide text-ink-muted">
                        {t('hardware.processor')}
                      </dt>
                      <dd className="text-sm font-medium text-ink">
                        {hardware.cpu.model}
                        {hardware.cpu.cores ? ` · ${t('hardware.cores', { count: hardware.cpu.cores })}` : ''}
                      </dd>
                    </div>
                  )}
                  {hardware.memory?.total_bytes != null && (
                    <div>
                      <dt className="text-xs uppercase tracking-wide text-ink-muted">{t('hardware.memory')}</dt>
                      <dd className="text-sm font-medium text-ink">
                        {bytesToGb(hardware.memory.total_bytes)}
                      </dd>
                    </div>
                  )}
                  {acceleratorLabels.length > 0 && (
                    <div className="sm:col-span-2">
                      <dt className="text-xs uppercase tracking-wide text-ink-muted">
                        {t('hardware.graphics')}
                      </dt>
                      <dd className="text-sm font-medium text-ink">
                        {acceleratorLabels.join(', ')}
                      </dd>
                    </div>
                  )}
                  {hardware.os && (
                    <div>
                      <dt className="text-xs uppercase tracking-wide text-ink-muted">{t('hardware.system')}</dt>
                      <dd className="text-sm font-medium text-ink">
                        {hardware.os} / {hardware.arch}
                      </dd>
                    </div>
                  )}
                </dl>
              ) : (
                !hardwareQuery.isLoading && (
                  <p className="text-sm text-ink-muted">{t('hardware.unavailable')}</p>
                )
              )}
            </div>
          </section>

          <section className="card mb-8">
            <h2 id="onboarding-purpose" className="font-display text-xl font-semibold text-ink">{t('question')}</h2>
            {/* One choice: each card says whether it is the chosen one. */}
            <div role="group" aria-labelledby="onboarding-purpose" className="mt-4 grid gap-3 sm:grid-cols-2">
              {presetOptions.map((option) => {
                const selected = selectedPurpose === option.id
                return (
                  <button
                    key={option.id}
                    type="button"
                    aria-pressed={selected}
                    onClick={() => setSelectedPurpose(option.id)}
                    className={[
                      'selectable',
                      selected ? 'selectable-active' : '',
                    ]
                      .filter(Boolean)
                      .join(' ')}
                  >
                    <p className="font-semibold text-ink">{t(`purposes.${option.id}.title`)}</p>
                    <p className="mt-1 text-sm text-ink-muted">{t(`purposes.${option.id}.description`)}</p>
                  </button>
                )
              })}
            </div>
          </section>

          {installError && (
            <p className="mb-4 rounded-lg bg-danger/10 px-4 py-3 text-sm text-danger">
              {installError}
            </p>
          )}

          <div className="flex justify-center gap-3">
            <button
              type="button"
              className="btn-primary px-8"
              onClick={handleContinue}
              disabled={recommendMutation.isPending}
            >
              {recommendMutation.isPending ? t('finding') : t('continue')}
            </button>
          </div>
        </>
      )}

      {step === 'recommend' && recommendation && (
        <>
          <section className="card mb-6">
            <div className="mb-3 flex flex-wrap items-center gap-2">
              <span className="badge-preferred">{t('recommend.badge')}</span>
              <span className="badge-mimir">Mimir</span>
            </div>
            <h2 className="font-display text-xl font-semibold text-ink">
              {t('recommend.title', { purpose: t(`purposes.${selectedPurpose}.title`) })}
            </h2>
            <p className="mt-2 text-sm leading-relaxed text-ink-muted">
              {recommendation.reason}
            </p>
            {recommendation.community_chosen ? (
              <p className="mt-1 text-xs text-ink-muted">{t('recommend.communityChosen')}</p>
            ) : null}

            <ul className="mt-4 space-y-3">
              {recommendation.roles.map((role) => {
                const model = recommendation.models.find((m) => m.id === role.model_id)
                return (
                  <li
                    key={`${role.role}-${role.model_id}`}
                    className="rounded-lg border border-line bg-raised/50 px-4 py-3"
                  >
                    <p className="label-caps">{friendlyRole(role.role, t)}</p>
                    <p className="font-medium text-ink">
                      {model?.display_name ?? role.model_id}
                    </p>
                    {model?.size_bytes ? (
                      <p className="mt-1 text-xs text-ink-muted">
                        {t('recommend.download', { size: formatBytes(model.size_bytes) })}
                      </p>
                    ) : null}
                  </li>
                )
              })}
            </ul>

            <dl className="mt-4 grid gap-2 text-sm sm:grid-cols-2">
              <div>
                <dt className="text-xs uppercase tracking-wide text-ink-muted">
                  {t('recommend.storage')}
                </dt>
                <dd className="font-medium text-ink">
                  {formatBytes(recommendation.estimated_storage_bytes)}
                </dd>
              </div>
              <div>
                <dt className="text-xs uppercase tracking-wide text-ink-muted">
                  {t('recommend.memory')}
                </dt>
                <dd className="font-medium text-ink">
                  {formatBytes(recommendation.estimated_vram_bytes)}
                </dd>
              </div>
            </dl>
          </section>

          {installError && (
            <p className="mb-4 rounded-lg bg-danger/10 px-4 py-3 text-sm text-danger">
              {installError}
            </p>
          )}

          <div className="flex flex-wrap justify-center gap-3">
            <button
              type="button"
              className="btn-primary px-8"
              onClick={() => installMutation.mutate(recommendation)}
              disabled={installMutation.isPending}
            >
              {t('recommend.install')}
            </button>
            <button type="button" className="btn-secondary px-6" onClick={handleSkipInstall}>
              {t('recommend.skip')}
            </button>
            <button
              type="button"
              className="btn-secondary px-6"
              onClick={() => setStep('setup')}
            >
              {t('recommend.back')}
            </button>
          </div>
        </>
      )}

      {step === 'installing' && (
        <div className="card py-10">
          <div className="mx-auto flex max-w-md flex-col items-center text-center">
            <LoadingSpinner label={phaseDetail || t('install.working')} />
            <div className="mt-6 h-2 w-full overflow-hidden rounded-full bg-raised">
              <div
                className="h-full rounded-full bg-primary transition-all duration-500"
                style={{ width: `${overallProgress}%` }}
              />
            </div>
            <p className="mt-3 text-xs text-ink-muted">{t('install.complete', { percent: formatPercent(overallProgress / 100) })}</p>
            <p className="mt-4 text-sm text-ink-muted">{t('install.keepOpen')}</p>
          </div>
        </div>
      )}

      {step === 'ready' && (
        <div className="card flex flex-col items-center py-12 text-center">
          <h2 className="font-display text-2xl font-semibold text-ink">{t('ready.title')}</h2>
          <p className="mt-2 max-w-md text-sm text-ink-muted">{t('ready.description')}</p>
          <button type="button" className="btn-primary mt-6 px-8" onClick={handleStartChatting}>
            {t('ready.start')}
          </button>
        </div>
      )}
    </main>
  )
}

function uniqueById<T extends { id: string }>(items: T[]): T[] {
  const seen = new Set<string>()
  const out: T[] = []
  for (const item of items) {
    if (seen.has(item.id)) {
      continue
    }
    seen.add(item.id)
    out.push(item)
  }
  return out
}

// Roles the catalog names (onboarding:roles.<role>); any other shows as the recommendation gives it.
const ROLES = ['coordinator', 'worker', 'reviewer', 'researcher', 'assistant']

function friendlyRole(role: string, t: TFunction<'onboarding'>): string {
  return ROLES.includes(role) ? t(`roles.${role}`) : role
}
