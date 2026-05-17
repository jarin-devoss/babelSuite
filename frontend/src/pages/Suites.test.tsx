import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import Suites from './Suites'

const { mockNavigate, mockGetSession, mockGetSuite, mockListExecutionLaunchSuites } = vi.hoisted(() => ({
  mockNavigate: vi.fn(),
  mockGetSession: vi.fn(),
  mockGetSuite: vi.fn(),
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
    getSuite: mockGetSuite,
    listExecutionLaunchSuites: mockListExecutionLaunchSuites,
    createExecution: vi.fn(),
    clearSession: vi.fn(),
  }
})

vi.mock('../lib/telemetry', () => ({ recordUnhandledError: vi.fn() }))

function renderSuites(path = '/suites') {
  mockGetSession.mockReturnValue({ token: 't', user: { userId: 'u1', workspaceId: 'w1', isAdmin: false } })
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path='/suites' element={<Suites />} />
        <Route path='/suites/:suiteId' element={<Suites />} />
      </Routes>
    </MemoryRouter>
  )
}

describe('Suites', () => {
  beforeEach(() => {
    mockNavigate.mockReset()
    mockGetSuite.mockReset()
    mockListExecutionLaunchSuites.mockReset()
  })

  it('shows notice when no suiteId in params', async () => {
    mockListExecutionLaunchSuites.mockResolvedValue([])
    renderSuites('/suites')
    await waitFor(() => {
      expect(screen.getByText(/suite not found/i)).toBeInTheDocument()
    })
  })

  it('loads suite data when suiteId param is present', async () => {
    mockListExecutionLaunchSuites.mockResolvedValue([])
    mockGetSuite.mockResolvedValue({
      id: 's1',
      title: 'My Suite',
      owner: 'acme',
      repository: 'acme/my-suite',
      version: 'v1.0.0',
      description: 'A test suite',
      status: 'Official',
      provider: 'docker.io',
      modules: [],
      tags: ['v1.0.0'],
      profiles: [],
      backends: [],
      pullCommand: 'bs pull acme/my-suite',
      forkCommand: 'bs fork acme/my-suite',
      suiteStar: 'def main(): pass',
      sourceFiles: [],
      folders: [],
      apiSurfaces: [],
      topology: [],
      score: 0,
      contracts: [],
    })
    mockGetSession.mockReturnValue({ token: 't', user: { userId: 'u1', workspaceId: 'w1', isAdmin: false } })
    render(
      <MemoryRouter initialEntries={['/suites/s1']}>
        <Routes>
          <Route path='/suites/:suiteId' element={<Suites />} />
        </Routes>
      </MemoryRouter>
    )
    await waitFor(() => {
      expect(screen.getAllByText('My Suite').length).toBeGreaterThan(0)
    })
  })

  it('shows error when suite load fails', async () => {
    mockListExecutionLaunchSuites.mockResolvedValue([])
    mockGetSuite.mockRejectedValue(new Error('Not found'))
    mockGetSession.mockReturnValue({ token: 't', user: { userId: 'u1', workspaceId: 'w1', isAdmin: false } })
    render(
      <MemoryRouter initialEntries={['/suites/bad-id']}>
        <Routes>
          <Route path='/suites/:suiteId' element={<Suites />} />
        </Routes>
      </MemoryRouter>
    )
    await waitFor(() => {
      expect(screen.getByText(/not found/i)).toBeInTheDocument()
    })
  })
})
