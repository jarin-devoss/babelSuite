import { test, expect } from '@playwright/test'
import { RegistriesPage } from '../pages/RegistriesPage'

test.describe('Settings — Registries', () => {
  test.describe.configure({ timeout: 15_000 })

  test('page loads without crashing', async ({ page }) => {
    const reg = new RegistriesPage(page)
    await reg.goto()
    await expect(page.locator('text=Something went wrong')).not.toBeVisible({ timeout: 8_000 })
    await expect(page).toHaveURL(/settings\/registries/)
  })

  test('shows at least one registry row', async ({ page }) => {
    const reg = new RegistriesPage(page)
    await reg.goto()
    await expect(reg.tableRows().first()).toBeVisible({ timeout: 10_000 })
    await expect.poll(() => reg.tableRows().count()).toBeGreaterThan(0)
  })

  test('Add Registry button opens the sliding panel', async ({ page }) => {
    const reg = new RegistriesPage(page)
    await reg.goto()
    await reg.addRegistryButton().first().click()
    await expect(reg.panel()).toBeVisible({ timeout: 5_000 })
  })
})
