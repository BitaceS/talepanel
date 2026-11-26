import { expect, test } from '@playwright/test'
import { accessToken, login, openServer, openTab, RUNNING_SERVER, serverByName } from './helpers'

const FILE = 'e2e-notes.txt'

test('files tab lists the server root and edits a file', async ({ page, request }) => {
  const token = await accessToken(request)
  const server = await serverByName(request, token, RUNNING_SERVER)
  const auth = { Authorization: `Bearer ${token}` }
  const put = await request.put(`/api/v1/servers/${server.id}/files/content`, {
    headers: auth,
    data: { path: `/${FILE}`, content: 'written by the e2e suite\n' },
  })
  expect(put.ok()).toBeTruthy()

  try {
    await login(page)
    await openServer(page, RUNNING_SERVER)
    await openTab(page, 'Files')
    const main = page.getByRole('main')
    // The demo's install puts the server under Server/ next to Assets.zip.
    await expect(main.getByText('Assets.zip', { exact: true })).toBeVisible()
    await expect(main.getByText(FILE, { exact: true })).toBeVisible()
    await main.getByText(FILE, { exact: true }).click()
    const editor = main.locator('textarea')
    await expect(editor).toHaveValue('written by the e2e suite\n')
    await editor.fill('edited in the browser\n')
    await main.getByRole('button', { name: 'Save', exact: true }).click()
    await expect.poll(async () => {
      const res = await request.get(`/api/v1/servers/${server.id}/files/content`, { headers: auth, params: { path: `/${FILE}` } })
      return (await res.json()).content
    }).toBe('edited in the browser\n')
  } finally {
    await request.delete(`/api/v1/servers/${server.id}/files`, { headers: auth, params: { path: `/${FILE}` } })
  }
})
