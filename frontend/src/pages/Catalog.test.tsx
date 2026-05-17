import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import Catalog from './Catalog'

const { mockNavigate, mockGetSession, mockListCatalogPackages } = vi.hoisted(() => ({
  mockNavigate: vi.fn(),
  mockGetSession: vi.fn(),
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
    listCatalogPackages: mockListCatalogPackages,
    addCatalogFavorite: vi.fn(),
    removeCatalogFavorite: vi.fn(),
    getSuite: vi.fn(),
    clearSession: vi.fn(),
  }
})

vi.mock('../lib/telemetry', () => ({ recordUnhandledError: vi.fn() }))

const mockSession = { token: 'tok', user: { userId: 'u1', workspaceId: 'w1', isAdmin: false, favorites: [] } }

function renderCatalog() {
  return render(
    <MemoryRouter initialEntries={['/catalog']}>
      <Catalog />
    </MemoryRouter>
  )
}

describe('Catalog', () => {
  beforeEach(() => {
    mockNavigate.mockReset()
    mockGetSession.mockReturnValue(mockSession)
  })

  it('renders Catalog heading', async () => {
    mockListCatalogPackages.mockResolvedValue([])
    renderCatalog()
    await waitFor(() => {
      expect(screen.getByRole('heading', { name: /catalog/i })).toBeInTheDocument()
    })
  })

  it('renders package cards when data loads', async () => {
    mockListCatalogPackages.mockResolvedValue([{
      id: 'pkg-1',
      title: 'Awesome Suite',
      owner: 'acme',
      repository: 'acme/awesome',
      version: 'v1.0.0',
      description: 'Test suite',
      provider: 'docker.io',
      kind: 'suite',
      status: 'Official',
      modules: [],
      tags: ['v1.0.0'],
      pullCommand: 'bs pull acme/awesome',
      forkCommand: 'bs fork acme/awesome',
    }])
    renderCatalog()
    await waitFor(() => {
      expect(screen.getByText('Awesome Suite')).toBeInTheDocument()
    })
  })

  it('shows search input', async () => {
    mockListCatalogPackages.mockResolvedValue([])
    renderCatalog()
    await waitFor(() => {
      expect(screen.getByPlaceholderText(/search/i)).toBeInTheDocument()
    })
  })
})
