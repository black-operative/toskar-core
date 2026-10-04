// Web UI leak check (#231): moves between every page many times in one
// browser session, against the demo server, and fails if the page's heap,
// DOM nodes, or event listeners keep growing after warm-up. A leak here is
// usually a subscription, interval, or listener an effect never removed.
//
//   node scripts/screenshots/leaks.mjs   (after building web/dist)
import { spawn } from 'node:child_process'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { chromium } from 'playwright'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..')
const pages = ['/chat', '/automations', '/models', '/train', '/knowledge', '/memory', '/nodes', '/performance', '/diagnostics', '/profiles', '/tools', '/api-access', '/settings']
const rounds = Number(process.env.LEAK_ROUNDS || 12)
const warmup = 3

const server = spawn('go', ['run', './cmd/screenshot', '-web', 'web/dist'], { cwd: root, detached: true, stdio: ['ignore', 'pipe', 'inherit'] })
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

const browser = await chromium.launch({ args: ['--js-flags=--expose-gc'] })
let failed = false
try {
  const context = await browser.newContext({ viewport: { width: 1280, height: 800 }, reducedMotion: 'reduce' })
  await context.addInitScript(() =>
    localStorage.setItem('yggdrasil-ui', JSON.stringify({ state: { onboardingComplete: true, advancedMode: true, theme: 'dark' }, version: 0 })),
  )
  const page = await context.newPage()
  // A page that throws while moving between pages is a bug, and the logged
  // error keeps that page's elements alive, which reads as a leak.
  const errors = []
  page.on('pageerror', (error) => errors.push(error.message.split('\n')[0]))
  // A crash the page's error boundary catches is logged as "page error".
  page.on('console', (message) => {
    if (message.type() === 'error' && message.text().startsWith('page error')) errors.push(message.text().split('\n')[0])
  })
  const cdp = await context.newCDPSession(page)
  await cdp.send('Performance.enable')
  const go = (to) =>
    page.evaluate((path) => {
      history.pushState({}, '', path)
      dispatchEvent(new PopStateEvent('popstate'))
    }, to)
  // Every sample is taken on Chat, so rounds compare like with like.
  const sample = async () => {
    await go('/chat')
    await page.waitForTimeout(800)
    // A full collection, DOM included; plain gc() leaves detached nodes for
    // Blink's own collector, which made single readings noisy.
    await cdp.send('HeapProfiler.collectGarbage')
    await cdp.send('HeapProfiler.collectGarbage')
    await page.waitForTimeout(200)
    const { metrics } = await cdp.send('Performance.getMetrics')
    const get = (name) => metrics.find((m) => m.name === name)?.value ?? 0
    return { heap: get('JSHeapUsedSize'), nodes: get('Nodes'), listeners: get('JSEventListeners') }
  }
  await cdp.send('HeapProfiler.enable')
  await page.goto(`${base}/chat`, { waitUntil: 'domcontentloaded' })
  await page.waitForTimeout(1500)
  // A sample after every round past warm-up. A leak raises the floor (what
  // can't be collected), while garbage not yet collected only adds noise, so
  // the check compares the lowest readings early and late in the run.
  const series = []
  for (let round = 0; round < rounds; round++) {
    for (const route of pages) {
      // In-app navigation, as a person clicking the sidebar does.
      await go(route)
      await page.waitForTimeout(250)
    }
    if (round >= warmup - 1) series.push(await sample())
  }
  const third = Math.max(2, Math.floor(series.length / 3))
  const floor = (part, key) => Math.min(...part.map((s) => s[key]))
  const early = series.slice(0, third)
  const late = series.slice(-third)
  const before = { heap: floor(early, 'heap'), nodes: floor(early, 'nodes'), listeners: floor(early, 'listeners') }
  const after = { heap: floor(late, 'heap'), nodes: floor(late, 'nodes'), listeners: floor(late, 'listeners') }
  const mb = (n) => (n / 1048576).toFixed(1)
  if (process.env.LEAK_TREND) console.log('per round:', series.map((s) => `${s.nodes}n/${s.listeners}l/${mb(s.heap)}MB`).join(' '))
  console.log(`lowest early: heap ${mb(before.heap)} MB, ${before.nodes} nodes, ${before.listeners} listeners`)
  console.log(`lowest late (${series.length} rounds sampled, ${pages.length} pages each): heap ${mb(after.heap)} MB, ${after.nodes} nodes, ${after.listeners} listeners`)
  // Slack for caches (React Query keeps each page's data) and lazy chunks;
  // a leak grows with every round and passes it.
  const problems = []
  if (errors.length) problems.push(`${errors.length} page error(s), such as: ${[...new Set(errors)].slice(0, 3).join('; ')}`)
  if (after.heap > before.heap * 1.5 + 8 * 1048576) problems.push(`heap grew from ${mb(before.heap)} MB to ${mb(after.heap)} MB`)
  if (after.nodes > before.nodes * 1.5 + 500) problems.push(`DOM nodes grew from ${before.nodes} to ${after.nodes}`)
  if (after.listeners > before.listeners * 1.5 + 100) problems.push(`event listeners grew from ${before.listeners} to ${after.listeners}`)
  if (problems.length) {
    failed = true
    console.error(`\nPossible leak:\n  ${problems.join('\n  ')}`)
  } else {
    console.log('\nNo growth beyond warm-up.')
  }
} finally {
  await browser.close()
  try {
    process.kill(-server.pid)
  } catch {
    // already gone
  }
}
process.exit(failed ? 1 : 0)
