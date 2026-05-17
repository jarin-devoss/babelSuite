import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import AppShell from './AppShell'

const { mockNavigate, mockGetSession } = vi.hoisted(() => ({
  mockNavigate: vi.fn(),
  mockGetSession: vi.fn(),
}))

vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-router-dom')>()
  return { ...actual, useNavigate: () => mockNavigate }
})

vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>()
  return { ...actual, getSession: mockGetSession, clearSession: vi.fn() }
})

function renderShell(isAdmin = false) {
  mockGetSession.mockReturnValue({
    token: 'tok',
    user: { userId: 'u1', workspaceId: 'w1', isAdmin },
  })
  return render(
    <MemoryRouter initialEntries={['/']}>
      <AppShell section='Home' title='Executions' description='desc'>
        <p>content</p>
      </AppShell>
    </MemoryRouter>
  )
}

describe('AppShell', () => {
  beforeEach(() => {
    mockNavigate.mockReset()
    localStorage.clear()
  })

  it('renders nav items for non-admin', () => {
    renderShell(false)
    expect(screen.getByRole('link', { name: /runs/i })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /catalog/i })).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /settings/i })).not.toBeInTheDocument()
  })

  it('renders settings link for admin', () => {
    renderShell(true)
    expect(screen.getByRole('link', { name: /settings/i })).toBeInTheDocument()
  })

  it('renders the section title and heading', () => {
    renderShell()
    expect(screen.getByText('Home')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Executions' })).toBeInTheDocument()
  })

  it('renders children', () => {
    renderShell()
    expect(screen.getByText('content')).toBeInTheDocument()
  })

  it('toggles collapsed class on collapse button click', async () => {
    const user = userEvent.setup()
    const { container } = renderShell()
    const collapseBtn = screen.getByRole('button', { name: /collapse navigation/i })
    await user.click(collapseBtn)
    expect(container.querySelector('.app-shell--collapsed')).toBeInTheDocument()
  })

  it('shows sign out button', () => {
    renderShell()
    expect(screen.getByRole('button', { name: /sign out/i })).toBeInTheDocument()
  })
})
