import type { Page, Locator } from '@playwright/test'

export class SignInPage {
  constructor(private page: Page) {}

  async goto() {
    await this.page.goto('/sign-in')
  }

  async signIn(email: string, password: string) {
    await this.page.locator('input[autocomplete="email"]').fill(email)
    await this.page.locator('input[autocomplete="current-password"]').fill(password)
    await this.page.getByRole('button', { name: 'Sign In' }).click()
  }

  errorMessage(): Locator {
    return this.page.locator('.auth-message--error')
  }
}
