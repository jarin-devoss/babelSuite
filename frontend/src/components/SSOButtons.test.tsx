import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import SSOButtons from './SSOButtons'
import type { SSOProvider } from '../lib/api'

const enabledProvider: SSOProvider = {
  providerId: 'github',
  name: 'GitHub',
  buttonLabel: 'Sign in with GitHub',
  enabled: true,
  startUrl: 'http://localhost:8090/api/v1/auth/github/start',
}

const disabledProvider: SSOProvider = {
  providerId: 'oidc',
  name: 'SSO',
  buttonLabel: 'Sign in with SSO',
  enabled: false,
  startUrl: '',
}

describe('SSOButtons', () => {
  it('renders an anchor for an enabled provider with safe URL', () => {
    render(<SSOButtons providers={[enabledProvider]} onUnavailable={vi.fn()} />)
    const link = screen.getByRole('link', { name: /sign in with github/i })
    expect(link).toBeInTheDocument()
    expect(link).toHaveAttribute('href', expect.stringContaining('localhost:8090'))
  })

  it('renders a button for a disabled provider', () => {
    render(<SSOButtons providers={[disabledProvider]} onUnavailable={vi.fn()} />)
    expect(screen.getByRole('button', { name: /sign in with sso/i })).toBeInTheDocument()
    expect(screen.queryByRole('link')).not.toBeInTheDocument()
  })

  it('calls onUnavailable when disabled provider button is clicked', async () => {
    const onUnavailable = vi.fn()
    const user = userEvent.setup()
    render(<SSOButtons providers={[disabledProvider]} onUnavailable={onUnavailable} />)
    await user.click(screen.getByRole('button', { name: /sign in with sso/i }))
    expect(onUnavailable).toHaveBeenCalledWith(disabledProvider)
  })

  it('renders multiple providers', () => {
    render(<SSOButtons providers={[enabledProvider, disabledProvider]} onUnavailable={vi.fn()} />)
    expect(screen.getByRole('link', { name: /github/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /sso/i })).toBeInTheDocument()
  })

  it('renders nothing for empty providers list', () => {
    const { container } = render(<SSOButtons providers={[]} onUnavailable={vi.fn()} />)
    expect(container.querySelector('.auth-sso')?.children).toHaveLength(0)
  })
})
