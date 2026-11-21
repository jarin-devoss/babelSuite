import type { Page, Locator } from '@playwright/test'

export class CatalogPage {
  constructor(private page: Page) {}

  async goto() {
    await this.page.goto('/catalog')
  }

  cards(): Locator {
    return this.page.getByTestId('catalog-card')
  }

  card(packageId: string): Locator {
    return this.page.locator(`[data-testid="catalog-card"][data-package="${packageId}"]`)
  }

  searchInput(): Locator {
    return this.page.getByLabel('Search packages')
  }

  count(): Locator {
    return this.page.getByTestId('catalog-count')
  }

  emptyState(): Locator {
    return this.page.locator('.catalog-empty')
  }

  async search(term: string) {
    await this.searchInput().fill(term)
  }
}
