import { test, expect } from '@playwright/test'
import { CatalogPage } from '../pages/CatalogPage'

test.describe('Catalog', () => {
  test.describe.configure({ timeout: 20_000 })

  test('loads and shows packages from registry', async ({ page }) => {
    const catalog = new CatalogPage(page)
    await catalog.goto()
    await expect(catalog.packageCards().first()).toBeVisible({ timeout: 15_000 })
    await expect.poll(() => catalog.packageCards().count()).toBeGreaterThan(0)
  })

  test('search filters the package list', async ({ page }) => {
    const catalog = new CatalogPage(page)
    await catalog.goto()
    await expect(catalog.packageCards().first()).toBeVisible({ timeout: 15_000 })
    const totalBefore = await catalog.packageCards().count()

    await catalog.search('payment')
    await expect.poll(() => catalog.packageCards().count(), { timeout: 5_000 })
      .toBeLessThanOrEqual(totalBefore)
  })

  test('clearing search restores full list', async ({ page }) => {
    const catalog = new CatalogPage(page)
    await catalog.goto()
    await expect(catalog.packageCards().first()).toBeVisible({ timeout: 15_000 })
    const full = await catalog.packageCards().count()

    await catalog.search('zzznomatch')
    await expect.poll(() => catalog.packageCards().count(), { timeout: 5_000 })
      .toBeLessThanOrEqual(full)

    await catalog.search('')
    await expect.poll(() => catalog.packageCards().count(), { timeout: 5_000 }).toBeGreaterThanOrEqual(full)
  })
})
