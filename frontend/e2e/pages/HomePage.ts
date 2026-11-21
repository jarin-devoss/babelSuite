import type { Page, Locator } from '@playwright/test'

export class HomePage {
  constructor(private page: Page) {}

  async goto() {
    await this.page.goto('/')
  }

  newExecutionButton(): Locator {
    return this.page.getByRole('button', { name: 'New Execution' })
  }

  modal(): Locator {
    return this.page.getByRole('dialog', { name: 'New execution' })
  }

  suiteSelect(): Locator {
    return this.modal().getByLabel('Suite')
  }

  profileOption(fileName: string): Locator {
    return this.modal().locator(`[data-testid="profile-option"][data-profile="${fileName}"]`)
  }

  profileOptions(): Locator {
    return this.modal().getByTestId('profile-option')
  }

  executeButton(): Locator {
    return this.modal().getByRole('button', { name: 'Execute' })
  }

  cancelButton(): Locator {
    return this.modal().getByRole('button', { name: 'Cancel' })
  }

  error(): Locator {
    return this.modal().locator('.ex-error')
  }

  runRows(): Locator {
    return this.page.getByTestId('run-row')
  }

  runRow(runId: string): Locator {
    return this.page.locator(`[data-testid="run-row"][data-run-id="${runId}"]`)
  }

  emptyNotice(): Locator {
    return this.page.locator('.runs-notice')
  }

  /** Opens the modal, picks a suite (and profile) and presses Execute. */
  async launch(suiteTitle: string, profileFile?: string) {
    await this.newExecutionButton().click()
    await this.suiteSelect().selectOption({ label: suiteTitle })
    if (profileFile) {
      await this.profileOption(profileFile).click()
    }
    await this.executeButton().click()
  }
}
