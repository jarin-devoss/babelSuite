import type { Page } from '@playwright/test'
import { Api } from '../../api/client'
import { knownBug, test, expect } from '../../fixtures'
import { SignInPage } from '../../pages/SignInPage'
import { ADMIN_STATE, readSession } from '../../support/session'
import { createUser, newUser, type NewUser } from '../../support/users'

const WRONG = 'Incorrect email or password.'

// Sign-up allows five attempts a minute per address, so the tests that need an
// existing account share one throw-away user instead of creating their own.
let shared: NewUser | undefined
async function sharedUser(api: import('../../api/client').Api): Promise<NewUser> {
  if (!shared) {
    shared = newUser('shared')
    await createUser(api, shared)
  }
  return shared
}

async function storedSession(page: Page) {
  return page.evaluate(() => {
    const raw = window.localStorage.getItem('babelsuite.session')
    return raw ? (JSON.parse(raw) as { token: string; user: { email: string; isAdmin: boolean } }) : null
  })
}

test.describe('Sign in and out', () => {
  test('@smoke the admin signs in and the session belongs to the admin', async ({ page }) => {
    const admin = readSession()
    const signIn = new SignInPage(page)
    await signIn.goto()
    await signIn.signIn(admin.email, admin.password)

    await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible()
    await expect(page).toHaveURL('/')
    const stored = await storedSession(page)
    expect(stored?.user.email).toBe(admin.email)
    expect(stored?.user.isAdmin).toBe(true)
  })

  test('a wrong password shows the exact error and stores no session', async ({ page, api }) => {
    const user = await sharedUser(api)

    const signIn = new SignInPage(page)
    await signIn.goto()
    await signIn.signIn(user.email, `${user.password}x`)

    await expect(signIn.errorMessage()).toHaveText(WRONG)
    await expect(page).toHaveURL(/sign-in/)
    expect(await storedSession(page)).toBeNull()
  })

  test('an unknown email gets the same error as a wrong password', async ({ page }) => {
    const signIn = new SignInPage(page)
    await signIn.goto()
    await signIn.signIn(`nobody-${Date.now()}@e2e.local`, 'Whatever1')

    // Identical text for both cases, so the form does not reveal which emails exist.
    await expect(signIn.errorMessage()).toHaveText(WRONG)
    expect(await storedSession(page)).toBeNull()
  })

  test('@smoke a deep link sends a signed-out visitor to sign-in and back after login', async ({ page }) => {
    const admin = readSession()
    await page.goto('/executions/does-not-exist')
    await expect(page).toHaveURL(/\/sign-in\?returnTo=%2Fexecutions%2Fdoes-not-exist/)

    const signIn = new SignInPage(page)
    await signIn.signIn(admin.email, admin.password)
    await expect(page).toHaveURL('/executions/does-not-exist')
  })

  test('signing out clears the session and protects the app again', async ({ page }) => {
    const admin = readSession()
    const signIn = new SignInPage(page)
    await signIn.goto()
    await signIn.signIn(admin.email, admin.password)
    await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible()

    await page.getByRole('button', { name: 'Sign out' }).click()
    await expect(page).toHaveURL(/sign-in/)
    expect(await storedSession(page)).toBeNull()

    await page.goto('/')
    await expect(page).toHaveURL(/sign-in\?returnTo=%2F/)
  })

  test.describe('already signed in', () => {
    test.use({ storageState: ADMIN_STATE })

    test('a signed-in visitor of /sign-in is sent to the dashboard', async ({ page }) => {
      await page.goto('/sign-in')
      await expect(page).toHaveURL('/')
    })
  })
})

test.describe('Sign up', () => {
  test('a new user can sign up, lands signed in, and is not an admin', async ({ page }) => {
    const user = newUser('signup')
    await page.goto('/sign-up')
    await page.getByLabel('Full Name').fill(user.fullName)
    await page.getByLabel('Email Address').fill(user.email)
    await page.locator('input[autocomplete="new-password"]').fill(user.password)
    await page.getByRole('button', { name: 'Create Account' }).click()

    await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible()
    const stored = await storedSession(page)
    expect(stored?.user.email).toBe(user.email)
    expect(stored?.user.isAdmin).toBe(false)

    // The account exists on the server: the same credentials sign in over the API.
    const asUser = await Api.signIn(user.email, user.password)
    await asUser.dispose()
  })

  test('signing up with an existing email is refused', async ({ page, api }) => {
    const user = await sharedUser(api)

    await page.goto('/sign-up')
    await page.getByLabel('Full Name').fill('Someone Else')
    await page.getByLabel('Email Address').fill(user.email)
    await page.locator('input[autocomplete="new-password"]').fill(user.password)
    await page.getByRole('button', { name: 'Create Account' }).click()

    await expect(page.locator('.auth-message--error')).toHaveText('An account already exists for that email address.')
    expect(await storedSession(page)).toBeNull()
  })

  test('a weak password is rejected before an account is created', async ({ page }) => {
    const user = newUser('weak')
    await page.goto('/sign-up')
    await page.getByLabel('Full Name').fill(user.fullName)
    await page.getByLabel('Email Address').fill(user.email)
    await page.locator('input[autocomplete="new-password"]').fill('short')
    await page.getByRole('button', { name: 'Create Account' }).click()

    await expect(page.locator('.auth-message--error')).toBeVisible()
    await expect(page).toHaveURL(/sign-up/)
    expect(await storedSession(page)).toBeNull()
  })
})

test.describe('Password reset', () => {
  test('forgot password tells the user the truth when no mail can be sent', async ({ page, api }) => {
    // E-N1: the server never passes an SMTP config to the auth handler, so
    // POST /auth/forgot-password answers 503 "not configured" for everyone, and
    // no reset mail can ever be sent. The page then shows "you will receive a
    // reset link shortly" anyway. Whichever way this is fixed (configure mail,
    // or show the 503 message), the user must not be told a link is coming
    // while the server refused. This turns red when that is true.
    knownBug('E-N1: password reset is never configured, and the page claims success')

    const user = await sharedUser(api)
    const refused = await api.http.post('/api/v1/auth/forgot-password', { data: { email: user.email } })

    await page.goto('/forgot-password')
    await page.getByLabel('Email Address').fill(user.email)
    await page.getByRole('button', { name: 'Send Reset Link' }).click()

    // Wait for the page to settle on one message, then check it matches what the server said.
    await expect(page.locator('.auth-message')).toBeVisible()
    const showsError = (await page.locator('.auth-message--error').count()) > 0
    expect(showsError, 'the page shows an error exactly when the server refused').toBe(refused.status() === 503)
  })
})

test.describe('Reset password page', () => {
  test('without a token it explains that the link is not valid', async ({ page }) => {
    await page.goto('/reset-password')
    await expect(page.getByText('This password reset link is invalid or has expired.')).toBeVisible()
    await expect(page.getByLabel('New Password')).toHaveCount(0)
    await expect(page.getByRole('link', { name: 'Request a new link' })).toBeVisible()
  })

  test('with a token but a wrong one, the reset is refused and nothing is signed in', async ({ page }) => {
    await page.goto('/reset-password?token=not-a-real-token')
    await page.getByLabel('New Password').fill(`${newUser('r').password}`)
    await page.getByRole('button', { name: 'Reset Password' }).click()
    await expect(page.locator('.auth-message--error')).toBeVisible()
    expect(await storedSession(page)).toBeNull()
  })
})

test.describe('Identity provider callback', () => {
  test('a callback with no session token explains the problem instead of a blank page', async ({ page }) => {
    await page.goto('/auth/callback')
    await expect(page.getByText('The sign-in callback did not include a session token.')).toBeVisible()
    expect(await storedSession(page)).toBeNull()
  })
})
