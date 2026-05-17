import type { Page, Locator } from '@playwright/test'

export class CatalogPage {
  constructor(private page: Page) {}

  async goto() {
    await this.page.goto('/catalog')
  }

  packageCards(): Locator {
    return this.page.locator('.catalog-card')
  }

  searchInput(): Locator {
    return this.page.getByPlaceholder(/search/i)
  }

  async search(term: string) {
    await this.searchInput().fill(term)
  }

  emptyState(): Locator {
    return this.page.locator('.catalog-empty, [class*="empty"]').first()
  }
}
