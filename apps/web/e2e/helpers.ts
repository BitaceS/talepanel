import { expect, test, type APIRequestContext, type Page } from '@playwright/test'

export const ADMIN_EMAIL = 'admin@demo.local'
export const ADMIN_PASSWORD = 'Demo-Password-2026!'

// Demo servers created by examples/demo/seed.sh (and run-demo.sh).
export const RUNNING_SERVER = 'Emberwood Survival'
export const STOPPED_SERVER = 'Skyforge Creative'

// POST /auth/login, waiting out the API's sign-in rate limit (30 per minute
// per IP). Locally the demo API sees 127.0.0.1, which development mode
// exempts; in CI it sees the Docker network and the limit applies.
async function postLogin(request: APIRequestContext) {
  for (let attempt = 0; ; attempt++) {
    const res = await request.post('/api/v1/auth/login', {
      data: { email: ADMIN_EMAIL, password: ADMIN_PASSWORD },
    })
    if (res.status() !== 429 || attempt >= 2) return res
    const wait = Number(res.headers()['retry-after'] ?? '60')
    await new Promise(r => setTimeout(r, (Math.min(Math.max(wait, 1), 60) + 1) * 1000))
  }
}

// Signs in through the API. The refresh cookie lands in the browser
// context, and the panel restores the session from it on the next load.
export async function login(page: Page): Promise<void> {
  test.setTimeout(180_000) // room for a rate-limit wait
  const res = await postLogin(page.request)
  expect(res.ok(), `login failed: ${res.status()}`).toBeTruthy()
}

// One access token per worker; it outlives the whole suite.
let cachedToken: string | undefined

export async function accessToken(request: APIRequestContext): Promise<string> {
  if (cachedToken) return cachedToken
  const res = await postLogin(request)
  expect(res.ok(), `login failed: ${res.status()}`).toBeTruthy()
  cachedToken = (await res.json()).access_token as string
  return cachedToken
}

export interface DemoServer { id: string, name: string, status: string }

export async function listServers(request: APIRequestContext, token: string): Promise<DemoServer[]> {
  const res = await request.get('/api/v1/servers', { headers: { Authorization: `Bearer ${token}` } })
  expect(res.ok()).toBeTruthy()
  return (await res.json()).servers as DemoServer[]
}

export async function serverByName(request: APIRequestContext, token: string, name: string): Promise<DemoServer> {
  const server = (await listServers(request, token)).find(s => s.name === name)
  expect(server, `demo server "${name}" not found; is the demo seeded?`).toBeTruthy()
  return server!
}

export async function openServer(page: Page, name: string): Promise<void> {
  await page.goto('/servers')
  await page.getByText(name, { exact: true }).filter({ visible: true }).first().click()
  await expect(page.getByRole('heading', { level: 2, name })).toBeVisible()
}

export async function openTab(page: Page, label: string): Promise<void> {
  await page.getByRole('main').getByRole('navigation').getByRole('button', { name: label, exact: true }).click()
}
