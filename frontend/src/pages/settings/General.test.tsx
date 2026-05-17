import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import General from './General'

const { mockNavigate, mockGetSession, mockGetPlatformSettings, mockUpdatePlatformSettings } = vi.hoisted(() => ({
  mockNavigate: vi.fn(),
  mockGetSession: vi.fn(),
  mockGetPlatformSettings: vi.fn(),
  mockUpdatePlatformSettings: vi.fn(),
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
    updatePlatformSettings: mockUpdatePlatformSettings,
    clearSession: vi.fn(),
  }
})

vi.mock('../../lib/telemetry', () => ({ recordUnhandledError: vi.fn() }))

const mockSettings = {
  mode: 'local',
  description: 'Test instance',
  agents: [],
  registries: [],
  secrets: { provider: 'none', globalOverrides: [] },
  updatedAt: '2024-01-01T00:00:00Z',
}

function renderGeneral() {
  mockGetSession.mockReturnValue({ token: 't', user: { userId: 'u1', workspaceId: 'w1', isAdmin: true } })
  return render(
    <MemoryRouter initialEntries={['/settings/general']}>
      <General />
    </MemoryRouter>
  )
}

describe('General settings', () => {
  beforeEach(() => {
    mockNavigate.mockReset()
    mockGetPlatformSettings.mockReset()
    mockUpdatePlatformSettings.mockReset()
  })

  it('renders General heading', async () => {
    mockGetPlatformSettings.mockResolvedValue(mockSettings)
    renderGeneral()
    await waitFor(() => {
      expect(screen.getByRole('heading', { name: /general/i })).toBeInTheDocument()
    })
  })

  it('renders mode selection after load', async () => {
    mockGetPlatformSettings.mockResolvedValue(mockSettings)
    renderGeneral()
    await waitFor(() => {
      expect(screen.getByText('Local')).toBeInTheDocument()
    })
  })

  it('renders Save button', async () => {
    mockGetPlatformSettings.mockResolvedValue(mockSettings)
    renderGeneral()
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /save/i })).toBeInTheDocument()
    })
  })

  it('shows error message when load fails', async () => {
    mockGetPlatformSettings.mockRejectedValue(new Error('Load failed'))
    renderGeneral()
    await waitFor(() => {
      expect(screen.getByText(/could not load settings/i)).toBeInTheDocument()
    })
  })
})
