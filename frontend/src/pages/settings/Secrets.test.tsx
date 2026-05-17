import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import Secrets from './Secrets'

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

vi.mock('../../lib/telemetry', () => ({ recordUnhandledError: vi.fn() }))

const mockSettings = {
  mode: 'local',
  description: 'Test',
  agents: [],
  registries: [],
  secrets: { provider: 'none', globalOverrides: [] },
  updatedAt: '2024-01-01T00:00:00Z',
}

function renderSecrets() {
  mockGetSession.mockReturnValue({ token: 't', user: { userId: 'u1', workspaceId: 'w1', isAdmin: true } })
  return render(
    <MemoryRouter initialEntries={['/settings/secrets']}>
      <Secrets />
    </MemoryRouter>
  )
}

describe('Secrets settings', () => {
  beforeEach(() => {
    mockNavigate.mockReset()
    mockGetPlatformSettings.mockReset()
  })

  it('renders Secrets heading', async () => {
    mockGetPlatformSettings.mockResolvedValue(mockSettings)
    renderSecrets()
    await waitFor(() => {
      expect(screen.getByRole('heading', { name: /secrets/i })).toBeInTheDocument()
    })
  })

  it('renders provider selection after load', async () => {
    mockGetPlatformSettings.mockResolvedValue(mockSettings)
    renderSecrets()
    await waitFor(() => {
      expect(screen.getByText(/no external manager/i)).toBeInTheDocument()
    })
  })

  it('shows error on load failure', async () => {
    mockGetPlatformSettings.mockRejectedValue(new Error('Failed'))
    renderSecrets()
    await waitFor(() => {
      expect(screen.getAllByText(/could not load/i).length).toBeGreaterThan(0)
    })
  })
})
