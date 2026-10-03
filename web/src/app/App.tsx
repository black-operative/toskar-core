import type { ReactNode } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { LifecycleHost } from '@/components/LifecycleHost'
import { NotificationHost } from '@/components/NotificationHost'
import { ServiceBootGate } from '@/components/ServiceBootGate'
import { LanguageSync } from '@/components/LanguageSync'
import { ThemeSync } from '@/components/ThemeSync'
import { AppLayout } from '@/components/layout/AppLayout'
import { ScreenshotMode } from '@/app/ScreenshotMode'
import { readScreenshotLaunch } from '@/lib/screenshotMode'
import { AutomationsPage } from '@/features/automations/AutomationsPage'
import { ApiAccessPage } from '@/features/api-access/ApiAccessPage'
import { ChatPage } from '@/features/chat/ChatPage'
import { DiagnosticsPage } from '@/features/diagnostics/DiagnosticsPage'
import { ModelsPage } from '@/features/models/ModelsPage'
import { NodesPage } from '@/features/nodes/NodesPage'
import { OnboardingPage } from '@/features/onboarding/OnboardingPage'
import { PerformancePage } from '@/features/performance/PerformancePage'
import { ProfilesPage } from '@/features/profiles/ProfilesPage'
import { SettingsPage } from '@/features/settings/SettingsPage'
import { ToolsPage } from '@/features/tools/ToolsPage'
import { KnowledgePage } from '@/features/knowledge/KnowledgePage'
import { MemoryPage } from '@/features/memory/MemoryPage'
import { TrainPage } from '@/features/train/TrainPage'
import { useUIStore } from '@/stores/uiStore'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      refetchOnWindowFocus: false,
      // One retry, not three: a failed request shows its error with Try
      // again in about a second, rather than a spinner for seven.
      retry: 1,
    },
  },
})

function OnboardingGate({ children }: { children: ReactNode }) {
  const onboardingComplete = useUIStore((s) => s.onboardingComplete)
  if (readScreenshotLaunch()?.enabled || onboardingComplete) {
    return <>{children}</>
  }
  return <Navigate to="/onboarding" replace />
}

export function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <ThemeSync />
      <LanguageSync />
      <LifecycleHost>
        <NotificationHost />
        <BrowserRouter>
          <ScreenshotMode />
          <ServiceBootGate>
            <Routes>
              <Route path="/onboarding" element={<OnboardingPage />} />
              <Route
                element={
                  <OnboardingGate>
                    <AppLayout />
                  </OnboardingGate>
                }
              >
                <Route index element={<Navigate to="/chat" replace />} />
                <Route path="chat" element={<ChatPage />} />
                <Route path="automations" element={<AutomationsPage />} />
                <Route path="models" element={<ModelsPage />} />
                <Route path="train" element={<TrainPage />} />
                <Route path="knowledge" element={<KnowledgePage />} />
                <Route path="memory" element={<MemoryPage />} />
                <Route path="profiles" element={<ProfilesPage />} />
                <Route path="tools" element={<ToolsPage />} />
                <Route path="nodes" element={<NodesPage />} />
                <Route path="api-access" element={<ApiAccessPage />} />
                <Route path="diagnostics" element={<DiagnosticsPage />} />
                <Route path="performance" element={<PerformancePage />} />
                <Route path="settings" element={<SettingsPage />} />
                {/* The old "coming soon" Orchestrators page; orchestration is chosen per profile. */}
                <Route path="orchestrators" element={<Navigate to="/profiles" replace />} />
              </Route>
              <Route path="*" element={<Navigate to="/chat" replace />} />
            </Routes>
          </ServiceBootGate>
        </BrowserRouter>
      </LifecycleHost>
    </QueryClientProvider>
  )
}
