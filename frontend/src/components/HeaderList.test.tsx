import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { HeaderList } from './HeaderList'

describe('HeaderList', () => {
  it('returns null for empty array', () => {
    const { container } = render(<HeaderList headers={[]} />)
    expect(container.firstChild).toBeNull()
  })

  it('renders header names and values', () => {
    render(<HeaderList headers={[{ name: 'Content-Type', value: 'application/json' }]} />)
    expect(screen.getByText('Content-Type')).toBeInTheDocument()
    expect(screen.getByText('application/json')).toBeInTheDocument()
  })

  it('renders multiple headers', () => {
    render(<HeaderList headers={[
      { name: 'Accept', value: '*/*' },
      { name: 'Authorization', value: 'Bearer token' },
    ]} />)
    expect(screen.getByText('Accept')).toBeInTheDocument()
    expect(screen.getByText('Authorization')).toBeInTheDocument()
  })
})
