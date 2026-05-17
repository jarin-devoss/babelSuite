import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import Sandbox from './Sandbox'

const { mockNavigate, mockGetSession, mockListExecutionLaunchSuites, mockListCatalogPackages } = vi.hoisted(() => ({
  mockNavigate: vi.fn(),
  mockGetSession: vi.fn(),
  mockListExecutionLaunchSuites: vi.fn(),
  mockListCatalogPackages: vi.fn(),
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
    listExecutionLaunchSuites: mockListExecutionLaunchSuites,
    listCatalogPackages: mockListCatalogPackages,
    createExecution: vi.fn(),
    resolveExecutionRef: vi.fn(),
    clearSession: vi.fn(),
  }
})

vi.mock('../lib/telemetry', () => ({ recordUnhandledError: vi.fn() }))

function renderSandbox() {
  mockGetSession.mockReturnValue({ token: 't', user: { userId: 'u1', workspaceId: 'w1', isAdmin: false } })
  return render(
    <MemoryRouter initialEntries={['/sandbox']}>
      <Sandbox />
    </MemoryRouter>
  )
}

describe('Sandbox', () => {
  beforeEach(() => {
    mockNavigate.mockReset()
    mockListExecutionLaunchSuites.mockReset()
    mockListCatalogPackages.mockReset()
  })

  it('renders the Sandbox heading', async () => {
    mockListExecutionLaunchSuites.mockResolvedValue([])
    mockListCatalogPackages.mockResolvedValue([])
    renderSandbox()
    await waitFor(() => {
      expect(screen.getByRole('heading', { name: /drop & run/i })).toBeInTheDocument()
    })
  })

  it('shows drop zone area', async () => {
    mockListExecutionLaunchSuites.mockResolvedValue([])
    mockListCatalogPackages.mockResolvedValue([])
    renderSandbox()
    await waitFor(() => {
      expect(screen.getByText(/drop or click to select a suite folder/i)).toBeInTheDocument()
    })
  })
})
