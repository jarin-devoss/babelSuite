import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import Home from './Home'

const { mockNavigate, mockGetSession, mockGetExecutionOverview, mockListExecutionLaunchSuites, mockGetSuite } = vi.hoisted(() => ({
  mockNavigate: vi.fn(),
  mockGetSession: vi.fn(),
  mockGetExecutionOverview: vi.fn(),
  mockListExecutionLaunchSuites: vi.fn(),
  mockGetSuite: vi.fn(),
}))

vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-router-dom')>()
  return { ...actual, useNavigate: () => mockNavigate }
})

vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>()
  return {
    ...actual,
    getSession: mockGetSession,
    getExecutionOverview: mockGetExecutionOverview,
    listExecutionLaunchSuites: mockListExecutionLaunchSuites,
    createExecution: vi.fn(),
    getSuite: mockGetSuite,
    clearSession: vi.fn(),
  }
})

vi.mock('../lib/telemetry', () => ({
  authEventCounter: { add: vi.fn() },
  setUserContext: vi.fn(),
  recordUnhandledError: vi.fn(),
}))

const mockSession = { token: 'tok', user: { userId: 'u1', workspaceId: 'w1', isAdmin: false } }

const emptyOverview = {
  executions: [],
  summary: { totalExecutions: 0, bootingExecutions: 0, healthyExecutions: 0, failedExecutions: 0 },
}

function renderHome() {
  return render(
    <MemoryRouter initialEntries={['/']}>
      <Home />
    </MemoryRouter>
  )
}

describe('Home', () => {
  beforeEach(() => {
    mockNavigate.mockReset()
    mockGetSession.mockReturnValue(mockSession)
    mockGetExecutionOverview.mockResolvedValue(emptyOverview)
    mockListExecutionLaunchSuites.mockResolvedValue([])
    mockGetSuite.mockReset()
  })

  it('renders the recent executions heading', async () => {
    renderHome()
    await waitFor(() => {
      expect(screen.getByText('Recent executions')).toBeInTheDocument()
    })
  })

  it('shows empty state when no executions', async () => {
    renderHome()
    await waitFor(() => {
      expect(screen.getByText(/no executions yet/i)).toBeInTheDocument()
    })
  })

  it('renders execution rows when executions exist', async () => {
    mockGetExecutionOverview.mockResolvedValue({
      ...emptyOverview,
      executions: [{
        id: 'exec-1',
        suiteId: 's1',
        suiteTitle: 'My Suite',
        profile: 'prod.yaml',
        backendId: 'local',
        backend: 'local',
        trigger: 'manual',
        status: 'Healthy',
        duration: '2s',
        startedAt: new Date().toISOString(),
        updatedAt: new Date().toISOString(),
        totalSteps: 3,
        runningSteps: 0,
        healthySteps: 3,
        failedSteps: 0,
        skippedSteps: 0,
        pendingSteps: 0,
        progressRatio: 1,
        steps: [],
      }],
    })
    renderHome()
    await waitFor(() => {
      expect(screen.getByText('My Suite')).toBeInTheDocument()
    })
  })

  it('returns null when no session', () => {
    mockGetSession.mockReturnValue(null)
    const { container } = renderHome()
    expect(container.firstChild).toBeNull()
  })

  it('renders New Execution button', async () => {
    renderHome()
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /new execution/i })).toBeInTheDocument()
    })
  })
})

describe('Home — suite inspect modal', () => {
  const run = {
    id: 'run-1',
    suiteId: 'gateway-check',
    suiteTitle: 'Gateway Check',
    profile: 'local.yaml',
    status: 'Healthy',
    duration: '00:02',
    startedAt: new Date().toISOString(),
    trigger: 'Manual',
  }

  // suite.star is a root source file as well as the package entrypoint, which
  // is what used to list it twice.
  const suite = {
    id: 'gateway-check',
    title: 'Gateway Check',
    owner: 'QA',
    repository: 'localhost:5000/qa/gateway-check',
    version: 'v1.0.0',
    tags: [],
    description: '',
    modules: [],
    status: 'Verified',
    score: 0,
    pullCommand: '',
    forkCommand: '',
    suiteStar: 'load("@babelsuite/runtime", "service")\n',
    profiles: [],
    folders: [],
    sourceFiles: [
      { path: 'suite.star', language: 'starlark', content: 'load("@babelsuite/runtime", "service")\n' },
      { path: 'README.md', language: 'markdown', content: '# Gateway Check\n' },
      { path: 'metadata.yaml', language: 'yaml', content: 'kind: OCIExampleSuite\n' },
    ],
    topology: [],
    contracts: [],
    apiSurfaces: [],
  }

  async function openInspect() {
    mockGetExecutionOverview.mockResolvedValue({
      executions: [run],
      summary: { totalExecutions: 1, bootingExecutions: 0, healthyExecutions: 1, failedExecutions: 0 },
    })
    mockGetSuite.mockResolvedValue(suite)
    const user = userEvent.setup()
    renderHome()
    await waitFor(() => expect(screen.getByTitle('Inspect suite')).toBeInTheDocument())
    await user.click(screen.getByTitle('Inspect suite'))
    await waitFor(() => expect(mockGetSuite).toHaveBeenCalledWith('gateway-check'))
    return user
  }

  it('lists suite.star exactly once in the package tree', async () => {
    await openInspect()
    await waitFor(() => {
      const entries = Array.from(document.querySelectorAll('.ci-tree__item'))
        .map((el) => el.textContent?.trim())
        .filter((text) => text === 'suite.star')
      expect(entries).toHaveLength(1)
    })
  })

  it('still lists the other root files alongside it', async () => {
    await openInspect()
    await waitFor(() => {
      const entries = Array.from(document.querySelectorAll('.ci-tree__item')).map((el) => el.textContent?.trim())
      expect(entries).toContain('suite.star')
      expect(entries).toContain('README.md')
      expect(entries).toContain('metadata.yaml')
    })
  })

  it('renders the suite.star body when it is selected', async () => {
    await openInspect()
    await waitFor(() => {
      expect(screen.getByText(/@babelsuite\/runtime/)).toBeInTheDocument()
    })
  })
})
