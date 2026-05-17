import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import Profiles from './Profiles'

const { mockNavigate, mockGetSession, mockListProfileSuites, mockGetSuiteProfiles } = vi.hoisted(() => ({
  mockNavigate: vi.fn(),
  mockGetSession: vi.fn(),
  mockListProfileSuites: vi.fn(),
  mockGetSuiteProfiles: vi.fn(),
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
    listProfileSuites: mockListProfileSuites,
    getSuiteProfiles: mockGetSuiteProfiles,
    createSuiteProfile: vi.fn(),
    updateSuiteProfile: vi.fn(),
    deleteSuiteProfile: vi.fn(),
    setDefaultSuiteProfile: vi.fn(),
    clearSession: vi.fn(),
  }
})

vi.mock('../lib/telemetry', () => ({ recordUnhandledError: vi.fn() }))

function renderProfiles() {
  mockGetSession.mockReturnValue({ token: 't', user: { userId: 'u1', workspaceId: 'w1', isAdmin: false } })
  return render(
    <MemoryRouter initialEntries={['/profiles']}>
      <Profiles />
    </MemoryRouter>
  )
}

describe('Profiles', () => {
  beforeEach(() => {
    mockNavigate.mockReset()
    mockListProfileSuites.mockReset()
    mockGetSuiteProfiles.mockReset()
  })

  it('renders Profiles heading', async () => {
    mockListProfileSuites.mockResolvedValue([])
    renderProfiles()
    await waitFor(() => {
      expect(screen.getByRole('heading', { name: /profiles/i })).toBeInTheDocument()
    })
  })

  it('renders suite list items after load', async () => {
    mockListProfileSuites.mockResolvedValue([
      { id: 's1', title: 'API Suite', repository: 'org/api' },
    ])
    mockGetSuiteProfiles.mockResolvedValue({ suiteId: 's1', profiles: [] })
    renderProfiles()
    await waitFor(() => {
      expect(screen.getByText('API Suite')).toBeInTheDocument()
    })
  })

  it('renders search input', async () => {
    mockListProfileSuites.mockResolvedValue([])
    renderProfiles()
    await waitFor(() => {
      expect(screen.getByPlaceholderText(/search/i)).toBeInTheDocument()
    })
  })
})
