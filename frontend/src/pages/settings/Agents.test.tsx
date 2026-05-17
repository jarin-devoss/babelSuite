import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import Agents from './Agents'

const { mockNavigate, mockGetSession, mockGetPlatformSettings } = vi.hoisted(() => ({
  mockNavigate: vi.fn(),
  mockGetSession: vi.fn(),
  mockGetPlatformSettings: vi.fn(),
}))

vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-router-dom')>()
  return { ...actual, useNavigate: () => mockNavigate }
})

vi.mock('../../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../lib/api')>()
  return {
    ...actual,
    getSession: mockGetSession,
    getPlatformSettings: mockGetPlatformSettings,
    updatePlatformSettings: vi.fn(),
    clearSession: vi.fn(),
  }
})

vi.mock('../../lib/connect', () => ({
  agentRegistryClient: { listAgents: vi.fn().mockResolvedValue({ registrations: [] }) },
}))

vi.mock('../../lib/telemetry', () => ({ recordUnhandledError: vi.fn() }))

const mockSettings = {
  mode: 'local',
  description: 'Test',
  agents: [],
  registries: [],
  secrets: { provider: 'none', globalOverrides: [] },
  updatedAt: '2024-01-01T00:00:00Z',
}

function renderAgents() {
  mockGetSession.mockReturnValue({ token: 't', user: { userId: 'u1', workspaceId: 'w1', isAdmin: true } })
  return render(
    <MemoryRouter initialEntries={['/settings/agents']}>
      <Agents />
    </MemoryRouter>
  )
}

describe('Agents settings', () => {
  beforeEach(() => {
    mockNavigate.mockReset()
    mockGetPlatformSettings.mockReset()
  })

  it('renders Execution Agents heading', async () => {
    mockGetPlatformSettings.mockResolvedValue(mockSettings)
    renderAgents()
    await waitFor(() => {
      expect(screen.getAllByRole('heading', { name: /execution agents/i }).length).toBeGreaterThan(0)
    })
  })

  it('renders Add Agent button', async () => {
    mockGetPlatformSettings.mockResolvedValue(mockSettings)
    renderAgents()
    await waitFor(() => {
      expect(screen.getAllByRole('button', { name: /add agent/i }).length).toBeGreaterThan(0)
    })
  })

  it('shows error on load failure', async () => {
    mockGetPlatformSettings.mockRejectedValue(new Error('Failed'))
    renderAgents()
    await waitFor(() => {
      expect(screen.getAllByText(/could not load/i).length).toBeGreaterThan(0)
    })
  })
})
