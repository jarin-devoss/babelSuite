import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import SignIn from './SignIn'

const mockNavigate = vi.fn()

vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-router-dom')>()
  return { ...actual, useNavigate: () => mockNavigate }
})

vi.mock('../lib/telemetry', () => ({
  authEventCounter: { add: vi.fn() },
  setUserContext: vi.fn(),
  useRouteTracking: vi.fn(),
}))

const mockSignIn = vi.fn()
const mockGetAuthConfig = vi.fn()

vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>()
  return {
    ...actual,
    signIn: (...args: Parameters<typeof actual.signIn>) => mockSignIn(...args),
    getAuthConfig: () => mockGetAuthConfig(),
    saveSession: vi.fn(),
  }
})

function renderSignIn(search = '') {
  return render(
    <MemoryRouter initialEntries={[`/sign-in${search}`]}>
      <SignIn />
    </MemoryRouter>
  )
}

describe('SignIn', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockGetAuthConfig.mockResolvedValue({ passwordAuthEnabled: true, signUpEnabled: true, providers: [] })
  })

  it('renders email and password inputs', () => {
    renderSignIn()
    expect(screen.getByLabelText('Email Address')).toBeInTheDocument()
    expect(screen.getByLabelText('Password')).toBeInTheDocument()
  })

  it('renders a sign-in submit button', () => {
    renderSignIn()
    expect(screen.getByRole('button', { name: 'Sign In' })).toBeInTheDocument()
  })

  it('renders a forgot password link', () => {
    renderSignIn()
    expect(screen.getByRole('link', { name: 'Forgot your password?' })).toBeInTheDocument()
  })

  it('shows an error message on sign-in failure', async () => {
    const user = userEvent.setup()
    mockSignIn.mockRejectedValueOnce({ message: 'Incorrect email or password.', status: 401 })
    renderSignIn()

    await user.type(screen.getByLabelText('Email Address'), 'wrong@example.com')
    await user.type(screen.getByLabelText('Password'), 'badpass')
    await user.click(screen.getByRole('button', { name: 'Sign In' }))

    await waitFor(() => {
      expect(screen.getByText('Cannot reach the authentication service right now.')).toBeInTheDocument()
    })
  })

  it('navigates to / on successful sign-in', async () => {
    const user = userEvent.setup()
    mockSignIn.mockResolvedValueOnce({
      token: 'tok',
      user: { userId: 'u1', workspaceId: 'w1', username: 'alice', email: 'alice@example.com', fullName: 'Alice', isAdmin: false, createdAt: '' },
      workspace: { workspaceId: 'w1', slug: 'alice', name: "Alice's workspace", createdAt: '' },
      expiresAt: '',
    })
    renderSignIn()

    await user.type(screen.getByLabelText('Email Address'), 'alice@example.com')
    await user.type(screen.getByLabelText('Password'), 'Password1')
    await user.click(screen.getByRole('button', { name: 'Sign In' }))

    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith('/')
    })
  })

  it('navigates to returnTo path when provided', async () => {
    const user = userEvent.setup()
    mockSignIn.mockResolvedValueOnce({
      token: 'tok',
      user: { userId: 'u1', workspaceId: 'w1', username: 'alice', email: 'alice@example.com', fullName: 'Alice', isAdmin: false, createdAt: '' },
      workspace: { workspaceId: 'w1', slug: 'alice', name: "Alice's workspace", createdAt: '' },
      expiresAt: '',
    })
    renderSignIn('?returnTo=%2Fcatalog')

    await user.type(screen.getByLabelText('Email Address'), 'alice@example.com')
    await user.type(screen.getByLabelText('Password'), 'Password1')
    await user.click(screen.getByRole('button', { name: 'Sign In' }))

    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith('/catalog')
    })
  })

  it('disables the submit button while loading', async () => {
    const user = userEvent.setup()
    let resolveSignIn!: (value: unknown) => void
    mockSignIn.mockReturnValueOnce(new Promise((res) => { resolveSignIn = res }))
    renderSignIn()

    await user.type(screen.getByLabelText('Email Address'), 'alice@example.com')
    await user.type(screen.getByLabelText('Password'), 'Password1')
    await user.click(screen.getByRole('button', { name: 'Sign In' }))

    expect(screen.getByRole('button', { name: 'Signing in...' })).toBeDisabled()
    resolveSignIn({ token: '', user: {}, workspace: {}, expiresAt: '' })
  })
})
