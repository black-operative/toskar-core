// Numbers in the form's price field, as people write them.

/**
 * A number as people write it: 1,299.99 and 1.299,99 are the same, and a
 * single separator before exactly three digits groups thousands (2.500 is
 * 2500), while one before one or two digits is the decimal point (4.99).
 */
export function readNumber(text: string): number | null {
  if (!/^\d[\d.,\u00a0\u202f' ]*$/.test(text)) return null
  const separators = [...text.matchAll(/[^\d]/g)]
  if (!separators.length) return Number(text)
  const last = separators[separators.length - 1]
  const lastIndex = last.index ?? 0
  const digitsAfter = text.length - lastIndex - 1
  const kinds = new Set(separators.map((s) => s[0]))
  const decimal = /[.,]/.test(last[0]) && (digitsAfter !== 3 || (kinds.size > 1 && separators.length > 1 && last[0] !== separators[0][0]))
  const whole = (decimal ? text.slice(0, lastIndex) : text).replace(/[^\d]/g, '')
  const fraction = decimal ? text.slice(lastIndex + 1) : ''
  const value = Number(fraction ? `${whole}.${fraction}` : whole)
  return Number.isFinite(value) ? value : null
}
