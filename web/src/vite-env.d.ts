/// <reference types="vite/client" />

// What the desktop shell sets on the page: the __TOSKAR_ names, or the
// __YGGDRASIL_ ones from a shell from before the rename (#237).
interface YggdrasilWindow extends Window {
  __TOSKAR_API_BASE__?: string
  __TOSKAR_SCREENSHOT__?: { enabled?: boolean; screen?: string }
  __YGGDRASIL_API_BASE__?: string
  __YGGDRASIL_SCREENSHOT__?: { enabled?: boolean; screen?: string }
}
