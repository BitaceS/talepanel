import { expect, test, type APIRequestContext } from '@playwright/test'
import { accessToken, login, openServer, openTab, RUNNING_SERVER, serverByName } from './helpers'

// The demo's simulated server writes no Hytale config files, so the test
// brings its own JSON file. The Config tab lists every *.json in the server
// root, so this exercises the same form and save path.
const FILE = 'e2e-settings.json'
const INITIAL = '{\n  "WelcomeText": "hello",\n  "Slots": 10,\n  "Enabled": true\n}\n'

let token = ''
let serverId = ''

async function readFile(request: APIRequestContext): Promise<string> {
  const res = await request.get(`/api/v1/servers/${serverId}/files/content`, {
    headers: { Authorization: `Bearer ${token}` },
    params: { path: `/${FILE}` },
  })
  expect(res.ok()).toBeTruthy()
  return (await res.json()).content as string
}

test.beforeEach(async ({ page, request }) => {
  token = await accessToken(request)
  serverId = (await serverByName(request, token, RUNNING_SERVER)).id
  const res = await request.put(`/api/v1/servers/${serverId}/files/content`, {
    headers: { Authorization: `Bearer ${token}` },
    data: { path: `/${FILE}`, content: INITIAL },
  })
  expect(res.ok()).toBeTruthy()
  await login(page)
  await openServer(page, RUNNING_SERVER)
  await openTab(page, 'Config')
  await page.getByRole('navigation', { name: 'Config files' }).getByRole('button', { name: FILE }).click()
})

test.afterAll(async ({ request }) => {
  if (!serverId) return
  await request.delete(`/api/v1/servers/${serverId}/files`, {
    headers: { Authorization: `Bearer ${token}` },
    params: { path: `/${FILE}` },
  })
})

test('edit a value in the form and save it', async ({ page, request }) => {
  const text = page.getByLabel('Welcome text')
  await expect(text).toHaveValue('hello')
  await text.fill('hello from e2e')
  await expect(page.getByText('Unsaved')).toBeVisible()
  await page.getByRole('button', { name: 'Save' }).click()
  await expect(page.getByText(`${FILE} saved`).first()).toBeVisible()
  await expect(page.getByText('Unsaved')).toHaveCount(0)

  const saved = await readFile(request)
  expect(JSON.parse(saved)).toEqual({ WelcomeText: 'hello from e2e', Slots: 10, Enabled: true })
  // Indent and trailing newline are kept.
  expect(saved).toBe(INITIAL.replace('"hello"', '"hello from e2e"'))
})

test('invalid JSON in the raw editor is not saved', async ({ page, request }) => {
  await page.getByRole('button', { name: 'Raw JSON' }).click()
  const raw = page.getByLabel(`Raw JSON of ${FILE}`)
  await raw.fill('{ "WelcomeText": ')
  await page.getByRole('button', { name: 'Save' }).click()
  await expect(page.getByRole('alert')).toContainText('the JSON is invalid')
  // Switching back to the form is refused too.
  await page.getByRole('button', { name: 'Form' }).click()
  await expect(page.getByRole('alert')).toContainText('Fix the JSON first')

  expect(await readFile(request)).toBe(INITIAL)
})
