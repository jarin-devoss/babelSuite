import type { Api, PlatformSettings } from '../../api/client'
import { test, expect } from '../../fixtures'
import { field } from '../../support/field'
import { readSession, registryHost } from '../../support/session'

/** Restores the platform settings to what they were before the test. */
function restoreSettings(api: Api, snapshot: PlatformSettings) {
  return async () => {
    await api.putSettings(snapshot)
  }
}

test.describe('Settings — General', () => {
  test('changing the description is saved on the server and survives a reload', async ({ page, api, uniq, cleanup }) => {
    const original = await api.settings()
    cleanup.add(restoreSettings(api, original))
    const description = `E2E description ${uniq('d')}`

    await page.goto('/settings/general')
    await expect(page.getByRole('button', { name: 'Saved' })).toBeDisabled()
    await field(page, 'Description').fill(description)
    await page.getByRole('button', { name: 'Save Changes' }).click()
    await expect(page.getByText('General settings saved.').first()).toBeVisible()

    expect((await api.settings()).description).toBe(description)
    await page.reload()
    await expect(field(page, 'Description')).toHaveValue(description)
  })

  test('switching the deployment mode changes the stored mode', async ({ page, api, cleanup }) => {
    const original = await api.settings()
    cleanup.add(restoreSettings(api, original))
    const target = ['local', 'hybrid'].find((mode) => mode !== original.mode) ?? 'local'

    await page.goto('/settings/general')
    await page.locator(`input[name="mode"][value="${target}"]`).check()
    await page.getByRole('button', { name: 'Save Changes' }).click()
    await expect(page.getByText('General settings saved.').first()).toBeVisible()

    expect((await api.settings()).mode).toBe(target)
  })

  test('the Save button is disabled until something changes', async ({ page }) => {
    await page.goto('/settings/general')
    await expect(page.getByRole('button', { name: 'Saved' })).toBeDisabled()
  })
})

test.describe('Settings — Registries', () => {
  test('a registry added in the UI is stored and indexed', async ({ page, api, uniq, cleanup }) => {
    const original = await api.settings()
    cleanup.add(restoreSettings(api, original))
    const id = uniq('reg')

    await page.goto('/settings/registries')
    await page.getByRole('button', { name: 'Add Registry' }).first().click()
    await field(page, 'Registry ID').fill(id)
    await field(page, 'Display Name').fill('E2E Extra Registry')
    await field(page, 'Registry URL').fill(readSession().registryUrl)
    // A scope no repository matches, so the catalog is unchanged by this registry.
    await field(page, 'Repository Scope').fill('no-such-scope')
    await page.getByRole('button', { name: 'Save', exact: true }).click()
    // Saving a new registry also syncs it, so the page reports the sync.
    await expect(page.locator('.platform-alert--success').first()).toBeVisible()

    const stored = (await api.settings()).registries.find((r) => r.registryId === id)
    expect(stored?.registryUrl).toBe(readSession().registryUrl)
    expect(stored?.repositoryScope).toBe('no-such-scope')

    const row = page.locator('.bs-table-list__row', { hasText: 'E2E Extra Registry' })
    await expect(row).toBeVisible()

    await expect.poll(async () => (await api.settings()).registries.find((r) => r.registryId === id)?.syncStatus).toBe('Indexed')
  })

  test('the seeded registry is listed with its scope', async ({ page }) => {
    await page.goto('/settings/registries')
    const row = page.locator('.bs-table-list__row', { hasText: 'E2E Registry' })
    await expect(row).toBeVisible()
    await expect(row).toContainText(registryHost())
  })
})

test.describe('Settings — Secrets', () => {
  test('@docker a global override is stored, shown as sensitive, and injected into a run', async ({ page, api, cleanup }) => {
    const original = await api.settings()
    cleanup.add(restoreSettings(api, original))

    await page.goto('/settings/secrets')
    await page.getByRole('button', { name: 'Add Override' }).first().click()
    await field(page, 'Key').fill('GLOBAL_VAR')
    await field(page, 'Value').fill('from-global')
    await page.getByLabel('Sensitive value — mask in logs and UI').check()
    await page.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(page.getByText('Global secrets saved.').first()).toBeVisible()

    // Stored and listed as sensitive.
    const stored = (await api.settings()).secrets.globalOverrides.find((o) => o.key === 'GLOBAL_VAR')
    expect(stored?.sensitive).toBe(true)
    const row = page.locator('.bs-table-list__row', { hasText: 'GLOBAL_VAR' })
    await expect(row).toContainText('sensitive')
    await expect(row).not.toContainText('from-global')

    // And it reaches the step environment of a new run.
    const runId = await api.launch('ui-env')
    await api.waitForRun(runId)
    expect((await api.logs(runId)).some((line) => line.includes('GLOBAL_VAR=from-global'))).toBe(true)
  })
})

test.describe('Settings — Notifications', () => {
  test('SMTP settings are saved, and the password field is blank after a reload', async ({ page, api, cleanup }) => {
    const original = await api.settings()
    cleanup.add(restoreSettings(api, original))

    await page.goto('/settings/notifications')
    await page.getByPlaceholder('smtp.example.com').fill('smtp.e2e.local')
    await page.getByPlaceholder('587').fill('2525')
    await page.getByPlaceholder('notifications@example.com').fill('reports@e2e.local')
    await field(page, 'Password').fill('s3cret-pass')
    await page.getByPlaceholder('BabelSuite <no-reply@example.com>').fill('BabelSuite <no-reply@e2e.local>')
    await page.getByRole('button', { name: 'Save Changes' }).click()
    await expect(page.getByText('Notification settings saved.').first()).toBeVisible()

    const smtp = (await api.settings()).notifications?.smtp
    expect(smtp?.host).toBe('smtp.e2e.local')
    expect(smtp?.port).toBe(2525)
    expect(smtp?.username).toBe('reports@e2e.local')
    // The stored password is never sent back to the browser.
    expect(smtp?.password ?? '').toBe('')

    await page.reload()
    await expect(page.getByPlaceholder('smtp.example.com')).toHaveValue('smtp.e2e.local')
    await expect(field(page, 'Password')).toHaveValue('')
    await expect(field(page, 'Password')).toHaveAttribute('placeholder', '(unchanged)')
  })
})

test.describe('Settings — overview', () => {
  const sections = [
    { title: 'General', path: '/settings/general' },
    { title: 'Execution Agents', path: '/settings/agents' },
    { title: 'OCI Registries', path: '/settings/registries' },
    { title: 'Global Secrets', path: '/settings/secrets' },
    { title: 'Notifications', path: '/settings/notifications' },
    { title: 'Plugins', path: '/settings/plugins' },
  ]

  test('each section card shows a live summary', async ({ page, api }) => {
    const settings = await api.settings()
    await page.goto('/settings')

    const card = (title: string) => page.locator('.settings-index__card', { hasText: title })
    await expect(card('Execution Agents')).toContainText(`${settings.agents.length} configured agent`)
    await expect(card('OCI Registries')).toContainText(`${settings.registries.length} upstream source`)
    await expect(card('Global Secrets')).toContainText(`${settings.secrets.globalOverrides.length} global override`)
    await expect(card('Plugins')).toContainText(`${settings.plugins.length} registered plugin`)
  })

  for (const section of sections) {
    test(`the ${section.title} card opens ${section.path}`, async ({ page }) => {
      await page.goto('/settings')
      await page.locator('.settings-index__card', { hasText: section.title }).click()
      await expect(page).toHaveURL(new RegExp(`${section.path}$`))
    })
  }
})

test.describe('Settings — Agents', () => {
  test('a remote agent added in the UI is stored and shown as not registered', async ({ page, api, uniq, cleanup }) => {
    const original = await api.settings()
    cleanup.add(restoreSettings(api, original))
    const agentId = uniq('agent')

    await page.goto('/settings/agents')
    await page.getByRole('button', { name: 'Add Agent' }).first().click()
    await field(page, 'Agent ID').fill(agentId)
    await field(page, 'Display Name').fill('E2E Remote Worker')
    await field(page, 'Agent Type').selectOption('remote-agent')
    await field(page, 'Worker URL').fill('http://agent.e2e.local:8091')
    await page.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(page.getByText('Execution agents saved.').first()).toBeVisible()

    const stored = (await api.settings()).agents.find((a) => a.agentId === agentId)
    expect(stored?.type).toBe('remote-agent')
    expect(stored?.hostUrl).toBe('http://agent.e2e.local:8091')

    const row = page.locator('.bs-table-list__row', { hasText: 'E2E Remote Worker' })
    await expect(row).toContainText('Not registered')
  })
})
