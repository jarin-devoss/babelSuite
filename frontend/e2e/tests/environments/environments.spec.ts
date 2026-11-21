import { test, expect } from '../../fixtures'

test.describe('Environments', () => {
  test('@docker a running run appears with its containers, and Nuke removes everything it started', async ({ page, api, docker }) => {
    const runId = await api.launch('ui-slow')

    await page.goto('/environments')
    const card = page.locator(`[data-testid="environment-card"][data-run-id="${runId}"]`)
    await expect(card).toBeVisible({ timeout: 90_000 })
    await expect(card).toContainText('ui-slow')
    await expect(card).toContainText('local.yaml')

    // The inventory agrees with Docker.
    await expect.poll(() => docker.containers(runId).length, { timeout: 60_000 }).toBeGreaterThan(0)

    await card.click()
    await page.getByRole('button', { name: 'Nuke' }).first().click()
    await page.getByRole('button', { name: 'Nuke' }).last().click()
    await expect(page.getByText('Environment reaped.')).toBeVisible({ timeout: 60_000 })

    await expect(card).toBeHidden({ timeout: 60_000 })
    await expect.poll(() => docker.containers(runId), { timeout: 60_000 }).toEqual([])
    await expect.poll(() => docker.networks(runId), { timeout: 60_000 }).toEqual([])
  })

  test('@docker the suite sidecar is listed without an orchestrator, so it is never a zombie', async ({ page, api }) => {
    const runId = await api.launch('ui-mock')
    await api.waitForRun(runId)

    const environments = await api.environments()
    const sidecar = environments.find((e) => e.runId.startsWith('sidecar-ui-mock'))
    expect(sidecar, 'the ui-mock sidecar should be listed').toBeDefined()
    expect(sidecar?.orchestratorPid ?? 0).toBe(0)
    expect(sidecar?.status).not.toBe('Zombie')

    await page.goto('/environments')
    await expect(page.locator('[data-testid="environment-card"][data-run-id^="sidecar-ui-mock"]')).toBeVisible()
  })

  test('/sandboxes redirects to /environments', async ({ page }) => {
    await page.goto('/sandboxes')
    await expect(page).toHaveURL(/\/environments$/)
  })
})
