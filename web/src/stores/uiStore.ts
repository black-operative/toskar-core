import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import { moveStored } from '@/lib/storage'

export type ThemePreference = 'light' | 'dark' | 'system'

const defaultUIState = {
  advancedMode: false,
  theme: 'dark' as ThemePreference,
  onboardingComplete: false,
  activeProfileId: null as string | null,
  chatHistoryPinned: false,
  pinnedConversationIds: [] as string[],
}

interface UIState {
  advancedMode: boolean
  theme: ThemePreference
  onboardingComplete: boolean
  activeProfileId: string | null
  chatHistoryPinned: boolean
  pinnedConversationIds: string[]
  setAdvancedMode: (enabled: boolean) => void
  setTheme: (theme: ThemePreference) => void
  setOnboardingComplete: (complete: boolean) => void
  setActiveProfileId: (id: string | null) => void
  setChatHistoryPinned: (pinned: boolean) => void
  togglePinnedConversation: (id: string) => void
  resetToDefaults: () => void
}

export function resolveTheme(preference: ThemePreference): 'light' | 'dark' {
  if (preference === 'system') {
    if (typeof window !== 'undefined' && window.matchMedia('(prefers-color-scheme: light)').matches) {
      return 'light'
    }
    return 'dark'
  }
  return preference
}

export function applyTheme(preference: ThemePreference) {
  if (typeof document === 'undefined') {
    return
  }
  const resolved = resolveTheme(preference)
  document.documentElement.dataset.theme = resolved
  document.documentElement.style.colorScheme = resolved
}

// Moved before the store reads it (#237).
moveStored('yggdrasil-ui', 'toskar-ui')

export const useUIStore = create<UIState>()(
  persist(
    (set) => ({
      ...defaultUIState,
      setAdvancedMode: (enabled) => set({ advancedMode: enabled }),
      setTheme: (theme) => {
        applyTheme(theme)
        set({ theme })
      },
      setOnboardingComplete: (complete) => set({ onboardingComplete: complete }),
      setActiveProfileId: (id) => set({ activeProfileId: id }),
      setChatHistoryPinned: (pinned) => set({ chatHistoryPinned: pinned }),
      togglePinnedConversation: (id) =>
        set((state) => {
          const has = state.pinnedConversationIds.includes(id)
          return {
            pinnedConversationIds: has
              ? state.pinnedConversationIds.filter((x) => x !== id)
              : [...state.pinnedConversationIds, id],
          }
        }),
      resetToDefaults: () => {
        applyTheme(defaultUIState.theme)
        set({ ...defaultUIState })
      },
    }),
    {
      name: 'toskar-ui',
      onRehydrateStorage: () => (state) => {
        applyTheme(state?.theme ?? defaultUIState.theme)
      },
    },
  ),
)
