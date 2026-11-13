// Regenerates the README screenshots from a running demo
// (examples/demo/run-demo.sh). Needs Playwright with Chromium:
//   npx playwright install chromium   (once)
//   node examples/demo/screenshots.mjs [out-dir]   (default docs/images)
import { mkdirSync } from 'node:fs'
import playwright from 'playwright' // CommonJS package: default import

const { chromium } = playwright

const BASE = process.env.DEMO_URL ?? 'http://localhost:14000'
const OUT = process.argv[2] ?? 'docs/images'
const EMAIL = 'admin@demo.local'
const PASSWORD = 'Demo-Password-2026!'
mkdirSync(OUT, { recursive: true })

const wait = (ms) => new Promise((r) => setTimeout(r, ms))

async function serverId(name) {
  const login = await fetch(`${BASE}/api/v1/auth/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email: EMAIL, password: PASSWORD }),
  }).then((r) => r.json())
  const list = await fetch(`${BASE}/api/v1/servers`, {
    headers: { Authorization: `Bearer ${login.access_token}` },
  }).then((r) => r.json())
  return list.servers.find((s) => s.name === name)?.id
}

const browser = await chromium.launch()
const sid = await serverId('Emberwood Survival')

for (const theme of ['classic', 'game']) {
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 2, colorScheme: 'dark' })
  await ctx.addInitScript((t) => { try { localStorage.setItem('tp_theme', t) } catch { /* ignore */ } }, theme)
  const page = await ctx.newPage()
  await page.goto(`${BASE}/auth/login`)
  await wait(1500)
  if (theme === 'classic') await page.screenshot({ path: `${OUT}/login.png` })
  await page.fill('input[type=email]', EMAIL)
  await page.fill('input[type=password]', PASSWORD)
  await page.click('button[type=submit]')
  await page.waitForURL((u) => !u.pathname.includes('/auth'), { timeout: 15000 })
  await wait(3500)
  await page.screenshot({ path: `${OUT}/${theme}-dashboard.png` })
  await page.goto(`${BASE}/servers`)
  await wait(3000)
  await page.screenshot({ path: `${OUT}/${theme}-servers.png` })

  if (theme === 'classic' && sid) {
    await page.goto(`${BASE}/servers/${sid}`)
    await wait(3000)
    for (const tab of ['Console', 'Players', 'Schedules']) {
      await page.getByRole('button', { name: tab, exact: true }).first().click()
      await wait(4000)
      await page.screenshot({ path: `${OUT}/server-${tab.toLowerCase()}.png` })
    }
    await page.goto(`${BASE}/servers`)
    await wait(2000)
    await page.getByRole('button', { name: /new server/i }).first().click()
    await wait(2000)
    await page.screenshot({ path: `${OUT}/create-from-template.png` })
  }
  await ctx.close()
}
await browser.close()
console.log(`screenshots written to ${OUT}`)
