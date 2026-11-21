import type { Api } from '../../api/client'
import { test, expect } from '../../fixtures'
import { HomePage } from '../../pages/HomePage'

interface SuiteProfiles {
  suiteId: string
  defaultProfileId: string
  profiles: { id: string; fileName: string; launchable: boolean; yaml: string }[]
}

const profilesOf = (api: Api, suite: string) => api.get<SuiteProfiles>(`/api/v1/profiles/suites/${suite}`)

async function removeProfile(api: Api, suite: string, fileName: string) {
  const current = await profilesOf(api, suite)
  const found = current.profiles.find((p) => p.fileName === fileName)
  if (found) {
    await api.send('DELETE', `/api/v1/profiles/suites/${suite}/${found.id}`)
  }
}

test.describe('Profiles', () => {
  test('lists the profiles a suite ships with', async ({ page }) => {
    await page.goto('/profiles')
    await page.getByLabel('Suite').selectOption('ui-env')

    await expect(page.getByTestId('profile-card')).toHaveCount(2)
    await expect(page.locator('[data-testid="profile-card"][data-profile="local.yaml"]')).toContainText('Local')
    await expect(page.locator('[data-testid="profile-card"][data-profile="alt.yaml"]')).toContainText('Alt')
    await expect(page.locator('[data-testid="profile-card"][data-profile="local.yaml"]')).toContainText('Default')
  })

  test('@docker a profile created in the UI is saved and changes what a run sees', async ({ page, api, uniq, cleanup }) => {
    const fileName = `${uniq('prof')}.yaml`
    cleanup.add(() => removeProfile(api, 'ui-env', fileName))

    await page.goto('/profiles')
    await page.getByLabel('Suite').selectOption('ui-env')
    await page.getByRole('button', { name: 'New Profile' }).click()

    await page.getByLabel('Display Name').fill('E2E From UI')
    await page.getByLabel('File Name').fill(fileName)
    await page.locator('.profiles-editor-wrap textarea').fill('env:\n  PROFILE_VAR: from-ui\n')
    await page.getByRole('button', { name: 'Save' }).click()
    await expect(page.getByText('Profile created.').first()).toBeVisible()

    // The server holds it.
    const stored = await profilesOf(api, 'ui-env')
    const created = stored.profiles.find((p) => p.fileName === fileName)
    expect(created?.yaml).toContain('PROFILE_VAR: from-ui')

    // And it is offered when launching, and it takes effect in the run.
    const home = new HomePage(page)
    await home.goto()
    await home.newExecutionButton().click()
    await home.suiteSelect().selectOption({ label: 'UI Env' })
    await expect(home.profileOption(fileName)).toBeVisible()
    await home.cancelButton().click()

    const runId = await api.launch('ui-env', fileName)
    await api.waitForRun(runId)
    expect((await api.logs(runId)).some((line) => line.includes('PROFILE_VAR=from-ui'))).toBe(true)
  })

  test('marking a profile as default changes the launch default', async ({ page, api, cleanup }) => {
    const before = await profilesOf(api, 'ui-env')
    const original = before.defaultProfileId
    cleanup.add(async () => {
      await api.send('POST', `/api/v1/profiles/suites/ui-env/${original}/default`)
    })

    await page.goto('/profiles')
    await page.getByLabel('Suite').selectOption('ui-env')
    await page.locator('[data-testid="profile-card"][data-profile="alt.yaml"]').click()
    await page.getByRole('button', { name: 'Default', exact: true }).click()
    await expect(page.getByText('alt.yaml is now the default launch profile').first()).toBeVisible()

    const after = await profilesOf(api, 'ui-env')
    expect(after.profiles.find((p) => p.id === after.defaultProfileId)?.fileName).toBe('alt.yaml')

    const home = new HomePage(page)
    await home.goto()
    await home.newExecutionButton().click()
    await home.suiteSelect().selectOption({ label: 'UI Env' })
    await expect(home.profileOption('alt.yaml')).toHaveClass(/ex-profile-card--active/)
  })

  test('deleting a created profile removes it from the server and the launcher', async ({ page, api, uniq, cleanup }) => {
    const fileName = `${uniq('del')}.yaml`
    cleanup.add(() => removeProfile(api, 'ui-env', fileName))
    await api.send('POST', '/api/v1/profiles/suites/ui-env', {
      name: 'To Delete',
      fileName,
      description: 'Removed by the test.',
      scope: 'suite',
      yaml: 'env:\n  X: "1"\n',
      launchable: true,
    })

    await page.goto('/profiles')
    await page.getByLabel('Suite').selectOption('ui-env')
    await page.locator(`[data-testid="profile-card"][data-profile="${fileName}"]`).click()
    await page.getByRole('button', { name: 'Delete profile' }).click()
    await expect(page.getByText('Profile removed from the suite-scoped profile set.').first()).toBeVisible()

    expect((await profilesOf(api, 'ui-env')).profiles.map((p) => p.fileName)).not.toContain(fileName)
    await expect(page.locator(`[data-testid="profile-card"][data-profile="${fileName}"]`)).toHaveCount(0)
  })

  test('a profile with invalid YAML is rejected and nothing is saved', async ({ page, api, uniq }) => {
    const fileName = `${uniq('bad')}.yaml`
    const before = (await profilesOf(api, 'ui-env')).profiles.length

    await page.goto('/profiles')
    await page.getByLabel('Suite').selectOption('ui-env')
    await page.getByRole('button', { name: 'New Profile' }).click()
    await page.getByLabel('File Name').fill(fileName)
    await page.locator('.profiles-editor-wrap textarea').fill('env:\n  A: [unclosed\n bad: : :\n')
    await page.getByRole('button', { name: 'Save' }).click()

    // The server refuses it; the message is generic ("Could not process request."), so assert only that one is shown.
    await expect(page.locator('.profiles-alert--error').first()).toBeVisible()
    expect((await profilesOf(api, 'ui-env')).profiles).toHaveLength(before)
  })
})
