import { test, expect } from '../../fixtures'
import { CatalogPage } from '../../pages/CatalogPage'

// The e2e registry scope is e2e, so the catalog holds exactly these suites.
// The catalog derives a card title from the repository name ("ui-pass" becomes
// "Ui Pass"), whereas the launch modal shows the suite's own metadata title.
const TITLES = [
  'Ui Broken',
  'Ui Continue',
  'Ui Ctrf',
  'Ui Env',
  'Ui Fail',
  'Ui Junit',
  'Ui Junit Empty',
  'Ui Junit Invalid',
  'Ui Junit Missing',
  'Ui Junit Soft',
  'Ui Mock',
  'Ui Pass',
  'Ui Plugin',
  'Ui Slow',
]

test.describe('Catalog', () => {
  test('@smoke lists exactly the fixture suites', async ({ page }) => {
    const catalog = new CatalogPage(page)
    await catalog.goto()
    await expect(catalog.cards()).toHaveCount(TITLES.length)

    const titles = await catalog.cards().locator('.catalog-card__title').allTextContents()
    expect([...titles].sort()).toEqual([...TITLES].sort())
    await expect(catalog.count()).toContainText(String(TITLES.length))
  })

  test('search narrows the list to the matching suite and clearing restores it', async ({ page }) => {
    const catalog = new CatalogPage(page)
    await catalog.goto()
    await expect(catalog.cards()).toHaveCount(TITLES.length)

    await catalog.search('ui-pass')
    await expect(catalog.cards()).toHaveCount(1)
    await expect(catalog.cards().first().locator('.catalog-card__title')).toHaveText('Ui Pass')

    await catalog.search('')
    await expect(catalog.cards()).toHaveCount(TITLES.length)
  })

  test('a search with no match shows the empty state and no cards', async ({ page }) => {
    const catalog = new CatalogPage(page)
    await catalog.goto()
    await expect(catalog.cards()).toHaveCount(TITLES.length)

    await catalog.search('zzz-no-such-suite')
    await expect(catalog.cards()).toHaveCount(0)
    await expect(catalog.emptyState()).toBeVisible()
  })

  test('a card shows the metadata the suite declares', async ({ page }) => {
    const catalog = new CatalogPage(page)
    await catalog.goto()
    await catalog.search('ui-pass')

    const card = catalog.cards().first()
    await expect(card.locator('.catalog-card__title')).toHaveText('Ui Pass')
    // The owner comes from the repository path ("e2e"), title-cased, not from metadata.owner.
    await expect(card.locator('.catalog-card__origin')).toContainText('E2e')
    await expect(card.locator('.catalog-card__origin')).toContainText('localhost:5000/e2e/ui-pass')
    await expect(card.locator('.catalog-card__version')).toHaveText('v1.0.0')
    // A catalog card is built from registry discovery, so its description names the registry.
    await expect(card.locator('.catalog-card__description')).toContainText('E2E Registry')
  })
})
