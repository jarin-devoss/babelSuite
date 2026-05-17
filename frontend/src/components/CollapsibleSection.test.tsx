import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { CollapsibleSection } from './CollapsibleSection'

describe('CollapsibleSection', () => {
  it('renders the title', () => {
    render(<CollapsibleSection title='My Section' open={true} onToggle={vi.fn()}><p>body</p></CollapsibleSection>)
    expect(screen.getByText('My Section')).toBeInTheDocument()
  })

  it('renders children', () => {
    render(<CollapsibleSection title='S' open={true} onToggle={vi.fn()}><p>child content</p></CollapsibleSection>)
    expect(screen.getByText('child content')).toBeInTheDocument()
  })

  it('calls onToggle when header button is clicked', async () => {
    const onToggle = vi.fn()
    const user = userEvent.setup()
    render(<CollapsibleSection title='Click me' open={false} onToggle={onToggle}><span /></CollapsibleSection>)
    await user.click(screen.getByRole('button'))
    expect(onToggle).toHaveBeenCalledTimes(1)
  })

  it('adds open class when open is true', () => {
    const { container } = render(
      <CollapsibleSection title='S' open={true} onToggle={vi.fn()}><span /></CollapsibleSection>
    )
    expect(container.querySelector('.profiles-section--open')).toBeInTheDocument()
  })

  it('does not add open class when open is false', () => {
    const { container } = render(
      <CollapsibleSection title='S' open={false} onToggle={vi.fn()}><span /></CollapsibleSection>
    )
    expect(container.querySelector('.profiles-section--open')).not.toBeInTheDocument()
  })
})
