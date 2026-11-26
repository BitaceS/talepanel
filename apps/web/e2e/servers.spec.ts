import { expect, test } from '@playwright/test'
import { login, openServer, openTab, RUNNING_SERVER, STOPPED_SERVER } from './helpers'

test.beforeEach(async ({ page }) => {
  await login(page)
})

test('server list shows the demo servers', async ({ page }) => {
  await page.goto('/servers')
  for (const name of [RUNNING_SERVER, STOPPED_SERVER, 'Lobby Hub']) {
    await expect(page.getByText(name, { exact: true }).first()).toBeVisible()
  }
  await page.getByPlaceholder('Search servers...').fill('Sky')
  await expect(page.getByText(STOPPED_SERVER, { exact: true }).first()).toBeVisible()
  await expect(page.getByText(RUNNING_SERVER, { exact: true })).toHaveCount(0)
})

test('every server tab opens without an error', async ({ page }) => {
  const pageErrors: string[] = []
  page.on('pageerror', e => pageErrors.push(e.message))
  await openServer(page, RUNNING_SERVER)
  const tabs = ['Overview', 'Console', 'Game Control', 'Files', 'Config', 'Mods', 'Worlds', 'Players', 'Databases', 'Backups', 'Schedules', 'Settings']
  for (const tab of tabs) {
    await openTab(page, tab)
    await expect(page.getByRole('main').getByRole('navigation').getByRole('button', { name: tab, exact: true })).toBeVisible()
  }
  expect(pageErrors).toEqual([])
})

test('console shows output of the running server', async ({ page }) => {
  await openServer(page, RUNNING_SERVER)
  await openTab(page, 'Console')
  await expect(page.locator('main')).toContainText(/\w+/)
})
