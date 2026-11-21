import type { Page } from '@playwright/test'
import { recordRun } from '../../api/client'
import { test, expect } from '../../fixtures'
import { CatalogPage } from '../../pages/CatalogPage'
import { ExecutionPage } from '../../pages/ExecutionPage'

/** Drops text on the Sandbox drop zone, as dragging a copied command or reference would. */
async function dropText(page: Page, text: string) {
  // A drop is resolved against the runnable suites the page has loaded so far,
  // so wait for that load; an earlier drop is wrongly reported as "Ref not found".
  await expect(page.getByText('Loading runnable suites…')).toBeHidden()
  const data = await page.evaluateHandle((value) => {
    const transfer = new DataTransfer()
    transfer.setData('text/plain', value)
    return transfer
  }, text)
  await page.locator('.sbx-dropzone').dispatchEvent('drop', { dataTransfer: data })
}

test.describe('Sandbox', () => {
  test('the command copied from a catalog card resolves to its suite when dropped', async ({ page }) => {
    const catalog = new CatalogPage(page)
    await catalog.goto()
    await catalog.search('ui-pass')
    await catalog.cards().first().getByRole('button', { name: 'Run command' }).click()
    const copied = await page.evaluate(() => navigator.clipboard.readText())
    expect(copied).toBe('babelctl run localhost:5000/e2e/ui-pass:v1.0.0')

    await page.goto('/sandbox')
    await dropText(page, copied)
    const card = page.locator('.sbx-suite-card')
    await expect(card).toContainText('UI Pass')
    await expect(card).toContainText('localhost:5000/e2e/ui-pass')
    await expect(card.locator('.sbx-choice', { hasText: 'local.yaml' })).toHaveClass(/sbx-choice--active/)
  })

  test('@docker Run in Sandbox launches the resolved suite and it finishes healthy', async ({ page, api }) => {
    await page.goto('/sandbox')
    await dropText(page, 'babelctl run localhost:5000/e2e/ui-pass:v1.0.0')
    await page.getByRole('button', { name: 'Run in Sandbox' }).click()

    await expect(page).toHaveURL(/\/executions\/[^/]+$/)
    const live = new ExecutionPage(page)
    recordRun(live.runId())
    await expect(live.status()).toHaveAttribute('data-status', 'healthy', { timeout: 120_000 })
    expect((await api.run(live.runId())).suite.id).toBe('ui-pass')
  })

  test('a reference that is not in the registry cannot start a run', async ({ page, api }) => {
    const before = (await api.runs()).length
    await page.goto('/sandbox')
    await dropText(page, 'localhost:5000/e2e/does-not-exist:v9.9.9')

    await expect(page.locator('.sbx-suite-card')).toContainText('Ref not found')
    await page.getByRole('button', { name: 'Try Launch' }).click()
    await expect(page.locator('.sbx-alert--error')).toBeVisible()
    await expect(page).toHaveURL(/\/sandbox$/)
    expect(await api.runs()).toHaveLength(before)
  })

  test('an empty drop leaves the page without a suite card', async ({ page }) => {
    await page.goto('/sandbox')
    await dropText(page, '   ')
    await expect(page.locator('.sbx-suite-card')).toHaveCount(0)
  })
})
