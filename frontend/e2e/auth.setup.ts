import { test as setup, expect } from '@playwright/test'

const AUTH_FILE = 'e2e/.auth/user.json'

setup('authenticate as admin', async ({ page }) => {
  await page.goto('/sign-in')
  await page.getByLabel('Email Address').fill('admin@babelsuite.test')
  await page.locator('input[autocomplete="current-password"]').fill('admin')
  await page.getByRole('button', { name: 'Sign In' }).click()

  // Wait until we land somewhere past the sign-in page
  await expect(page).not.toHaveURL(/sign-in/, { timeout: 10_000 })

  await page.context().storageState({ path: AUTH_FILE })
})
