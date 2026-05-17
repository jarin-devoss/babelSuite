import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import Registries from './Registries'

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
    syncRegistry: vi.fn(),
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

function renderRegistries() {
  mockGetSession.mockReturnValue({ token: 't', user: { userId: 'u1', workspaceId: 'w1', isAdmin: true } })
  return render(
    <MemoryRouter initialEntries={['/settings/registries']}>
      <Registries />
    </MemoryRouter>
  )
}

describe('Registries settings', () => {
  beforeEach(() => {
    mockNavigate.mockReset()
    mockGetPlatformSettings.mockReset()
  })

  it('renders OCI Registries heading', async () => {
    mockGetPlatformSettings.mockResolvedValue(mockSettings)
    renderRegistries()
    await waitFor(() => {
      expect(screen.getAllByRole('heading', { name: /oci registries/i }).length).toBeGreaterThan(0)
    })
  })

  it('renders Add Registry button', async () => {
    mockGetPlatformSettings.mockResolvedValue(mockSettings)
    renderRegistries()
    await waitFor(() => {
      expect(screen.getAllByRole('button', { name: /add registry/i }).length).toBeGreaterThan(0)
    })
  })

  it('shows error on load failure', async () => {
    mockGetPlatformSettings.mockRejectedValue(new Error('Failed'))
    renderRegistries()
    await waitFor(() => {
      expect(screen.getAllByText(/could not load/i).length).toBeGreaterThan(0)
    })
  })
})
