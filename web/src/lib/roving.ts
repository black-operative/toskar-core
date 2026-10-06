import { useEffect, useRef, type KeyboardEvent } from 'react'

const items = '[role="tab"], [role="radio"], [role="menuitem"], [role="menuitemcheckbox"]'

/**
 * Arrow keys for a tab list, radio group, or menu (WAI-ARIA patterns): put it
 * on the container's onKeyDown. Left/Up and Right/Down move to the previous
 * or next item, wrapping, and Home and End to the first and last. Tabs and
 * radios are chosen as focus moves; menu items only take focus. Give the
 * chosen item tabIndex 0 and the others -1, so the group is one Tab stop.
 */
export function rovingKeyDown(event: KeyboardEvent<HTMLElement>) {
  const list = [...event.currentTarget.querySelectorAll<HTMLElement>(items)].filter(
    (el) => !(el as HTMLButtonElement).disabled && el.getAttribute('aria-disabled') !== 'true',
  )
  const at = list.indexOf(document.activeElement as HTMLElement)
  if (at < 0 || list.length === 0) return
  const rtl = getComputedStyle(event.currentTarget).direction === 'rtl'
  let next: number
  switch (event.key) {
    case 'ArrowRight':
      next = at + (rtl ? -1 : 1)
      break
    case 'ArrowLeft':
      next = at + (rtl ? 1 : -1)
      break
    case 'ArrowDown':
      next = at + 1
      break
    case 'ArrowUp':
      next = at - 1
      break
    case 'Home':
      next = 0
      break
    case 'End':
      next = list.length - 1
      break
    default:
      return
  }
  event.preventDefault()
  const target = list[(next + list.length) % list.length]
  target.focus()
  // Tabs and radios are chosen as focus reaches them; menu items, checkbox
  // ones included, wait for Enter or a click.
  const role = target.getAttribute('role')
  if (role === 'tab' || role === 'radio') target.click()
}

/**
 * Keyboard behavior for a pop-up menu: when it opens, focus moves to its
 * first item; Escape closes it and returns focus to the button that opened
 * it; Tab closes it and moves on. Put the returned ref on the element with
 * role="menu", and rovingKeyDown on it for the arrow keys. open is anything
 * that names the open menu, such as its row's id, or null when none is.
 */
export function useMenu<T extends HTMLElement = HTMLDivElement>(open: string | boolean | null, onClose: () => void) {
  const ref = useRef<T | null>(null)
  const onCloseRef = useRef(onClose)
  onCloseRef.current = onClose
  useEffect(() => {
    if (!open) return
    const menu = ref.current
    if (!menu) return
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null
    menu.querySelector<HTMLElement>('[role="menuitem"]')?.focus()
    const onKey = (event: globalThis.KeyboardEvent) => {
      if (!menu.contains(document.activeElement)) return
      if (event.key === 'Escape') {
        event.stopPropagation()
        onCloseRef.current()
        opener?.focus()
      } else if (event.key === 'Tab') {
        onCloseRef.current()
      }
    }
    document.addEventListener('keydown', onKey, true)
    return () => document.removeEventListener('keydown', onKey, true)
  }, [open])
  return ref
}
