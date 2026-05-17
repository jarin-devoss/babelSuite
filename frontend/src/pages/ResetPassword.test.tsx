import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import ResetPassword from './ResetPassword'

const { mockResetPassword } = vi.hoisted(() => ({
  mockResetPassword: vi.fn(),
}))

vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>()
  return { ...actual, resetPassword: mockResetPassword }
})

function renderWithToken(token = 'valid-token-abc') {
  return render(
    <MemoryRouter initialEntries={[`/reset-password?token=${token}`]}>
      <ResetPassword />
    </MemoryRouter>
  )
}

function renderWithoutToken() {
  return render(
    <MemoryRouter initialEntries={['/reset-password']}>
      <ResetPassword />
    </MemoryRouter>
  )
}

describe('ResetPassword', () => {
  beforeEach(() => {
    mockResetPassword.mockReset()
  })

  it('shows invalid link page when no token in URL', () => {
    renderWithoutToken()
    expect(screen.getByText('Invalid link')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Request a new link' })).toBeInTheDocument()
  })

  it('renders the password field when token is present', () => {
    renderWithToken()
    expect(screen.getByLabelText('New Password')).toBeInTheDocument()
  })

  it('renders the reset button', () => {
    renderWithToken()
    expect(screen.getByRole('button', { name: 'Reset Password' })).toBeInTheDocument()
  })

  it('toggles password visibility', async () => {
    renderWithToken()
    const user = userEvent.setup()
    const input = screen.getByLabelText('New Password')
    expect(input).toHaveAttribute('type', 'password')

    await user.click(screen.getByRole('button', { name: 'Show password' }))
    expect(input).toHaveAttribute('type', 'text')

    await user.click(screen.getByRole('button', { name: 'Hide password' }))
    expect(input).toHaveAttribute('type', 'password')
  })

  it('shows success state after reset', async () => {
    mockResetPassword.mockResolvedValue({ status: 'ok' })
    const user = userEvent.setup()
    renderWithToken()

    await user.type(screen.getByLabelText('New Password'), 'NewSecure123!')
    await user.click(screen.getByRole('button', { name: 'Reset Password' }))

    await waitFor(() => {
      expect(screen.getByText('Password updated')).toBeInTheDocument()
    })
    expect(screen.getByRole('link', { name: 'Sign in with your new password' })).toBeInTheDocument()
  })

  it('shows error message on failure', async () => {
    mockResetPassword.mockRejectedValue(new Error('network failure'))
    const user = userEvent.setup()
    renderWithToken()

    await user.type(screen.getByLabelText('New Password'), 'pass')
    await user.click(screen.getByRole('button', { name: 'Reset Password' }))

    await waitFor(() => {
      expect(screen.getByText(/something went wrong/i)).toBeInTheDocument()
    })
  })

  it('shows loading state while submitting', async () => {
    let resolve: (v: unknown) => void
    mockResetPassword.mockReturnValue(new Promise((r) => { resolve = r }))
    const user = userEvent.setup()
    renderWithToken()

    await user.type(screen.getByLabelText('New Password'), 'pass')
    await user.click(screen.getByRole('button', { name: 'Reset Password' }))

    expect(screen.getByRole('button', { name: 'Resetting...' })).toBeDisabled()
    resolve!({ status: 'ok' })
  })
})
