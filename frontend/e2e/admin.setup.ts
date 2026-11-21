import { writeFileSync } from 'node:fs'
import { test as setup, expect } from '@playwright/test'
import { SignInPage } from './pages/SignInPage'
import { ADMIN_STATE, ADMIN_TOKEN_FILE, readSession } from './support/session'

setup('sign in as the admin', async ({ page }) => {
  const session = readSession()
  const signIn = new SignInPage(page)
  await signIn.goto()
  await signIn.signIn(session.email, session.password)

  // Wait for something only a signed-in page has, not merely for the URL to move.
  await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible()
  await page.context().storageState({ path: ADMIN_STATE })

  // Later specs and the teardown reuse this token instead of signing in again.
  const token = await page.evaluate(() => (JSON.parse(window.localStorage.getItem('babelsuite.session') ?? '{}') as { token?: string }).token ?? '')
  expect(token, 'the session should hold a token').not.toBe('')
  writeFileSync(ADMIN_TOKEN_FILE, token)
})
