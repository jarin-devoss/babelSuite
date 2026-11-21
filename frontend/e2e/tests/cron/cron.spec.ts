import type { Api } from '../../api/client'
import { knownBug, test, expect } from '../../fixtures'

interface CronJob {
  id: string
  name: string
  schedule: string
  enabled: boolean
  suites: { suiteId: string; profile: string }[]
  nextRunAt?: string
  lastRunAt?: string
}

async function jobs(api: Api): Promise<CronJob[]> {
  const body = await api.get<CronJob[] | { jobs?: CronJob[]; cronJobs?: CronJob[] }>('/api/v1/cron-jobs')
  return Array.isArray(body) ? body : (body.jobs ?? body.cronJobs ?? [])
}

async function removeJobs(api: Api, name: string) {
  for (const job of await jobs(api)) {
    if (job.name === name) {
      await api.send('DELETE', `/api/v1/cron-jobs/${job.id}`)
    }
  }
}

async function createInUi(page: import('@playwright/test').Page, name: string, schedule: string, suiteLabel: string) {
  await page.goto('/cron-jobs')
  await page.getByRole('button', { name: 'New Job' }).first().click()
  await page.getByPlaceholder('Weekly report').fill(name)
  await page.getByPlaceholder('0 9 * * 1').fill(schedule)
  await page.getByRole('button', { name: 'Add Suite' }).click()
  await page.locator('.cj-suite-row select').first().selectOption({ label: suiteLabel })
  await page.getByRole('button', { name: 'Create', exact: true }).click()
  await expect(page.getByText('Cron job created.')).toBeVisible()
}

test.describe('Cron jobs', () => {
  test('a job created in the UI is stored with its schedule and suite', async ({ page, api, uniq, cleanup }) => {
    const name = uniq('cron')
    cleanup.add(() => removeJobs(api, name))

    await createInUi(page, name, '0 9 * * 1', 'UI Pass')

    const stored = (await jobs(api)).find((job) => job.name === name)
    expect(stored).toBeDefined()
    expect(stored?.schedule).toBe('0 9 * * 1')
    expect(stored?.enabled).toBe(true)
    expect(stored?.suites.map((s) => s.suiteId)).toEqual(['ui-pass'])

    const row = page.locator('.bs-table-list__row', { hasText: name })
    await expect(row).toContainText('0 9 * * 1')
    await expect(row).toContainText('1 suite')
    await expect(row).toContainText('Never')
  })

  test('an enabled job shows when it will run next', async ({ page, api, uniq, cleanup }) => {
    // E-N10: nextRunAt is declared on the model and shown in the History panel,
    // but nothing ever sets it, so it is always null.
    knownBug('E-N10: nextRunAt is never set')
    const name = uniq('next')
    cleanup.add(() => removeJobs(api, name))
    await createInUi(page, name, '0 9 * * 1', 'UI Pass')
    expect((await jobs(api)).find((job) => job.name === name)?.nextRunAt).toBeTruthy()
  })

  test.describe('a minutely job', () => {
    test.describe.configure({ mode: 'serial', timeout: 180_000 })
    const state: { name: string; firedAfter: number } = { name: '', firedAfter: 0 }

    test('@docker fires on schedule and records its last run', async ({ page, api, uniq, cleanup }) => {
      state.name = uniq('fires')
      cleanup.add(() => removeJobs(api, state.name))
      const startedAt = Date.now()
      await createInUi(page, state.name, '* * * * *', 'UI Pass')

      // lastRunAt is written after the run it started has finished.
      await expect
        .poll(async () => (await jobs(api)).find((job) => job.name === state.name)?.lastRunAt ?? '', {
          timeout: 150_000,
          intervals: [3_000],
        })
        .not.toBe('')
      state.firedAfter = Date.now() - startedAt
      const job = (await jobs(api)).find((j) => j.name === state.name)
      expect((job as { lastError?: string }).lastError ?? '').toBe('')

      await page.goto('/cron-jobs')
      await expect(page.locator('.bs-table-list__row', { hasText: state.name })).not.toContainText('Never')
    })

    test('the run it started is listed with trigger cron', async ({ api }) => {
      // E-N11: the scheduler creates executions with no workspace, and the run
      // list only returns executions of the caller's workspace, so a cron run
      // can never be found in the list or opened from the UI.
      knownBug('E-N11: cron-started runs are invisible in the run list')
      expect(state.firedAfter, 'the previous test must have seen the job fire').toBeGreaterThan(0)
      const cronRuns = (await api.runs()).filter((r) => r.suiteId === 'ui-pass' && r.trigger === 'cron')
      expect(cronRuns.length).toBeGreaterThan(0)
    })
  })

  test('a disabled job does not fire', async ({ page, api, uniq, cleanup }) => {
    test.setTimeout(150_000)
    const name = uniq('off')
    cleanup.add(() => removeJobs(api, name))

    await page.goto('/cron-jobs')
    await page.getByRole('button', { name: 'New Job' }).first().click()
    await page.getByPlaceholder('Weekly report').fill(name)
    await page.getByPlaceholder('0 9 * * 1').fill('* * * * *')
    await page.getByRole('button', { name: 'Add Suite' }).click()
    await page.locator('.cj-suite-row select').first().selectOption({ label: 'UI Pass' })
    await page.locator('.cj-toggle').click()
    await page.getByRole('button', { name: 'Create', exact: true }).click()
    await expect(page.getByText('Cron job created.')).toBeVisible()
    expect((await jobs(api)).find((job) => job.name === name)?.enabled).toBe(false)

    // Two scheduler ticks pass with no run and no last-run time. A negative
    // cannot be awaited any other way than letting the time go by.
    // eslint-disable-next-line playwright/no-wait-for-timeout
    await page.waitForTimeout(125_000)
    expect((await jobs(api)).find((job) => job.name === name)?.lastRunAt ?? '').toBe('')
  })

  test('deleting a job removes it from the server', async ({ page, api, uniq, cleanup }) => {
    const name = uniq('del')
    cleanup.add(() => removeJobs(api, name))
    await createInUi(page, name, '0 9 * * 1', 'UI Pass')

    await page.locator('.bs-table-list__row', { hasText: name }).click()
    await page.getByTitle('Delete job').click()

    await expect(page.locator('.bs-table-list__row', { hasText: name })).toHaveCount(0)
    expect((await jobs(api)).map((job) => job.name)).not.toContain(name)
  })

  test('a job without a name cannot be saved', async ({ page }) => {
    await page.goto('/cron-jobs')
    await page.getByRole('button', { name: 'New Job' }).first().click()
    await expect(page.getByRole('button', { name: 'Create', exact: true })).toBeDisabled()
  })
})
