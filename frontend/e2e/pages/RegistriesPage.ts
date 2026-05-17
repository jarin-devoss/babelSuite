import type { Page, Locator } from '@playwright/test'

export class RegistriesPage {
  constructor(private page: Page) {}

  async goto() {
    await this.page.goto('/settings/registries')
  }

  tableRows(): Locator {
    return this.page.locator('.bs-table-list__row')
  }

  addRegistryButton(): Locator {
    return this.page.getByRole('button', { name: 'Add Registry' })
  }

  saveButton(): Locator {
    return this.page.getByRole('button', { name: /Save/ })
  }

  alert(tone: 'success' | 'error'): Locator {
    return this.page.locator(`.platform-alert--${tone}`)
  }

  panel(): Locator {
    return this.page.locator('.sliding-panel, [class*="sliding-panel"]').first()
  }
}
