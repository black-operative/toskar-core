import { useEffect, useRef } from 'react'

const focusable =
  'a[href], button:not([disabled]), input:not([disabled]):not([type="hidden"]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'

function focusableIn(root: HTMLElement): HTMLElement[] {
  return [...root.querySelectorAll<HTMLElement>(focusable)].filter(
    (el) => !el.closest('[hidden], [inert]') && getComputedStyle(el).visibility !== 'hidden',
  )
}

/**
 * Keyboard behavior for a modal dialog or drawer (WAI-ARIA dialog pattern):
 * while open, focus moves inside (to the element marked data-autofocus, or
 * the first control), Tab and Shift+Tab stay inside, and Escape calls
 * onClose; when it closes, focus returns to what had it before. Put the
 * returned ref on the dialog's container.
 *
 * Mark the safe choice data-autofocus (Cancel, Deny), so a stray Enter
 * never confirms something, or pass initialFocus, a selector inside the
 * dialog.
 */
export function useDialog<T extends HTMLElement = HTMLDivElement>(
  open: boolean,
  onClose?: () => void,
  { initialFocus }: { initialFocus?: string } = {},
) {
  const ref = useRef<T | null>(null)
  const onCloseRef = useRef(onClose)
  onCloseRef.current = onClose

  useEffect(() => {
    if (!open) return
    const root = ref.current
    if (!root) return
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null
    if (!root.contains(document.activeElement)) {
      const first =
        (initialFocus ? root.querySelector<HTMLElement>(initialFocus) : null) ??
        root.querySelector<HTMLElement>('[data-autofocus]') ??
        focusableIn(root)[0] ??
        root
      if (first === root && !root.hasAttribute('tabindex')) root.setAttribute('tabindex', '-1')
      first.focus()
    }

    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && onCloseRef.current) {
        event.stopPropagation()
        onCloseRef.current()
        return
      }
      if (event.key !== 'Tab') return
      const items = focusableIn(root)
      if (items.length === 0) {
        event.preventDefault()
        return
      }
      const first = items[0]
      const last = items[items.length - 1]
      const active = document.activeElement
      if (event.shiftKey && (active === first || !root.contains(active))) {
        event.preventDefault()
        last.focus()
      } else if (!event.shiftKey && (active === last || !root.contains(active))) {
        event.preventDefault()
        first.focus()
      }
    }
    document.addEventListener('keydown', onKey, true)
    return () => {
      document.removeEventListener('keydown', onKey, true)
      if (opener?.isConnected) opener.focus()
    }
  }, [open, initialFocus])

  return ref
}
