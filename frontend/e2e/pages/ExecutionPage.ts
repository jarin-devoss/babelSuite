import type { Page, Locator } from '@playwright/test'

export type StepState = 'pending' | 'running' | 'healthy' | 'failed' | 'skipped'

export class ExecutionPage {
  constructor(private page: Page) {}

  async goto(runId: string) {
    await this.page.goto(`/executions/${runId}`)
  }

  /** The run id from the address bar. */
  runId(): string {
    return new URL(this.page.url()).pathname.split('/').pop() ?? ''
  }

  status(): Locator {
    return this.page.getByTestId('exec-status')
  }

  node(stepId: string): Locator {
    return this.page.locator(`[data-testid="exec-node"][data-node="${stepId}"]`)
  }

  nodes(): Locator {
    return this.page.getByTestId('exec-node')
  }

  stat(kind: 'healthy' | 'running' | 'failed' | 'skipped'): Locator {
    return this.page.getByTestId(`exec-stat-${kind}`)
  }

  /** Lines currently rendered. The log is virtualised, so this is the visible window. */
  logLines(): Locator {
    return this.page.getByTestId('log-line')
  }

  logCount(): Locator {
    return this.page.getByTestId('log-count')
  }

  searchLogs(term: string) {
    return this.page.getByLabel('Search logs').fill(term)
  }

  artifactsButton(): Locator {
    return this.page.getByRole('button', { name: 'Artifacts' })
  }

  mockButton(): Locator {
    return this.page.getByRole('button', { name: 'Mock', exact: true })
  }

  graphButton(): Locator {
    return this.page.getByRole('button', { name: 'Graph' })
  }

  restartButton(): Locator {
    return this.page.getByRole('button', { name: /Restart/ })
  }

  copyLogsButton(): Locator {
    return this.page.getByTitle('Copy visible logs')
  }

  artifactDialog(): Locator {
    return this.page.getByRole('dialog').filter({ has: this.page.getByTestId('artifact-total') })
  }
}
