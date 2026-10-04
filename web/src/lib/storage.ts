/**
 * Moves a value kept in this browser from its name before the Toskar rename
 * to its new one (#237), so nobody is signed out or loses settings. The old
 * name is removed: left behind, it would come back when the new value is
 * cleared, such as a forgotten API key. Runs before the value is first read.
 */
export function moveStored(oldName: string, newName: string) {
  try {
    const old = localStorage.getItem(oldName)
    if (old === null) return
    if (localStorage.getItem(newName) === null) localStorage.setItem(newName, old)
    localStorage.removeItem(oldName)
  } catch {
    // Storage can be off, as in a private window; there is nothing to move.
  }
}
