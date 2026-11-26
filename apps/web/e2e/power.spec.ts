import { expect, test } from '@playwright/test'
import { accessToken, login, openServer, serverByName, STOPPED_SERVER } from './helpers'

// Start and stop the simulated server. It ends where it began (stopped), so
// a rerun against the same stack works.
test('start and stop a server', async ({ page, request }) => {
  test.setTimeout(120_000)
  const token = await accessToken(request)
  const before = await serverByName(request, token, STOPPED_SERVER)
  test.skip(before.status !== 'stopped', `${STOPPED_SERVER} is ${before.status}, expected stopped`)

  await login(page)
  await openServer(page, STOPPED_SERVER)
  const main = page.getByRole('main')
  const start = main.getByRole('button', { name: 'Start', exact: true })
  const stop = main.getByRole('button', { name: 'Stop', exact: true })

  await expect(start).toBeEnabled()
  await start.click()
  await expect(main.getByText('Online', { exact: true }).first()).toBeVisible({ timeout: 60_000 })
  await expect(stop).toBeEnabled()

  await stop.click()
  // Some panels ask before stopping; confirm if a dialog shows up.
  const confirm = page.getByRole('dialog').getByRole('button', { name: /stop/i })
  if (await confirm.isVisible().catch(() => false)) await confirm.click()
  await expect(main.getByText('Offline', { exact: true }).first()).toBeVisible({ timeout: 60_000 })
  await expect(start).toBeEnabled()
})
