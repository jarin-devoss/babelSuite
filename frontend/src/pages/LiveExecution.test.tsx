import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import LiveExecution from './LiveExecution'

const { mockNavigate, mockGetSession, mockUseExecutionStream } = vi.hoisted(() => ({
  mockNavigate: vi.fn(),
  mockGetSession: vi.fn(),
  mockUseExecutionStream: vi.fn(),
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
    createExecution: vi.fn(),
    clearSession: vi.fn(),
  }
})

vi.mock('../hooks/useExecutionStream', () => ({
  useExecutionStream: () => mockUseExecutionStream(),
}))

vi.mock('../lib/telemetry', () => ({ recordUnhandledError: vi.fn() }))

const defaultStream = {
  execution: null,
  logs: [],
  loading: true,
  error: '',
  paused: false,
  setPaused: vi.fn(),
  refresh: vi.fn(),
  executionStreamState: 'connecting' as const,
  logStreamState: 'connecting' as const,
}

function renderLiveExecution() {
  mockGetSession.mockReturnValue({ token: 't', user: { userId: 'u1', workspaceId: 'w1', isAdmin: false } })
  return render(
    <MemoryRouter initialEntries={['/executions/exec-1']}>
      <LiveExecution />
    </MemoryRouter>
  )
}

describe('LiveExecution', () => {
  beforeEach(() => {
    mockNavigate.mockReset()
    mockUseExecutionStream.mockReturnValue(defaultStream)
  })

  it('renders loading heading when execution is null', () => {
    renderLiveExecution()
    expect(screen.getByRole('heading', { name: /loading execution/i })).toBeInTheDocument()
  })

  it('shows connecting to stream message while loading', () => {
    renderLiveExecution()
    expect(screen.getByText(/connecting to execution stream/i)).toBeInTheDocument()
  })

  it('shows error text when stream has error', () => {
    mockUseExecutionStream.mockReturnValue({
      ...defaultStream,
      loading: false,
      error: 'Execution not found',
    })
    renderLiveExecution()
    expect(screen.getByText('Execution not found')).toBeInTheDocument()
  })
})
