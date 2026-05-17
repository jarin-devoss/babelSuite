import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { ErrorBoundary } from './ErrorBoundary'

vi.mock('../lib/telemetry', () => ({
  recordUnhandledError: vi.fn(),
}))

function Bomb(): never {
  throw new Error('kaboom')
}

describe('ErrorBoundary', () => {
  beforeEach(() => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  it('renders children when no error', () => {
    render(<ErrorBoundary><p>all good</p></ErrorBoundary>)
    expect(screen.getByText('all good')).toBeInTheDocument()
  })

  it('renders default fallback when child throws', () => {
    render(<ErrorBoundary><Bomb /></ErrorBoundary>)
    expect(screen.getByText('Something went wrong')).toBeInTheDocument()
    expect(screen.getByText('kaboom')).toBeInTheDocument()
  })

  it('renders custom fallback when provided', () => {
    render(
      <ErrorBoundary fallback={<p>custom error UI</p>}>
        <Bomb />
      </ErrorBoundary>
    )
    expect(screen.getByText('custom error UI')).toBeInTheDocument()
    expect(screen.queryByText('Something went wrong')).not.toBeInTheDocument()
  })

  it('renders reload button in default fallback', () => {
    render(<ErrorBoundary><Bomb /></ErrorBoundary>)
    expect(screen.getByRole('button', { name: /reload/i })).toBeInTheDocument()
  })
})
