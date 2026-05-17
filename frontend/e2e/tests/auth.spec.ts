import { test, expect } from '@playwright/test'
import { SignInPage } from '../pages/SignInPage'

test.use({ storageState: { cookies: [], origins: [] } })

test.describe('Authentication', () => {
  test('sign in with valid credentials lands on dashboard', async ({ page }) => {
    const signIn = new SignInPage(page)
    await signIn.goto()
    await signIn.signIn('admin@babelsuite.test', 'admin')
    await expect(page).not.toHaveURL(/sign-in/, { timeout: 10_000 })
  })

  test('wrong password shows error message', async ({ page }) => {
    const signIn = new SignInPage(page)
    await signIn.goto()
    await signIn.signIn('admin@babelsuite.test', 'wrongpassword')
    await expect(signIn.errorMessage()).toBeVisible()
  })

  test('unknown email shows error message', async ({ page }) => {
    const signIn = new SignInPage(page)
    await signIn.goto()
    await signIn.signIn('nobody@example.com', 'admin')
    await expect(signIn.errorMessage()).toBeVisible()
  })

  test('unauthenticated visit to /catalog redirects to sign-in', async ({ page }) => {
    await page.goto('/catalog')
    await expect(page).toHaveURL(/sign-in/, { timeout: 10_000 })
  })
})
