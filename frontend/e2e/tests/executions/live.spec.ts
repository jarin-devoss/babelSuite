import { stepStatuses } from '../../api/client'
import { test, expect } from '../../fixtures'
import { ExecutionPage } from '../../pages/ExecutionPage'

async function reapRun(api: import('../../api/client').Api, runId: string) {
  for (const environment of await api.environments()) {
    if (environment.runId === runId) {
      await api.reap(environment.sandboxId)
    }
  }
}

test.describe('The live execution page', () => {
  test('@docker steps run in dependency order and the sidebar follows the backend', async ({ page, api }) => {
    const runId = await api.launch('ui-pass')
    const live = new ExecutionPage(page)
    await live.goto(runId)

    await expect(live.status()).toHaveAttribute('data-status', 'healthy', { timeout: 120_000 })
    for (const step of ['echo', 'verify']) {
      await expect(live.node(step)).toHaveAttribute('data-status', 'healthy')
    }

    // The order of the events is the dependency order: echo finishes before verify starts.
    const record = await api.run(runId)
    const index = (source: string, status: string) => record.events.findIndex((e) => e.source === source && e.status.toLowerCase() === status)
    expect(index('echo', 'healthy')).toBeGreaterThan(-1)
    expect(index('verify', 'running')).toBeGreaterThan(-1)
    expect(index('echo', 'healthy')).toBeLessThan(index('verify', 'healthy'))
    expect(stepStatuses(record).echo).toBe('healthy')
  })

  test('@docker selecting a step filters the log to that step and shows the profile value', async ({ page, api }) => {
    const runId = await api.launch('ui-pass')
    await api.waitForRun(runId)
    const live = new ExecutionPage(page)
    await live.goto(runId)

    await live.node('echo').click()
    // The profile's env (E2E_VAR) reached the container.
    await expect(live.logLines().filter({ hasText: 'UI-PASS value=pass-value' })).toHaveCount(1)
    await expect(live.logLines().filter({ hasText: 'UI-PASS verified' })).toHaveCount(0)

    await live.node('verify').click()
    await expect(live.logLines().filter({ hasText: 'UI-PASS verified' })).toHaveCount(1)
    await expect(live.logLines().filter({ hasText: 'value=pass-value' })).toHaveCount(0)
  })

  test('@docker log lines stream in while the run is still going', async ({ page, api, cleanup }) => {
    const runId = await api.launch('ui-slow')
    cleanup.add(() => reapRun(api, runId))
    const live = new ExecutionPage(page)
    await live.goto(runId)

    await live.node('tick').click()
    await live.searchLogs('TICK 3')
    await expect(live.logLines().filter({ hasText: /TICK 3$/ })).toHaveCount(1, { timeout: 60_000 })
    // The run has not finished: the line arrived over the stream, not from a finished record.
    await expect(live.status()).toHaveAttribute('data-status', 'booting')
  })

  test('@docker a reload mid-run resumes the log without duplicating lines', async ({ page, api, cleanup }) => {
    const runId = await api.launch('ui-slow')
    cleanup.add(() => reapRun(api, runId))
    const live = new ExecutionPage(page)
    await live.goto(runId)
    await live.node('tick').click()
    await live.searchLogs('TICK 4')
    await expect(live.logLines().filter({ hasText: /TICK 4$/ })).toHaveCount(1, { timeout: 60_000 })

    await page.reload()
    await live.node('tick').click()
    await live.searchLogs('TICK ')
    // Every line seen before the reload is back exactly once, and new ones keep arriving.
    await live.searchLogs('TICK 3')
    await expect(live.logLines().filter({ hasText: /TICK 3$/ })).toHaveCount(1)
    await live.searchLogs('TICK 6')
    await expect(live.logLines().filter({ hasText: /TICK 6$/ })).toHaveCount(1, { timeout: 60_000 })
  })

  test('@docker the stream reconnects after the network drops', async ({ page, api, cleanup, context }) => {
    const runId = await api.launch('ui-slow')
    cleanup.add(() => reapRun(api, runId))
    const live = new ExecutionPage(page)
    await live.goto(runId)
    await live.node('tick').click()
    await live.searchLogs('TICK 2')
    await expect(live.logLines().filter({ hasText: /TICK 2$/ })).toHaveCount(1, { timeout: 60_000 })

    await context.setOffline(true)
    // Lines are produced while the page cannot receive them; time has to pass.
    // eslint-disable-next-line playwright/no-wait-for-timeout
    await page.waitForTimeout(6_000)
    await context.setOffline(false)

    await live.searchLogs('TICK 6')
    await expect(live.logLines().filter({ hasText: /TICK 6$/ })).toHaveCount(1, { timeout: 60_000 })
  })

  test('@docker Copy visible logs puts the shown lines on the clipboard', async ({ page, api }) => {
    const runId = await api.launch('ui-pass')
    await api.waitForRun(runId)
    const live = new ExecutionPage(page)
    await live.goto(runId)
    await live.node('verify').click()
    await expect(live.logLines().filter({ hasText: 'UI-PASS verified' })).toHaveCount(1)

    await live.copyLogsButton().click()
    const copied = await page.evaluate(() => navigator.clipboard.readText())
    expect(copied).toContain('UI-PASS verified')
    expect(copied).not.toContain('value=pass-value')
  })

  test('@docker Restart starts a new run of the same suite and profile', async ({ page, api }) => {
    const first = await api.launch('ui-pass', 'local.yaml')
    await api.waitForRun(first)
    const live = new ExecutionPage(page)
    await live.goto(first)

    await live.restartButton().click()
    await expect(page).not.toHaveURL(new RegExp(`/executions/${first}$`))
    const second = live.runId()
    expect(second).not.toBe(first)

    const record = await api.run(second)
    expect(record.suite.id).toBe('ui-pass')
    expect(record.profile).toBe('local.yaml')
    await api.waitForRun(second)
  })
})
