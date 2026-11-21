import { recordRun, stepStatuses } from '../../api/client'
import { test, expect } from '../../fixtures'
import { ExecutionPage } from '../../pages/ExecutionPage'
import { HomePage } from '../../pages/HomePage'

test.describe('Launching a run', () => {
  test('@smoke @docker the New Execution modal runs ui-pass to a healthy result', async ({ page, api }) => {
    const home = new HomePage(page)
    await home.goto()
    await home.launch('UI Pass', 'local.yaml')

    await expect(page).toHaveURL(/\/executions\/[^/]+$/)
    const live = new ExecutionPage(page)
    const runId = live.runId()
    recordRun(runId)

    await expect(live.status()).toHaveAttribute('data-status', 'healthy', { timeout: 120_000 })
    await expect(live.stat('healthy')).toHaveText('3')
    await expect(live.stat('failed')).toHaveText('0')

    // The backend agrees with what the page shows.
    const record = await api.run(runId)
    expect(record.status).toBe('Healthy')
    expect(record.suite.id).toBe('ui-pass')
    expect(record.profile).toBe('local.yaml')
    expect(Object.values(stepStatuses(record))).not.toContain('failed')
  })

  test('@docker the finished run is the first row of the recent executions', async ({ page, api }) => {
    const runId = await api.launch('ui-pass')
    await api.waitForRun(runId)

    const home = new HomePage(page)
    await home.goto()
    const row = home.runRow(runId)
    await expect(row).toBeVisible()
    await expect(row.getByTestId('run-status')).toHaveAttribute('data-status', 'healthy')
    await expect(row).toContainText('UI Pass')
    await expect(row).toContainText('local.yaml')
    await expect(home.runRows().first()).toHaveAttribute('data-run-id', runId)

    await row.getByTitle('Open live execution').click()
    await expect(page).toHaveURL(new RegExp(`/executions/${runId}$`))
  })

  test('Cancel closes the modal and creates no run', async ({ page, api }) => {
    const before = (await api.runs()).length
    const home = new HomePage(page)
    await home.goto()
    await home.newExecutionButton().click()
    await expect(home.modal()).toBeVisible()
    await home.cancelButton().click()
    await expect(home.modal()).toBeHidden()
    expect(await api.runs()).toHaveLength(before)
  })

  test('Escape closes the modal and creates no run', async ({ page, api }) => {
    const before = (await api.runs()).length
    const home = new HomePage(page)
    await home.goto()
    await home.newExecutionButton().click()
    await expect(home.modal()).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(home.modal()).toBeHidden()
    expect(await api.runs()).toHaveLength(before)
  })

  test('the modal offers every profile of the chosen suite and marks the default', async ({ page }) => {
    const home = new HomePage(page)
    await home.goto()
    await home.newExecutionButton().click()
    await home.suiteSelect().selectOption({ label: 'UI Env' })

    await expect(home.profileOptions()).toHaveCount(2)
    await expect(home.profileOption('local.yaml')).toBeVisible()
    await expect(home.profileOption('alt.yaml')).toBeVisible()
    await expect(home.profileOption('local.yaml')).toHaveClass(/ex-profile-card--active/)

    await home.profileOption('alt.yaml').click()
    await expect(home.profileOption('alt.yaml')).toHaveClass(/ex-profile-card--active/)
    await expect(home.profileOption('local.yaml')).not.toHaveClass(/ex-profile-card--active/)
  })
})
