import { test, expect } from '@playwright/test'
import { HomePage } from '../pages/HomePage'

test.describe('Home — Executions', () => {
  test.describe.configure({ timeout: 20_000 })

  test('page loads without crashing', async ({ page }) => {
    const home = new HomePage(page)
    await home.goto()
    await expect(page.locator('text=Something went wrong')).not.toBeVisible({ timeout: 8_000 })
    await expect(page).toHaveURL('/')
  })

  test('New Execution button is present', async ({ page }) => {
    const home = new HomePage(page)
    await home.goto()
    await expect(home.newExecutionButton()).toBeVisible({ timeout: 10_000 })
  })

  test('New Execution button is enabled when catalog has suites', async ({ page }) => {
    const home = new HomePage(page)
    await home.goto()
    await expect(home.newExecutionButton()).toBeEnabled({ timeout: 10_000 })
  })

  test('clicking New Execution opens the modal', async ({ page }) => {
    const home = new HomePage(page)
    await home.goto()
    await home.newExecutionButton().click()
    await expect(home.executionModal()).toBeVisible({ timeout: 5_000 })
  })

  test('modal shows at least one suite option', async ({ page }) => {
    const home = new HomePage(page)
    await home.goto()
    await home.newExecutionButton().click()
    await expect(home.executionModal()).toBeVisible({ timeout: 5_000 })
    const select = home.suiteSelect()
    const card = home.executionModal().locator('.ex-suite-card')
    const hasSelect = await select.isVisible().catch(() => false)
    const hasCard = await card.isVisible().catch(() => false)
    expect(hasSelect || hasCard).toBe(true)
  })

  test('modal Execute button is present', async ({ page }) => {
    const home = new HomePage(page)
    await home.goto()
    await home.newExecutionButton().click()
    await expect(home.executionModal()).toBeVisible({ timeout: 5_000 })
    await expect(home.executeButton()).toBeVisible()
  })

  test('Cancel closes the modal', async ({ page }) => {
    const home = new HomePage(page)
    await home.goto()
    await home.newExecutionButton().click()
    await expect(home.executionModal()).toBeVisible({ timeout: 5_000 })
    await home.cancelButton().click()
    await expect(home.executionModal()).not.toBeVisible({ timeout: 3_000 })
  })

  test('Escape key closes the modal', async ({ page }) => {
    const home = new HomePage(page)
    await home.goto()
    await home.newExecutionButton().click()
    await expect(home.executionModal()).toBeVisible({ timeout: 5_000 })
    await page.keyboard.press('Escape')
    await expect(home.executionModal()).not.toBeVisible({ timeout: 3_000 })
  })

  test('recent runs table or empty notice is shown', async ({ page }) => {
    const home = new HomePage(page)
    await home.goto()
    await expect.poll(
      () => Promise.all([
        home.recentRunsRows().first().isVisible().catch(() => false),
        home.noExecutionsNotice().isVisible().catch(() => false),
      ]).then(([rows, notice]) => rows || notice),
      { timeout: 5_000 },
    ).toBe(true)
  })
})
