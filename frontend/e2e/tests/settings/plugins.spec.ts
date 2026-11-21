import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import type { Api } from '../../api/client'
import { stepStatuses } from '../../api/client'
import { knownBug, test, expect } from '../../fixtures'
import { field } from '../../support/field'

const here = dirname(fileURLToPath(import.meta.url))
const fixture = (file: string) => readFileSync(resolve(here, '..', '..', '..', '..', 'e2e', 'plugins', 'e2e-echo', file), 'utf-8')

async function unregister(api: Api, name: string) {
  const settings = await api.settings()
  if (settings.plugins.some((p) => p.name === name)) {
    await api.send('DELETE', `/api/v1/platform-settings/plugins/${name}`)
  }
}

test.describe.configure({ mode: 'serial' })

test.describe('Settings — Plugins', () => {
  // The three tests share one registered plugin, so it is removed once, at the end.
  test.afterAll(async ({ api }) => {
    await unregister(api, 'e2e-echo')
  })

  test('registering a plugin in the UI stores it with its trigger route', async ({ page, api }) => {
    await unregister(api, 'e2e-echo')

    await page.goto('/settings/plugins')
    await page.getByRole('button', { name: 'Register Plugin' }).first().click()
    await field(page, 'Plugin Name').fill('e2e-echo')
    await field(page, 'Version').fill('1.0.0')
    await field(page, 'Kind').fill('plugin')
    await field(page, 'Trigger Route').fill('/_babelsuite/plugins/e2e-echo/start')
    await field(page, 'Variants').fill('e2e-echo')
    await field(page, 'Operations').fill('echo')
    await field(page, 'Lua Plugin Source').fill(fixture('plugin.lua'))
    await field(page, 'Starlark Module Source').fill(fixture('plugin.star'))
    await page.getByRole('button', { name: 'Register', exact: true }).click()
    await expect(page.getByText('Plugin "e2e-echo" registered.').first()).toBeVisible()

    const stored = (await api.settings()).plugins.find((p) => p.name === 'e2e-echo')
    expect(stored?.trigger).toBe('/_babelsuite/plugins/e2e-echo/start')
    expect(stored?.operations).toEqual(['echo'])
    // The Lua and the Starlark wrapper reached the server exactly as typed.
    expect(stored?.lua).toBe(fixture('plugin.lua'))
    expect(stored?.star).toBe(fixture('plugin.star'))

    const row = page.locator('.bs-table-list__row', { hasText: 'e2e-echo' })
    await expect(row).toContainText('v1.0.0')
    await expect(row).toContainText('echo')
  })

  test('@docker a suite step dispatches to the registered plugin and uses its verdict', async ({ api }) => {
    const runId = await api.launch('ui-plugin')
    const record = await api.waitForRun(runId, 240_000)
    const steps = stepStatuses(record)

    const logs = await api.logs(runId)
    expect(steps.hello, `the passing step; log: ${logs.slice(-6).join(' | ')}`).toBe('healthy')
    expect(steps.rejected, 'the plugin rejected this step').toBe('failed')
    expect(record.status).toBe('Failed')

    expect(logs.some((line) => line.includes('e2e-echo: hi from the plugin'))).toBe(true)
    expect(logs.some((line) => line.includes('rejected on request: not this one'))).toBe(true)
  })

  test('@docker a removed plugin makes its steps fail with a clear reason', async ({ api }) => {
    await api.send('DELETE', '/api/v1/platform-settings/plugins/e2e-echo')
    expect((await api.settings()).plugins.map((p) => p.name)).not.toContain('e2e-echo')

    const runId = await api.launch('ui-plugin')
    const record = await api.waitForRun(runId, 240_000)
    // The suite cannot even be evaluated: its load() of the plugin has nothing to resolve.
    expect(record.status).toBe('Failed')
    expect(stepStatuses(record).boot).toBe('failed')
    expect((await api.logs(runId)).some((line) => line.includes('cannot load @plugins/e2e-echo'))).toBe(true)
  })

  test('the row menu deletes the only plugin in the list', async ({ page, api }) => {
    // E-N13: .bs-table-list clips its children (overflow: hidden), so the
    // dropdown of the last row is cut off and its Delete item cannot be clicked.
    knownBug('E-N13: the row menu of the last table row is clipped')
    await api.send('POST', '/api/v1/platform-settings/plugins', {
      name: 'e2e-echo',
      version: '1.0.0',
      kind: 'plugin',
      variants: ['e2e-echo'],
      operations: ['echo'],
      lua: fixture('plugin.lua'),
      star: fixture('plugin.star'),
      trigger: '/_babelsuite/plugins/e2e-echo/start',
    })

    await page.goto('/settings/plugins')
    const row = page.locator('.bs-table-list__row', { hasText: 'e2e-echo' })
    await row.getByRole('button', { name: 'Row actions' }).click()
    await row.getByRole('button', { name: 'Delete' }).click({ timeout: 10_000 })
    await row.getByRole('button', { name: 'Yes' }).click({ timeout: 10_000 })
    await expect(page.getByText('Plugin "e2e-echo" removed.').first()).toBeVisible()
  })
})
