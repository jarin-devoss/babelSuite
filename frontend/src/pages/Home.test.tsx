import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import Home from './Home'

const { mockNavigate, mockGetSession, mockGetExecutionOverview, mockListExecutionLaunchSuites } = vi.hoisted(() => ({
  mockNavigate: vi.fn(),
  mockGetSession: vi.fn(),
  mockGetExecutionOverview: vi.fn(),
  mockListExecutionLaunchSuites: vi.fn(),
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
    getSuite: vi.fn(),
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
