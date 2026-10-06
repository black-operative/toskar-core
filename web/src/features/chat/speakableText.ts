/** The most the computer reads aloud at once (the daemon's limit). */
const MAX_SPEECH = 5000

/** An answer as it should be spoken: without code, links' addresses, or formatting marks. */
export function speakableText(markdown: string): string {
  let text = markdown
    .replace(/```[\s\S]*?```/g, ' ')
    .replace(/!\[[^\]]*\]\([^)]*\)/g, ' ')
    .replace(/\[([^\]]+)\]\([^)]*\)/g, '$1')
    .replace(/`([^`]+)`/g, '$1')
    .replace(/^\s{0,3}(#{1,6}|>|[-*+]|\d+[.)])\s+/gm, '')
    .replace(/^\s*\|?[\s:|-]+\|[\s:|-]*$/gm, '')
    .replace(/\|/g, ' ')
    .replace(/(\*\*|__|\*|_|~~)(\S(?:.*?\S)?)\1/g, '$2')
    .split('\n')
    .map((line) => line.replace(/\s+/g, ' ').trim())
    .filter(Boolean)
    .join('\n')
  if (text.length > MAX_SPEECH) {
    const cut = text.slice(0, MAX_SPEECH)
    const end = Math.max(cut.lastIndexOf('. '), cut.lastIndexOf('\n'))
    text = end > MAX_SPEECH / 2 ? cut.slice(0, end + 1) : cut
  }
  return text
}

/** Whether this browser or app can speak with the device's own voices. */
export function hasSystemSpeech(): boolean {
  return typeof window !== 'undefined' && 'speechSynthesis' in window && 'SpeechSynthesisUtterance' in window
}
