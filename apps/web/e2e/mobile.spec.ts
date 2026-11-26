import { expect, test, type Page } from '@playwright/test'
import { login, RUNNING_SERVER } from './helpers'

// 390 px: a common phone width. Pages must not scroll sideways.
async function expectNoHorizontalScroll(page: Page) {
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)
  expect(overflow, 'page is wider than the viewport').toBeLessThanOrEqual(0)
}

test.beforeEach(async ({ page }) => {
  await login(page)
})

for (const path of ['/', '/servers', '/nodes', '/settings']) {
  test(`no sideways scroll on ${path}`, async ({ page }) => {
    await page.goto(path)
    await expect(page.getByRole('main')).toBeVisible()
    await page.waitForLoadState('networkidle')
    await expectNoHorizontalScroll(page)
  })
}

test('server page works on a phone', async ({ page }) => {
  await page.goto('/servers')
  await page.getByText(RUNNING_SERVER, { exact: true }).filter({ visible: true }).first().click()
  await expect(page.getByRole('heading', { level: 2, name: RUNNING_SERVER })).toBeVisible()
  await page.waitForLoadState('networkidle')
  await expectNoHorizontalScroll(page)
})

test('the sidebar opens from the menu button', async ({ page }) => {
  await page.goto('/')
  const nav = page.getByRole('complementary', { name: 'Main navigation' })
  await expect(nav.getByRole('link', { name: /Servers/ })).not.toBeInViewport()
  await page.getByRole('button', { name: 'Open menu' }).click()
  await expect(nav.getByRole('link', { name: /Servers/ })).toBeInViewport()
  await nav.getByRole('link', { name: /Servers/ }).click()
  await expect(page).toHaveURL(/\/servers$/)
})
