import { execFileSync } from 'node:child_process'
import { Api } from '../../api/client'
import { test, expect } from '../../fixtures'
import { restartControlPlane, startSecondControlPlane } from '../../support/stack'
import { readSession } from '../../support/session'

test.describe('Error states', () => {
  // A request the server answers with 500 must show the page's own message,
  // not a blank page or the error boundary.
  const cases = [
    { name: 'General', path: '/settings/general', api: '**/api/v1/platform-settings', text: 'General settings unavailable' },
    { name: 'Execution agents', path: '/settings/agents', api: '**/api/v1/platform-settings', text: 'Execution agents unavailable' },
    { name: 'Global secrets', path: '/settings/secrets', api: '**/api/v1/platform-settings', text: 'Global secrets unavailable' },
  ]

  for (const item of cases) {
    test(`${item.name} shows its own message when the settings request fails`, async ({ page }) => {
      await page.route(item.api, (route) => route.fulfill({ status: 500, contentType: 'application/json', body: '{"error":"boom"}' }))
      await page.goto(item.path)
      await expect(page.getByText(item.text).first()).toBeVisible()
      await expect(page.getByText('Something went wrong')).toHaveCount(0)
    })
  }

  test('an unknown route does not render a blank page', async ({ page }) => {
    await page.goto('/this/route/does/not/exist')
    // Either a not-found view or a redirect, but the app shell must be there.
    await expect(page.getByRole('navigation', { name: 'Primary navigation' })).toBeVisible()
  })
})

test.describe('Session expiry', () => {
  test('an invalid token sends the user to sign-in and keeps the page to come back to', async ({ page }) => {
    await page.goto('/')
    await page.evaluate(() => {
      const raw = window.localStorage.getItem('babelsuite.session')
      if (raw) {
        const session = JSON.parse(raw) as { token: string }
        session.token = 'not-a-valid-token'
        window.localStorage.setItem('babelsuite.session', JSON.stringify(session))
      }
    })
    await page.goto('/catalog')
    await expect(page).toHaveURL(/\/sign-in\?returnTo=%2Fcatalog/)
  })
})

/** What Docker still holds for a run: its containers and its network. */
function leftovers(runId: string): string {
  const ids = (args: string[]) => execFileSync('docker', args, { encoding: 'utf-8' }).trim()
  return [
    ids(['ps', '-aq', '--filter', `label=babelsuite.run_id=${runId}`]),
    ids(['network', 'ls', '-q', '--filter', `name=babel-net-${runId}`]),
  ].join('')
}

test.describe('Restarts', () => {
  test.describe.configure({ mode: 'serial', timeout: 240_000 })

  test('@docker a finished run is still there after the control plane restarts', async ({ api }) => {
    // A restarted control plane lost the workspace of every saved run, so each
    // was 404 for the signed-in user and the list was empty.
    const runId = await api.launch('ui-pass')
    await api.waitForRun(runId)

    await restartControlPlane()

    const again = await Api.admin()
    const record = await again.run(runId)
    expect(record.status).toBe('Healthy')
    expect((await again.runs()).map((r) => r.id)).toContain(runId)
    await again.dispose()
  })

  test('@docker a run that was running when the control plane was killed ends Interrupted, and its leftovers are removed', async ({ api }) => {
    const runId = await api.launch('ui-slow')
    await expect
      .poll(async () => (await api.logs(runId)).some((line) => line.includes('TICK 1')), { timeout: 60_000 })
      .toBe(true)

    await restartControlPlane()

    const again = await Api.admin()
    try {
      // Its lease runs out and the restarted control plane ends it; nothing resumes it.
      await expect
        .poll(async () => (await again.run(runId)).status, { timeout: 90_000, intervals: [2_000] })
        .toBe('Interrupted')
      const record = await again.run(runId)
      expect(record.events.some((event) => /interrupted/i.test(event.text))).toBe(true)
      expect((await again.runs()).map((r) => r.id)).toContain(runId)

      // The run's container and network were left by a process that is gone.
      await expect.poll(() => leftovers(runId), { timeout: 60_000, intervals: [2_000] }).toBe('')
    } finally {
      await again.dispose()
    }
  })

  test('@docker a second control plane on the same database sees the same runs', async ({ api }) => {
    // A second control plane lists the runs saved before it started.
    const runId = await api.launch('ui-pass')
    await api.waitForRun(runId)

    const second = await startSecondControlPlane()
    try {
      const session = readSession()
      const other = await Api.signIn(session.email, session.password, second.url)
      expect(await other.status('GET', `/api/v1/executions/${runId}`)).toBe(200)
      await other.dispose()
    } finally {
      second.stop()
    }
  })
})
