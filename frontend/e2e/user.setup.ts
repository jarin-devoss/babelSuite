import { writeFileSync } from 'node:fs'
import { randomBytes } from 'node:crypto'
import { test as setup, expect } from '@playwright/test'
import { SignInPage } from './pages/SignInPage'
import { USER_FILE, USER_STATE, USER_TOKEN_FILE, readSession } from './support/session'

// A non-admin account, created through the API so the rbac specs can sign in as it.
setup('create and sign in a non-admin user', async ({ page, request }) => {
  const session = readSession()
  const credentials = {
    fullName: 'E2E User',
    email: `user-${randomBytes(3).toString('hex')}@e2e.local`,
    // The server requires an upper-case letter, a lower-case letter and a digit.
    password: `${randomBytes(8).toString('hex')}Aa1`,
  }

  await request.get(`${session.apiUrl}/api/v1/auth/config`)
  const csrf = (await request.storageState()).cookies.find((c) => c.name === 'csrf_token')?.value ?? ''
  const created = await request.post(`${session.apiUrl}/api/v1/auth/sign-up`, {
    data: credentials,
    headers: { 'X-CSRF-Token': csrf },
  })
  expect(created.ok(), await created.text()).toBe(true)
  writeFileSync(USER_FILE, JSON.stringify(credentials))

  const signIn = new SignInPage(page)
  await signIn.goto()
  await signIn.signIn(credentials.email, credentials.password)
  await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible()
  await page.context().storageState({ path: USER_STATE })
  const token = await page.evaluate(() => (JSON.parse(window.localStorage.getItem('babelsuite.session') ?? '{}') as { token?: string }).token ?? '')
  expect(token, 'the session should hold a token').not.toBe('')
  writeFileSync(USER_TOKEN_FILE, token)
})
