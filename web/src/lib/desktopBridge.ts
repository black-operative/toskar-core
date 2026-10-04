/** Optional bridge to the Wails desktop shell (no-op in the browser). */

type WailsApp = {
  SetBackgroundMode?: (v: boolean) => Promise<void>
  QuitAndStopDaemon?: () => Promise<void>
  OpenPath?: (path: string) => Promise<void>
  SetLaunchAtLogin?: (v: boolean) => Promise<void>
  IsLaunchAtLogin?: () => Promise<boolean>
  MarkScreenshotReady?: () => Promise<void>
  /** Shows the shell's menus and tray in a language, as the page does. */
  SetLanguage?: (tag: string) => Promise<void>
  GetDaemonURL?: () => Promise<string>
  /** Present when the shell passes the daemon's events on as Wails events. */
  HasEventRelay?: () => Promise<boolean>
  /** Asks where to save, then streams a daemon file there. Resolves to the saved path, or '' if cancelled. */
  SaveDownload?: (name: string, apiPath: string) => Promise<string>
  /** Asks where to save, then writes base64 bytes there. Resolves to the saved path, or '' if cancelled. */
  SaveData?: (name: string, base64: string) => Promise<string>
}

type WailsRuntime = {
  EventsOn?: (eventName: string, callback: (...data: unknown[]) => void) => () => void
  BrowserOpenURL?: (url: string) => void
}

function wailsGoApp(): WailsApp | undefined {
  return (window as unknown as { go?: { main?: { App?: WailsApp } } }).go?.main?.App
}

function wailsRuntime(): WailsRuntime | undefined {
  return (window as unknown as { runtime?: WailsRuntime }).runtime
}

export async function notifyDesktopBackgroundMode(enabled: boolean): Promise<void> {
  try {
    await wailsGoApp()?.SetBackgroundMode?.(enabled)
  } catch {
    // Running in a normal browser against the daemon — ignore.
  }
}

/** Enable or disable OS launch-at-login when running in the desktop shell. */
export async function notifyDesktopLaunchAtLogin(enabled: boolean): Promise<void> {
  try {
    await wailsGoApp()?.SetLaunchAtLogin?.(enabled)
  } catch {
    // Browser / headless — setting is still stored by the daemon.
  }
}

/** Tells the desktop shell the UI language, so its menus and tray match the page. */
export async function notifyDesktopLanguage(tag: string): Promise<void> {
  try {
    await wailsGoApp()?.SetLanguage?.(tag)
  } catch {
    // An older shell keeps its own language.
  }
}

export function isDesktopShell(): boolean {
  return Boolean(wailsGoApp())
}

export async function markScreenshotReady(): Promise<void> {
  try {
    await wailsGoApp()?.MarkScreenshotReady?.()
  } catch {
    // The capture script also watches data-screenshot-ready on the page.
  }
}

/** Reveal a path in Finder / Explorer / file manager when running in desktop. */
export async function openPathInOS(path: string): Promise<boolean> {
  if (!path) return false
  try {
    const app = wailsGoApp()
    if (app?.OpenPath) {
      await app.OpenPath(path)
      return true
    }
  } catch {
    // fall through
  }
  try {
    const runtime = wailsRuntime()
    if (runtime?.BrowserOpenURL) {
      const href = path.startsWith('file:') ? path : `file://${path}`
      runtime.BrowserOpenURL(href)
      return true
    }
  } catch {
    // ignore
  }
  return false
}

// The desktop app's web view (WebKit in Wails) cannot open windows or
// download files: window.open, target="_blank" links, and <a download> do
// nothing there. These helpers hand those jobs to the shell, and do the
// usual browser thing anywhere else.

const externalProtocols = new Set(['http:', 'https:', 'mailto:'])

/** Opens a page in the person's own browser. */
export function openExternal(url: string): void {
  const runtime = wailsRuntime()
  if (isDesktopShell() && runtime?.BrowserOpenURL) {
    runtime.BrowserOpenURL(url)
    return
  }
  window.open(url, '_blank', 'noopener,noreferrer')
}

/**
 * In the desktop app, sends links that leave the app (answer sources, links
 * in answers, a service's sign-in) to the default browser. Elsewhere it does
 * nothing. Returns a function that removes the handler.
 */
export function routeExternalLinks(doc: Document = document): () => void {
  if (!isDesktopShell()) return () => {}
  const onClick = (event: MouseEvent) => {
    if (event.defaultPrevented || event.button !== 0) return
    const anchor = (event.target as Element | null)?.closest?.('a[href]') as HTMLAnchorElement | null
    if (!anchor || anchor.hasAttribute('download')) return
    let url: URL
    try {
      url = new URL(anchor.href, doc.baseURI)
    } catch {
      return
    }
    // Only pages outside the app: one of its own pages opened in a browser
    // (wails://, or wails.localhost on Windows) could not load.
    if (!externalProtocols.has(url.protocol) || url.origin === doc.defaultView?.location.origin) return
    event.preventDefault()
    openExternal(url.href)
  }
  doc.addEventListener('click', onClick, true)
  return () => doc.removeEventListener('click', onClick, true)
}

/**
 * Opens a window for a service's sign-in during the click, so a browser
 * does not block it; the sign-in address is filled in later. The desktop
 * app opens sign-in in the person's browser instead, so there is none.
 */
export function preopenSignInWindow(): Window | null {
  if (isDesktopShell()) return null
  return window.open('about:blank', 'toskar-sign-in', 'width=520,height=720')
}

/**
 * The address a service sends the browser back to after sign-in. A page in
 * the desktop app has a wails:// origin that no browser can reach, so it is
 * the daemon's own address there.
 */
export async function signInReturnAddress(): Promise<string> {
  try {
    const url = await wailsGoApp()?.GetDaemonURL?.()
    if (url) return url
  } catch {
    // fall through
  }
  return window.location.origin
}

/**
 * Saves a file the daemon serves (apiPath is its own path, such as
 * /api/v1/artifacts/{id}/content) through the desktop shell. Resolves to
 * the saved path, '' if the person cancelled, or null outside the desktop
 * app, where the caller downloads as usual.
 */
export async function saveFromDaemon(name: string, apiPath: string): Promise<string | null> {
  const save = wailsGoApp()?.SaveDownload
  if (!save) return null
  return save(name, apiPath)
}

/** Saves text the page has, as UTF-8, through the desktop shell. Resolves like saveFromDaemon. */
export async function saveText(name: string, text: string): Promise<string | null> {
  const save = wailsGoApp()?.SaveData
  if (!save) return null
  const bytes = new TextEncoder().encode(text)
  let binary = ''
  for (const byte of bytes) binary += String.fromCharCode(byte)
  return save(name, btoa(binary))
}

/**
 * True when the desktop shell passes the daemon's events to the page. The
 * page can't read the event stream through the app on every platform (on
 * Windows, Wails hands over a proxied response only once it ends, and an
 * event stream never ends), so the shell reads it and relays each event.
 */
export function hasEventRelay(): boolean {
  return Boolean(wailsGoApp()?.HasEventRelay && wailsRuntime()?.EventsOn)
}

/** Hears each event's JSON from the desktop shell's relay. Returns an unsubscribe function. */
export function onRelayedEvent(handler: (json: string) => void): () => void {
  const runtime = wailsRuntime()
  if (!runtime?.EventsOn) return () => {}
  return runtime.EventsOn('ygg:event', (...data: unknown[]) => {
    if (typeof data[0] === 'string') handler(data[0])
  })
}

/** Ask the desktop shell to quit and stop the daemon (full restart by relaunching). */
export async function quitDesktopForRestart(): Promise<boolean> {
  try {
    const app = wailsGoApp()
    if (app?.QuitAndStopDaemon) {
      await app.QuitAndStopDaemon()
      return true
    }
  } catch {
    // ignore
  }
  return false
}

export type DesktopLifecycleEvent = {
  state: 'stopping' | 'background' | string
  message?: string
}

/** Subscribe to desktop shell lifecycle events. Returns an unsubscribe fn. */
export function onDesktopLifecycle(
  handler: (event: DesktopLifecycleEvent) => void,
): () => void {
  const runtime = wailsRuntime()
  if (!runtime?.EventsOn) {
    return () => {}
  }
  return runtime.EventsOn('ygg:lifecycle', (...data: unknown[]) => {
    const raw = data[0]
    if (raw && typeof raw === 'object') {
      const obj = raw as Record<string, unknown>
      handler({
        state: typeof obj.state === 'string' ? obj.state : 'stopping',
        message: typeof obj.message === 'string' ? obj.message : undefined,
      })
      return
    }
    handler({ state: 'stopping' })
  })
}
