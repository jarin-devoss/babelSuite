import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import AuthCallback from './AuthCallback'

const { mockNavigate, mockResolveSessionFromToken, mockSaveSession } = vi.hoisted(() => ({
  mockNavigate: vi.fn(),
  mockResolveSessionFromToken: vi.fn(),
  mockSaveSession: vi.fn(),
}))

vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-router-dom')>()
  return { ...actual, useNavigate: () => mockNavigate }
})

vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>()
  return {
    ...actual,
    resolveSessionFromToken: mockResolveSessionFromToken,
    saveSession: mockSaveSession,
  }
})

vi.mock('../lib/telemetry', () => ({
  authEventCounter: { add: vi.fn() },
  setUserContext: vi.fn(),
  recordUnhandledError: vi.fn(),
}))

function renderCallback(search = '') {
  return render(
    <MemoryRouter initialEntries={[{ pathname: '/auth/callback', search }]}>
      <AuthCallback />
    </MemoryRouter>
  )
}

describe('AuthCallback', () => {
  beforeEach(() => {
    mockNavigate.mockReset()
    mockResolveSessionFromToken.mockReset()
    mockSaveSession.mockReset()
  })

  it('shows completing SSO message initially', () => {
    mockResolveSessionFromToken.mockReturnValue(new Promise(() => {}))
    renderCallback('?token=abc')
    expect(screen.getByText(/completing single sign-on/i)).toBeInTheDocument()
  })

  it('shows error when error param is present', async () => {
    renderCallback('?error=Access+denied')
    await waitFor(() => {
      expect(screen.getByText('Access denied')).toBeInTheDocument()
    })
  })

  it('shows error when no token in params', async () => {
    renderCallback('?foo=bar')
    await waitFor(() => {
      expect(screen.getByText(/did not include a session token/i)).toBeInTheDocument()
    })
  })

  it('navigates to / on successful token exchange', async () => {
    mockResolveSessionFromToken.mockResolvedValue({
      token: 'jwt',
      user: { userId: 'u1', workspaceId: 'w1', isAdmin: false },
    })
    renderCallback('?token=valid-token')
    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith('/', { replace: true })
    })
  })

  it('navigates to return_url when provided', async () => {
    mockResolveSessionFromToken.mockResolvedValue({
      token: 'jwt',
      user: { userId: 'u1', workspaceId: 'w1', isAdmin: false },
    })
    renderCallback('?token=t&return_url=%2Fcatalog')
    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith('/catalog', { replace: true })
    })
  })

  it('shows error when resolveSessionFromToken throws', async () => {
    mockResolveSessionFromToken.mockRejectedValue(new Error('network failure'))
    renderCallback('?token=bad')
    await waitFor(() => {
      expect(screen.getByText(/could not finish signing you in/i)).toBeInTheDocument()
    })
  })
})
