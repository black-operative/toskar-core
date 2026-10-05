export interface ScreenshotLaunch {
  enabled: boolean
  screen: string
  path: string
}

const routes: Record<string, string> = {
  chat: '/chat',
  models: '/models',
  computers: '/nodes',
  performance: '/performance',
  diagnostics: '/diagnostics',
  'api-manager': '/api-access',
  automations: '/automations',
  'automations-create': '/automations',
  train: '/train',
  knowledge: '/knowledge',
  memory: '/memory',
  tools: '/tools',
  profiles: '/profiles',
}

export function screenshotPath(screen: string): string {
  return routes[screen] ?? '/chat'
}

export function readScreenshotLaunch(): ScreenshotLaunch | null {
  if (typeof window === 'undefined') {
    return null
  }
  const w = window as YggdrasilWindow
  const fromBoot = w.__TOSKAR_SCREENSHOT__ ?? w.__YGGDRASIL_SCREENSHOT__
  if (fromBoot?.enabled) {
    const screen = fromBoot.screen || 'chat'
    return { enabled: true, screen, path: screenshotPath(screen) }
  }
  const params = new URLSearchParams(window.location.search)
  if (params.get('screenshot') !== '1') {
    return null
  }
  const screen = params.get('screen') || 'chat'
  return { enabled: true, screen, path: screenshotPath(screen) }
}
