import { expect, test } from '@playwright/test'
import { login } from './helpers'

test('switching to German translates the panel and survives a reload', async ({ page }) => {
  await login(page)
  await page.goto('/')
  const nav = page.getByRole('complementary', { name: 'Main navigation' })
  await expect(nav.getByRole('link', { name: /Settings/ })).toBeVisible()

  await page.getByRole('banner').getByRole('button', { name: /demo-admin/ }).click()
  await page.getByRole('radio', { name: 'Deutsch' }).click()

  const navDe = page.getByRole('complementary', { name: 'Hauptnavigation' })
  await expect(navDe.getByRole('link', { name: /Einstellungen/ })).toBeVisible()
  await expect(page.locator('html')).toHaveAttribute('lang', 'de')

  await page.reload()
  await expect(navDe.getByRole('link', { name: /Einstellungen/ })).toBeVisible()
  await page.goto('/servers')
  await expect(page.getByPlaceholder('Server suchen…')).toBeVisible()
})

test('a German browser gets the German panel on the first visit', async ({ browser }) => {
  const context = await browser.newContext({ locale: 'de-DE' })
  const page = await context.newPage()
  await page.goto('/auth/login')
  await expect(page.locator('html')).toHaveAttribute('lang', 'de')
  await context.close()
})
