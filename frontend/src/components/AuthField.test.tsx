import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import AuthField from './AuthField'

describe('AuthField', () => {
  it('renders the label text', () => {
    render(<AuthField label='Email Address' value='' onChange={vi.fn()} />)
    expect(screen.getByText('Email Address')).toBeInTheDocument()
  })

  it('renders an input with the correct type', () => {
    render(<AuthField label='Password' type='password' value='' onChange={vi.fn()} />)
    expect(screen.getByLabelText('Password')).toHaveAttribute('type', 'password')
  })

  it('defaults input type to text when not specified', () => {
    render(<AuthField label='Name' value='' onChange={vi.fn()} />)
    expect(screen.getByLabelText('Name')).toHaveAttribute('type', 'text')
  })

  it('displays the current value', () => {
    render(<AuthField label='Email' value='test@example.com' onChange={vi.fn()} />)
    expect(screen.getByLabelText('Email')).toHaveValue('test@example.com')
  })

  it('calls onChange when the user types', async () => {
    const user = userEvent.setup()
    const handleChange = vi.fn()
    render(<AuthField label='Email' value='' onChange={handleChange} />)
    await user.type(screen.getByLabelText('Email'), 'a')
    expect(handleChange).toHaveBeenCalledTimes(1)
  })

  it('renders a trailing element when provided', () => {
    render(
      <AuthField
        label='Password'
        value=''
        onChange={vi.fn()}
        trailing={<button type='button'>Toggle</button>}
      />
    )
    expect(screen.getByRole('button', { name: 'Toggle' })).toBeInTheDocument()
  })
})
