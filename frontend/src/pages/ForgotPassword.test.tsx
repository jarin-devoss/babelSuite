import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import ForgotPassword from './ForgotPassword'

const { mockForgotPassword } = vi.hoisted(() => ({
  mockForgotPassword: vi.fn(),
}))

vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>()
  return { ...actual, forgotPassword: mockForgotPassword }
})

function renderForgotPassword() {
  return render(
    <MemoryRouter initialEntries={['/forgot-password']}>
      <ForgotPassword />
    </MemoryRouter>
  )
}

describe('ForgotPassword', () => {
  beforeEach(() => {
    mockForgotPassword.mockReset()
  })

  it('renders the email input', () => {
    renderForgotPassword()
    expect(screen.getByLabelText('Email Address')).toBeInTheDocument()
  })

  it('renders the send reset link button', () => {
    renderForgotPassword()
    expect(screen.getByRole('button', { name: 'Send Reset Link' })).toBeInTheDocument()
  })

  it('renders a link back to sign-in', () => {
    renderForgotPassword()
    expect(screen.getByRole('link', { name: 'Sign in' })).toBeInTheDocument()
  })

  it('shows confirmation message after successful submission', async () => {
    mockForgotPassword.mockResolvedValue({ status: 'ok' })
    const user = userEvent.setup()
    renderForgotPassword()

    await user.type(screen.getByLabelText('Email Address'), 'alice@example.com')
    await user.click(screen.getByRole('button', { name: 'Send Reset Link' }))

    await waitFor(() => {
      expect(screen.getByText(/you will receive a reset link/i)).toBeInTheDocument()
    })
    expect(screen.queryByRole('button', { name: /send reset link/i })).not.toBeInTheDocument()
  })

  it('shows loading state while submitting', async () => {
    let resolve: (v: unknown) => void
    mockForgotPassword.mockReturnValue(new Promise((r) => { resolve = r }))
    const user = userEvent.setup()
    renderForgotPassword()

    await user.type(screen.getByLabelText('Email Address'), 'alice@example.com')
    await user.click(screen.getByRole('button', { name: 'Send Reset Link' }))

    expect(screen.getByRole('button', { name: 'Sending...' })).toBeDisabled()
    resolve!({ status: 'ok' })
  })

  it('shows confirmation even on non-503 error (enumeration prevention)', async () => {
    mockForgotPassword.mockRejectedValue(new Error('Not found'))
    const user = userEvent.setup()
    renderForgotPassword()

    await user.type(screen.getByLabelText('Email Address'), 'unknown@example.com')
    await user.click(screen.getByRole('button', { name: 'Send Reset Link' }))

    await waitFor(() => {
      expect(screen.getByText(/you will receive a reset link/i)).toBeInTheDocument()
    })
  })

  it('accepts typed email value', async () => {
    renderForgotPassword()
    const user = userEvent.setup()
    const input = screen.getByLabelText('Email Address')
    await user.type(input, 'test@example.com')
    expect(input).toHaveValue('test@example.com')
  })
})
