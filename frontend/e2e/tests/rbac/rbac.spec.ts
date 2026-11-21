import { Api } from '../../api/client'
import { knownBug, test, expect } from '../../fixtures'
import { HomePage } from '../../pages/HomePage'

// This whole file runs signed in as the non-admin user (see the rbac project).
const ADMIN_ONLY = [
  '/settings',
  '/settings/general',
  '/settings/agents',
  '/settings/registries',
  '/settings/secrets',
  '/settings/notifications',
  '/settings/plugins',
]

test.describe('A non-admin user in the UI', () => {
  test('does not see Settings in the navigation', async ({ page }) => {
    await page.goto('/')
    await expect(page.getByRole('navigation', { name: 'Primary navigation' })).toBeVisible()
    await expect(page.getByRole('link', { name: 'Settings' })).toHaveCount(0)
    await expect(page.getByRole('link', { name: 'Catalog' })).toBeVisible()
  })

  for (const route of ADMIN_ONLY) {
    test(`is sent away from ${route}`, async ({ page }) => {
      await page.goto(route)
      await expect(page).toHaveURL('/')
      await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible()
    })
  }

  test('@docker can still launch a run and sees it in their own list', async ({ page }) => {
    const home = new HomePage(page)
    await home.goto()
    await home.launch('UI Pass', 'local.yaml')
    await expect(page).toHaveURL(/\/executions\/[^/]+$/)
    const runId = new URL(page.url()).pathname.split('/').pop() ?? ''

    const user = await Api.user()
    expect((await user.runs()).map((r) => r.id)).toContain(runId)
    await user.waitForRun(runId, 120_000)
    await user.dispose()
  })
})

test.describe('A non-admin user against the API', () => {
  test('is refused every write to the platform settings', async () => {
    const user = await Api.user()
    expect(await user.status('PUT', '/api/v1/platform-settings', { agents: [], registries: [], secrets: { provider: 'none', globalOverrides: [] }, plugins: [] })).toBe(403)
    expect(await user.status('POST', '/api/v1/platform-settings/plugins', { name: 'x', kind: 'plugin', trigger: '/x', lua: 'return {}' })).toBe(403)
    expect(await user.status('POST', '/api/v1/platform-settings/registries/e2e-zot/sync')).toBe(403)
    expect(await user.status('POST', '/api/v1/sandboxes/reap-all')).toBe(403)
    await user.dispose()
  })

  test('cannot list the registered plugins and their Lua source', async ({ api, cleanup }) => {
    // E-N15: docs/plugins.md says every plugin endpoint but check and validate
    // needs an admin session, yet the list (which carries each plugin's Lua
    // source) answers 200 to any signed-in user.
    knownBug('E-N15: the plugin list is readable by any user')

    const original = await api.settings()
    cleanup.add(async () => {
      await api.putSettings(original)
    })
    const user = await Api.user()
    expect(await user.status('GET', '/api/v1/platform-settings/plugins')).toBe(403)
    await user.dispose()
  })
})
