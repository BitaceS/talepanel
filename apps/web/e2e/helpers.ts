import { expect, type APIRequestContext, type Page } from '@playwright/test'

export const ADMIN_EMAIL = 'admin@demo.local'
export const ADMIN_PASSWORD = 'Demo-Password-2026!'

// Demo servers created by examples/demo/seed.sh (and run-demo.sh).
export const RUNNING_SERVER = 'Emberwood Survival'
export const STOPPED_SERVER = 'Skyforge Creative'

// Signs in through the API. The refresh cookie lands in the browser
// context, and the panel restores the session from it on the next load.
export async function login(page: Page): Promise<void> {
  const res = await page.request.post('/api/v1/auth/login', {
    data: { email: ADMIN_EMAIL, password: ADMIN_PASSWORD },
  })
  expect(res.ok(), `login failed: ${res.status()}`).toBeTruthy()
}

export async function accessToken(request: APIRequestContext): Promise<string> {
  const res = await request.post('/api/v1/auth/login', {
    data: { email: ADMIN_EMAIL, password: ADMIN_PASSWORD },
  })
  expect(res.ok()).toBeTruthy()
  return (await res.json()).access_token as string
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
