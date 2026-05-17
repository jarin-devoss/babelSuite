import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import SignUp from './Signup'

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

const mockSignUp = vi.fn()
const mockGetAuthConfig = vi.fn()

vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>()
  return {
    ...actual,
    signUp: (...args: Parameters<typeof actual.signUp>) => mockSignUp(...args),
    getAuthConfig: () => mockGetAuthConfig(),
    saveSession: vi.fn(),
  }
})

function renderSignUp() {
  return render(
    <MemoryRouter initialEntries={['/sign-up']}>
      <SignUp />
    </MemoryRouter>
  )
}

describe('SignUp', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockGetAuthConfig.mockResolvedValue({ passwordAuthEnabled: true, signUpEnabled: true, providers: [] })
  })

  it('renders name, email, and password inputs', () => {
    renderSignUp()
    expect(screen.getByLabelText('Full Name')).toBeInTheDocument()
    expect(screen.getByLabelText('Email Address')).toBeInTheDocument()
    expect(screen.getByLabelText('Password')).toBeInTheDocument()
  })

  it('renders a create account submit button', () => {
    renderSignUp()
    expect(screen.getByRole('button', { name: 'Create Account' })).toBeInTheDocument()
  })

  it('shows password strength label as user types', async () => {
    const user = userEvent.setup()
    renderSignUp()

    await user.type(screen.getByLabelText('Password'), 'MyLongPassword1!')
    expect(screen.getByText('Excellent')).toBeInTheDocument()
  })

  it('shows Needs work label for a weak password', async () => {
    const user = userEvent.setup()
    renderSignUp()

    await user.type(screen.getByLabelText('Password'), 'abc')
    expect(screen.getByText('Needs work')).toBeInTheDocument()
  })

  it('toggles password visibility', async () => {
    const user = userEvent.setup()
    renderSignUp()

    const passwordInput = screen.getByLabelText('Password')
    expect(passwordInput).toHaveAttribute('type', 'password')

    await user.click(screen.getByRole('button', { name: 'Show password' }))
    expect(passwordInput).toHaveAttribute('type', 'text')

    await user.click(screen.getByRole('button', { name: 'Hide password' }))
    expect(passwordInput).toHaveAttribute('type', 'password')
  })

  it('shows an error message on sign-up failure', async () => {
    const user = userEvent.setup()
    mockSignUp.mockRejectedValueOnce({ message: 'An account already exists for that email address.', status: 409 })
    renderSignUp()

    await user.type(screen.getByLabelText('Full Name'), 'Alice')
    await user.type(screen.getByLabelText('Email Address'), 'alice@example.com')
    await user.type(screen.getByLabelText('Password'), 'Password1')
    await user.click(screen.getByRole('button', { name: 'Create Account' }))

    await waitFor(() => {
      expect(screen.getByText('Cannot reach the authentication service right now.')).toBeInTheDocument()
    })
  })

  it('navigates to / after successful sign-up', async () => {
    const user = userEvent.setup()
    mockSignUp.mockResolvedValueOnce({
      token: 'tok',
      user: { userId: 'u1', workspaceId: 'w1', username: 'alice', email: 'alice@example.com', fullName: 'Alice', isAdmin: false, createdAt: '' },
      workspace: { workspaceId: 'w1', slug: 'alice', name: "Alice's workspace", createdAt: '' },
      expiresAt: '',
    })
    renderSignUp()

    await user.type(screen.getByLabelText('Full Name'), 'Alice')
    await user.type(screen.getByLabelText('Email Address'), 'alice@example.com')
    await user.type(screen.getByLabelText('Password'), 'Password1')
    await user.click(screen.getByRole('button', { name: 'Create Account' }))

    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith('/')
    })
  })
})
