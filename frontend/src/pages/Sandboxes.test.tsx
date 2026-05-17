import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import Sandboxes from './Sandboxes'

const { mockNavigate, mockGetSession, mockUseSandboxStream } = vi.hoisted(() => ({
  mockNavigate: vi.fn(),
  mockGetSession: vi.fn(),
  mockUseSandboxStream: vi.fn(),
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
    reapAllSandboxes: vi.fn(),
    reapSandbox: vi.fn(),
    clearSession: vi.fn(),
  }
})

vi.mock('../hooks/useSandboxStream', () => ({
  useSandboxStream: () => mockUseSandboxStream(),
}))

vi.mock('../lib/telemetry', () => ({ recordUnhandledError: vi.fn() }))

function renderSandboxes() {
  mockGetSession.mockReturnValue({ token: 't', user: { userId: 'u1', workspaceId: 'w1', isAdmin: false } })
  return render(
    <MemoryRouter initialEntries={['/environments']}>
      <Sandboxes />
    </MemoryRouter>
  )
}

describe('Sandboxes', () => {
  beforeEach(() => {
    mockNavigate.mockReset()
    mockUseSandboxStream.mockReturnValue({
      snapshot: null,
      loading: true,
      refreshing: false,
      error: '',
      streamState: 'connecting',
      refresh: vi.fn(),
    })
  })

  it('renders Environments heading', () => {
    renderSandboxes()
    expect(screen.getByRole('heading', { name: /environments/i })).toBeInTheDocument()
  })

  it('shows empty state when no sandboxes', () => {
    mockUseSandboxStream.mockReturnValue({
      snapshot: {
        dockerAvailable: true,
        updatedAt: new Date().toISOString(),
        summary: { activeSandboxes: 0, zombieSandboxes: 0, containers: 0, networks: 0, volumes: 0, totalCpuPercent: 0, totalMemoryBytes: 0 },
        sandboxes: [],
        warnings: [],
      },
      loading: false,
      refreshing: false,
      error: '',
      streamState: 'connected',
      refresh: vi.fn(),
    })
    renderSandboxes()
    expect(screen.getByText(/no managed resources yet/i)).toBeInTheDocument()
  })

  it('shows error message when stream errors', () => {
    mockUseSandboxStream.mockReturnValue({
      snapshot: null,
      loading: false,
      refreshing: false,
      error: 'Connection failed',
      streamState: 'disconnected',
      refresh: vi.fn(),
    })
    renderSandboxes()
    expect(screen.getByText(/connection failed/i)).toBeInTheDocument()
  })
})
