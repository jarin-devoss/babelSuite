import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { recordRun } from '../../api/client'
import { knownBug, test, expect } from '../../fixtures'

const here = dirname(fileURLToPath(import.meta.url))
const suiteFile = (suite: string, file: string) => resolve(here, '..', '..', '..', '..', 'e2e', 'suites', suite, file)

test.describe('Suite page', () => {
  test('@smoke shows the suite title, description and profiles from the package', async ({ page }) => {
    await page.goto('/suites/ui-pass')
    await expect(page.locator('.suite-jumbotron__title')).toHaveText('UI Pass')
    await expect(page.locator('.suite-jumbotron__desc')).toContainText('A three-step run that always succeeds.')
  })

  test('the dependency graph shows each step with what it waits for', async ({ page }) => {
    await page.goto('/suites/ui-pass')
    const nodes = page.getByTestId('suite-node')
    await expect(nodes).toHaveCount(3)

    const dependsOn = async (id: string) =>
      (await page.locator(`[data-testid="suite-node"][data-node="${id}"] .suite-node__deps span`).allTextContents()).sort()
    expect(await dependsOn('start')).toEqual(['root'])
    expect(await dependsOn('echo')).toEqual(['start'])
    expect(await dependsOn('verify')).toEqual(['echo'])
  })

  test('the Starlark view shows suite.star exactly as it was published', async ({ page }) => {
    await page.goto('/suites/ui-pass')
    await expect(page.locator('.suite-code-line__content').first()).toBeVisible()
    const published = readFileSync(suiteFile('ui-pass', 'suite.star'), 'utf-8').replace(/\r\n/g, '\n').split('\n')
    const shown = await page.locator('.suite-code-viewer').first().locator('.suite-code-line__content').allTextContents()

    // Trailing newline in the file yields one extra empty line; compare real lines.
    const trim = (lines: string[]) => lines.map((l) => l.trimEnd()).filter((l, i, all) => l !== '' || i < all.length - 1)
    expect(trim(shown)).toEqual(trim(published))
  })

  test('@docker Run Suite launches a run of this suite with the chosen profile', async ({ page, api }) => {
    await page.goto('/suites/ui-env')
    await page.getByRole('button', { name: 'Run Suite' }).click()
    const dialog = page.getByRole('dialog', { name: 'Run suite' })
    await expect(dialog).toBeVisible()
    await dialog.getByLabel('Profile').selectOption('alt.yaml')
    await dialog.getByRole('button', { name: 'Execute' }).click()

    await expect(page).toHaveURL(/\/executions\/[^/]+$/)
    const runId = new URL(page.url()).pathname.split('/').pop() ?? ''
    recordRun(runId)
    const record = await api.run(runId)
    expect(record.suite.id).toBe('ui-env')
    expect(record.profile).toBe('alt.yaml')
    await api.waitForRun(runId)
    expect((await api.logs(runId)).some((line) => line.includes('PROFILE_VAR=from-alt'))).toBe(true)
  })

  test('an unknown suite shows "Package unavailable" instead of crashing', async ({ page }) => {
    await page.goto('/suites/does-not-exist')
    await expect(page.getByText('Package unavailable').first()).toBeVisible()
  })

  test('the mock surface lists the operation the suite declares', async ({ page }) => {
    // E-N8: GET /api/v1/suites/{id} returns each operation with no exchanges, so
    // the page hides the whole Contracts & Mocks panel and counts 0 exchanges,
    // although the same suite's execution record carries the exchange.
    knownBug('E-N8: the suite page never shows mock operations of a catalog suite')

    await page.goto('/suites/ui-mock')
    await expect(page.locator('.suite-op-btn')).toHaveCount(1)
    await expect(page.locator('.suite-op-btn')).toContainText('GET')
    await expect(page.locator('.suite-op-btn')).toContainText('/greeting')
  })

  test('the suites route without an id keeps the app usable', async ({ page }) => {
    await page.goto('/suites')
    await expect(page.getByRole('navigation', { name: 'Primary navigation' })).toBeVisible()
    await expect(page.getByText('Something went wrong')).toHaveCount(0)
  })
})
