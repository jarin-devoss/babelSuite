import type { Page, Locator } from '@playwright/test'

export class HomePage {
  constructor(private page: Page) {}

  async goto() {
    await this.page.goto('/')
  }

  newExecutionButton(): Locator {
    return this.page.getByRole('button', { name: 'New Execution' })
  }

  executionModal(): Locator {
    return this.page.getByRole('dialog', { name: 'New execution' })
  }

  suiteSelect(): Locator {
    return this.executionModal().locator('.ex-select')
  }

  profileCards(): Locator {
    return this.executionModal().locator('.ex-profile-card')
  }

  executeButton(): Locator {
    return this.executionModal().getByRole('button', { name: 'Execute' })
  }

  cancelButton(): Locator {
    return this.executionModal().getByRole('button', { name: 'Cancel' })
  }

  closeButton(): Locator {
    return this.executionModal().getByRole('button', { name: 'Close' })
  }

  errorMessage(): Locator {
    return this.executionModal().locator('.ex-error')
  }

  recentRunsRows(): Locator {
    return this.page.locator('.runs-table__row')
  }

  noExecutionsNotice(): Locator {
    return this.page.locator('.runs-notice')
  }
}
