// Renders the Linux hicolor icons from the vector sources in docs/brand/logo.
// Sizes of 48 px and up use toskar-icon.svg. Smaller sizes use the small
// mark on the same tile with the same padding, so they stay legible.
// Uses the Playwright install in scripts/screenshots; run `make icons`.
import { createRequire } from 'node:module'
import { mkdir, readFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..')
const require = createRequire(path.join(root, 'scripts/screenshots/package.json'))
const { chromium } = require('playwright')

const SIZES = [16, 22, 24, 32, 48, 64, 128, 256, 512]
const SMALL_BELOW = 48
const TILE = '#0B0F14'
// The full icon places the mark at translate(11 10) scale(0.78) on the tile.
const PADDING = 'translate(11.00 10.00) scale(0.78)'

const logo = (name) => readFile(path.join(root, 'docs/brand/logo', name), 'utf8')

/** The small mark on the icon tile. */
function smallIcon(smallMark) {
  const inner = smallMark.replace(/^<svg[^>]*>/, '').replace(/<\/svg>\s*$/, '')
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><rect width="100" height="100" fill="${TILE}"/><g transform="${PADDING}">${inner}</g></svg>`
}

const full = await logo('toskar-icon.svg')
const small = smallIcon(await logo('toskar-mark-small.svg'))

const browser = await chromium.launch()
try {
  const page = await browser.newPage({ deviceScaleFactor: 1 })
  for (const size of SIZES) {
    const svg = size < SMALL_BELOW ? small : full
    await page.setViewportSize({ width: size, height: size })
    await page.setContent(
      `<!doctype html><html><body style="margin:0;background:${TILE}">` +
        svg.replace('<svg ', `<svg width="${size}" height="${size}" style="display:block" `) +
        '</body></html>',
    )
    const dir = path.join(root, 'assets/brand/generated/linux', `${size}x${size}`, 'apps')
    await mkdir(dir, { recursive: true })
    const out = path.join(dir, 'toskar.png')
    await page.screenshot({ path: out, clip: { x: 0, y: 0, width: size, height: size } })
    console.log(path.relative(root, out))
  }
} finally {
  await browser.close()
}
