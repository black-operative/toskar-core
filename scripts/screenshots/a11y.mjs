// Accessibility check (#217): axe-core over every page of the built web UI,
// served with demo data by cmd/screenshot, in both themes and at desktop
// and phone widths. Fails on any WCAG 2.2 A/AA or best-practice violation.
//
//   node scripts/screenshots/a11y.mjs   (after building web/dist)
//   A11Y_PAGES=/chat,/models node scripts/screenshots/a11y.mjs   (only some pages)
import { spawn } from 'node:child_process'
import { createRequire } from 'node:module'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { chromium } from 'playwright'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..')
const axePath = createRequire(import.meta.url).resolve('axe-core/axe.min.js')

const allPages = [
  '/chat',
  '/automations',
  '/models',
  '/train',
  '/knowledge',
  '/memory',
  '/nodes',
  '/performance',
  '/diagnostics',
  '/profiles',
  '/tools',
  '/api-access',
  '/settings',
]
const only = (process.env.A11Y_PAGES || '').split(',').map((p) => p.trim()).filter(Boolean)
const pages = only.length ? allPages.filter((p) => only.includes(p)) : allPages
// Onboarding is checked in a session that has not finished it yet.
const checkOnboarding = only.length === 0 || only.includes('/onboarding')
const themes = ['dark', 'light']
const widths = [
  { name: 'desktop', viewport: { width: 1440, height: 900 } },
  { name: 'phone', viewport: { width: 390, height: 844 } },
]
const tags = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa', 'best-practice']

const server = spawn('go', ['run', './cmd/screenshot', '-web', 'web/dist'], {
  cwd: root,
  detached: true,
  stdio: ['ignore', 'pipe', 'inherit'],
})
const base = await new Promise((resolve, reject) => {
  let log = ''
  const timer = setTimeout(() => reject(new Error('demo server did not start')), 120_000)
  server.stdout.on('data', (chunk) => {
    log += chunk.toString()
    const match = log.match(/listening (http:\/\/\S+)/)
    if (match) {
      clearTimeout(timer)
      resolve(match[1])
    }
  })
  server.on('exit', (code) => reject(new Error(`demo server exited ${code}`)))
})

const failures = []
const browser = await chromium.launch(process.env.SCREENSHOT_CHROME ? { executablePath: process.env.SCREENSHOT_CHROME } : {})
// Each theme and width runs in its own browser context, all at once: most of
// the time is each page settling, not the check itself.
const runs = []
for (const theme of themes) {
  for (const { name, viewport } of widths) {
    for (const onboarded of checkOnboarding ? [true, false] : [true]) {
      const routes = onboarded ? pages : ['/onboarding']
      if (routes.length > 0) runs.push({ theme, name, viewport, onboarded, routes })
    }
  }
}

async function check({ theme, name, viewport, onboarded, routes }) {
  const context = await browser.newContext({ viewport, reducedMotion: 'reduce' })
  // Advanced mode on so every page is in the sidebar, onboarding done
  // (or not, for its own check), and the theme under test.
  await context.addInitScript(
    ({ theme, onboarded }) => {
      localStorage.setItem(
        'toskar-ui',
        JSON.stringify({ state: { onboardingComplete: onboarded, advancedMode: true, theme }, version: 0 }),
      )
    },
    { theme, onboarded },
  )
  const page = await context.newPage()
  const errors = []
  page.on('pageerror', (error) => errors.push(error.message))
  for (const route of routes) {
    errors.length = 0
    await page.goto(`${base}${route}`, { waitUntil: 'domcontentloaded' })
    await page.waitForSelector('main h1', { timeout: 30_000 }).catch(() => {})
    // Let queries settle (the event stream stays open, so the network is
    // never idle), and measure colors at rest, not mid-transition.
    await page.waitForTimeout(1500)
    await page.addStyleTag({ content: '*,*::before,*::after{transition:none!important;animation:none!important}' })
    await page.addScriptTag({ path: axePath })
    const violations = await page.evaluate(
      async (tags) =>
        (await window.axe.run(document, { runOnly: { type: 'tag', values: tags } })).violations.map((v) => ({
          id: v.id,
          impact: v.impact,
          help: v.help,
          nodes: v.nodes.slice(0, 3).map((n) => `${n.target.join(' ')} — ${(n.failureSummary || '').split('\n')[1]?.trim() || ''}`),
          count: v.nodes.length,
        })),
      tags,
    )
    const where = `${theme} ${name} ${route}`
    if (errors.length) failures.push({ where, id: 'page-error', impact: 'critical', help: errors[0], nodes: [], count: errors.length })
    for (const v of violations) failures.push({ where, ...v })
    console.log(`${violations.length || errors.length ? '✗' : '✓'} ${where}`)
  }
  await context.close()
}

try {
  await Promise.all(runs.map(check))
} finally {
  await browser.close()
  try {
    process.kill(-server.pid)
  } catch {
    // already gone
  }
}

if (failures.length) {
  failures.sort((a, b) => a.where.localeCompare(b.where))
  console.error(`\n${failures.length} accessibility problem(s):`)
  for (const f of failures) {
    console.error(`\n[${f.impact}] ${f.id} (${f.count}) on ${f.where}: ${f.help}`)
    for (const n of f.nodes) console.error(`  ${n}`)
  }
  process.exit(1)
}
const checked = pages.length + (checkOnboarding ? 1 : 0)
console.log(`\nNo accessibility problems on ${checked} pages × ${themes.length} themes × ${widths.length} widths.`)
