import { stepStatuses } from '../../api/client'
import { test, expect } from '../../fixtures'
import { ExecutionPage } from '../../pages/ExecutionPage'

test.describe('Run results', () => {
  test('@docker a failing step fails the run and is shown as failed', async ({ page, api }) => {
    const runId = await api.launch('ui-fail')
    const record = await api.waitForRun(runId)
    expect(record.status).toBe('Failed')

    const live = new ExecutionPage(page)
    await live.goto(runId)
    await expect(live.status()).toHaveAttribute('data-status', 'failed')
    await expect(live.node('prep')).toHaveAttribute('data-status', 'healthy')
    await expect(live.node('boom')).toHaveAttribute('data-status', 'failed')
    await expect(live.stat('failed')).toHaveText('1')

    const steps = stepStatuses(record)
    expect(steps.prep).toBe('healthy')
    expect(steps.boom).toBe('failed')

    await live.node('boom').click()
    await expect(live.logLines().filter({ hasText: 'UI-FAIL about to fail' })).toHaveCount(1)
  })

  test('@docker continue_on_failure keeps the run healthy but still shows the step as failed', async ({ page, api }) => {
    const runId = await api.launch('ui-continue')
    const record = await api.waitForRun(runId)
    expect(stepStatuses(record).bad).toBe('failed')

    const live = new ExecutionPage(page)
    await live.goto(runId)
    // A failed step must never look green, whatever the run's own verdict is.
    await expect(live.node('bad')).toHaveAttribute('data-status', 'failed')
    await expect(live.stat('failed')).toHaveText('1')
    await expect(live.logLines().filter({ hasText: 'UI-CONTINUE the run went on' }).first()).toBeVisible()
  })

  test('@docker a JUnit export shows its real counts, and a report with failures fails the step', async ({ page, api }) => {
    const runId = await api.launch('ui-junit')
    const record = await api.waitForRun(runId)
    // The step exits 0, but its own report says 2 of 5 tests failed.
    expect(record.status).toBe('Failed')
    expect(stepStatuses(record).report).toBe('failed')
    const artifact = record.artifacts?.find((a) => a.name === 'go-tests')
    expect(artifact?.status).toBe('collected')
    expect(artifact?.testSummary).toMatchObject({ total: 5, failures: 2 })

    const live = new ExecutionPage(page)
    await live.goto(runId)
    await expect(live.node('report')).toHaveAttribute('data-status', 'failed')
    await live.artifactsButton().click()
    await expect(page.getByTestId('artifact-total')).toHaveText('5')
    await expect(page.getByTestId('artifact-passed')).toHaveText('3')
    await expect(page.getByTestId('artifact-failures')).toHaveText('2')
    await expect(page.getByTestId('artifact-verdict')).toContainText('2 tests did not pass')
  })

  test('@docker a JUnit export with no file is not reported as a passing test', async ({ page, api }) => {
    const runId = await api.launch('ui-junit-missing')
    const record = await api.waitForRun(runId)
    expect(record.status).toBe('Failed')
    expect(stepStatuses(record).report).toBe('failed')
    const artifact = record.artifacts?.find((a) => a.name === 'go-tests')
    expect(artifact?.status).toBe('missing')
    expect(artifact?.testSummary).toBeUndefined()

    const live = new ExecutionPage(page)
    await live.goto(runId)
    await live.artifactsButton().click()
    await expect(page.getByTestId('artifact-status')).toContainText('Report not produced')
    await expect(page.getByTestId('artifact-passed')).toHaveCount(0)
  })

  test('@docker a JUnit report that lists no tests fails the step', async ({ page, api }) => {
    const runId = await api.launch('ui-junit-empty')
    const record = await api.waitForRun(runId)
    expect(record.status).toBe('Failed')
    const artifact = record.artifacts?.find((a) => a.name === 'go-tests')
    expect(artifact?.status).toBe('collected')
    expect(artifact?.testSummary).toMatchObject({ total: 0 })

    const live = new ExecutionPage(page)
    await live.goto(runId)
    await live.artifactsButton().click()
    await expect(page.getByTestId('artifact-verdict')).toContainText('No tests in this report')
  })

  test('@docker a JUnit file that is not XML is unreadable, and the step fails', async ({ page, api }) => {
    const runId = await api.launch('ui-junit-invalid')
    const record = await api.waitForRun(runId)
    expect(record.status).toBe('Failed')
    const artifact = record.artifacts?.find((a) => a.name === 'go-tests')
    expect(artifact?.status).toBe('invalid')
    expect(artifact?.testSummary).toBeUndefined()

    const live = new ExecutionPage(page)
    await live.goto(runId)
    await live.artifactsButton().click()
    await expect(page.getByTestId('artifact-status')).toContainText('Report could not be read')
    await expect(page.getByTestId('artifact-problem')).toBeVisible()
    // The file as written stays visible, so the author can see what is wrong.
    await expect(page.getByRole('dialog', { name: 'Artifacts' })).toContainText('this is not xml at all')
  })

  test('@docker a missing report that is not enforced warns and does not fail the step', async ({ page, api }) => {
    const runId = await api.launch('ui-junit-soft')
    const record = await api.waitForRun(runId)
    expect(record.status).toBe('Healthy')
    expect(stepStatuses(record).report).toBe('healthy')
    const artifact = record.artifacts?.find((a) => a.name === 'go-tests')
    expect(artifact?.status).toBe('missing')

    const live = new ExecutionPage(page)
    await live.goto(runId)
    await live.artifactsButton().click()
    await expect(page.getByTestId('artifact-status')).toContainText('Report not produced')
  })

  test('@docker a CTRF report shows its summary, and its failure fails the step', async ({ page, api }) => {
    const runId = await api.launch('ui-ctrf')
    const record = await api.waitForRun(runId)
    expect(record.status).toBe('Failed')
    const artifact = record.artifacts?.find((a) => a.name === 'ctrf-tests')
    expect(artifact?.testSummary).toMatchObject({ total: 3, passed: 2, failures: 1 })

    const live = new ExecutionPage(page)
    await live.goto(runId)
    await live.artifactsButton().click()
    await expect(page.getByTestId('artifact-total')).toHaveText('3')
    await expect(page.getByTestId('artifact-failures')).toHaveText('1')
    await expect(page.getByTestId('artifact-verdict')).toContainText('1 test did not pass')
  })
})

test.describe('Mock surfaces', () => {
  test('@docker the live page lists the suite mock and its exchange', async ({ page, api }) => {
    const runId = await api.launch('ui-mock')
    await api.waitForRun(runId)

    const live = new ExecutionPage(page)
    await live.goto(runId)
    await expect(live.node('ui-api')).toHaveAttribute('data-status', 'healthy')
    await live.node('ui-api').click()
    await live.mockButton().click()
    await expect(page.getByRole('dialog')).toContainText('hello from ui-mock')
  })

  test('@docker the sidecar answers the mock from the exchange the suite declares', async ({ api, docker, playwright }) => {
    const runId = await api.launch('ui-mock')
    await api.waitForRun(runId)

    const sidecar = docker.sidecarUrl('ui-mock')
    expect(sidecar, 'the ui-mock sidecar should be published on a host port').not.toBe('')
    const client = await playwright.request.newContext({ baseURL: sidecar })
    const response = await client.get('/greeting?scenario=hello', {
      headers: { Host: 'ui-mock.mock.internal', Accept: 'application/json' },
    })
    expect(response.status()).toBe(200)
    expect(response.headers()['x-mock-source']).toBe('ui-mock')
    expect(await response.json()).toEqual({ message: 'hello from ui-mock' })
    await client.dispose()
  })

  test('@docker a step reaches the mock through BABELSUITE_GATEWAY_URL', async ({ api }) => {
    // The variable used to hold the sidecar's host-published address
    // (http://127.0.0.1:<port>), which inside a step container is the container
    // itself. The sidecar now joins the run's network and the step is given its
    // address there.
    const runId = await api.launch('ui-mock')
    const record = await api.waitForRun(runId)
    expect(record.status).toBe('Healthy')
    expect((await api.logs(runId)).some((line) => line.includes('UI-MOCK PASS'))).toBe(true)
  })
})

test.describe('Runs that cannot start', () => {
  test('@docker a Starlark error fails the run with the error in the log', async ({ page, api }) => {
    const runId = await api.launch('ui-broken')
    const record = await api.waitForRun(runId)
    expect(record.status).toBe('Failed')

    const live = new ExecutionPage(page)
    await live.goto(runId)
    await expect(live.status()).toHaveAttribute('data-status', 'failed')
    await expect(live.logLines().filter({ hasText: 'undefined: no_such_node' })).toHaveCount(1)
  })
})
