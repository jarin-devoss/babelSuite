import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import Settings from './Settings'

const { mockNavigate, mockGetSession, mockGetPlatformSettings } = vi.hoisted(() => ({
  mockNavigate: vi.fn(),
  mockGetSession: vi.fn(),
  mockGetPlatformSettings: vi.fn(),
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
    getPlatformSettings: mockGetPlatformSettings,
    clearSession: vi.fn(),
  }
})

vi.mock('../lib/telemetry', () => ({ recordUnhandledError: vi.fn() }))

const mockPlatform = {
  mode: 'local',
  description: 'Test instance',
  agents: [],
  registries: [],
  secrets: { provider: 'none', globalOverrides: [] },
  updatedAt: '2024-01-01T00:00:00Z',
}

function renderSettings() {
  mockGetSession.mockReturnValue({ token: 't', user: { userId: 'u1', workspaceId: 'w1', isAdmin: true } })
  return render(
    <MemoryRouter initialEntries={['/settings']}>
      <Settings />
    </MemoryRouter>
  )
}

describe('Settings', () => {
  beforeEach(() => {
    mockNavigate.mockReset()
    mockGetPlatformSettings.mockReset()
  })

  it('renders settings section cards after load', async () => {
    mockGetPlatformSettings.mockResolvedValue(mockPlatform)
    renderSettings()
    await waitFor(() => {
      expect(screen.getByText('General')).toBeInTheDocument()
    })
    expect(screen.getByText('Execution Agents')).toBeInTheDocument()
    expect(screen.getByText('OCI Registries')).toBeInTheDocument()
  })

  it('shows platform mode in General description when loaded', async () => {
    mockGetPlatformSettings.mockResolvedValue(mockPlatform)
    renderSettings()
    await waitFor(() => {
      expect(screen.getAllByText(/local mode/i).length).toBeGreaterThan(0)
    })
  })

  it('shows error message when load fails', async () => {
    mockGetPlatformSettings.mockRejectedValue(new Error('Server error'))
    renderSettings()
    await waitFor(() => {
      expect(screen.getByText(/could not load platform settings/i)).toBeInTheDocument()
    })
  })
})
