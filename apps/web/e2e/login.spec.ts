import { expect, test } from '@playwright/test'
import { ADMIN_EMAIL, ADMIN_PASSWORD } from './helpers'

test('wrong password is refused', async ({ page }) => {
  await page.goto('/auth/login')
  await page.getByLabel('Email Address').fill(ADMIN_EMAIL)
  await page.getByLabel('Password', { exact: true }).fill('not-the-password')
  await page.getByRole('button', { name: /sign in/i }).click()
  await expect(page).toHaveURL(/\/auth\/login/)
  await expect(page.getByText(/invalid|incorrect|wrong/i).first()).toBeVisible()
})

test('sign in lands on the dashboard and sign out ends the session', async ({ page }) => {
  await page.goto('/')
  await expect(page).toHaveURL(/\/auth\/login/)
  await page.getByLabel('Email Address').fill(ADMIN_EMAIL)
  await page.getByLabel('Password', { exact: true }).fill(ADMIN_PASSWORD)
  await page.getByRole('button', { name: /sign in/i }).click()
  await expect(page).toHaveURL(/\/$/)
  await expect(page.getByRole('heading', { name: 'Dashboard', level: 1 })).toBeVisible()

  await page.getByRole('button', { name: /logout/i }).click()
  await expect(page).toHaveURL(/\/auth\/login/)
  await page.goto('/servers')
  await expect(page).toHaveURL(/\/auth\/login/)
})
