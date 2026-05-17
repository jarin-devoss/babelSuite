import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import SlidingPanel from './SlidingPanel'

describe('SlidingPanel', () => {
  it('renders header content', () => {
    render(<SlidingPanel isOpen={true} onClose={vi.fn()} header={<h2>Panel Title</h2>}><p>body</p></SlidingPanel>)
    expect(screen.getByText('Panel Title')).toBeInTheDocument()
  })

  it('renders children', () => {
    render(<SlidingPanel isOpen={true} onClose={vi.fn()} header={<span>H</span>}><p>Panel body content</p></SlidingPanel>)
    expect(screen.getByText('Panel body content')).toBeInTheDocument()
  })

  it('applies open class when isOpen is true', () => {
    const { baseElement } = render(
      <SlidingPanel isOpen={true} onClose={vi.fn()} header={<span />}><span /></SlidingPanel>
    )
    expect(baseElement.querySelector('.sliding-panel--open')).toBeInTheDocument()
    expect(baseElement.querySelector('.sliding-panel-overlay--open')).toBeInTheDocument()
  })

  it('does not apply open class when isOpen is false', () => {
    const { baseElement } = render(
      <SlidingPanel isOpen={false} onClose={vi.fn()} header={<span />}><span /></SlidingPanel>
    )
    expect(baseElement.querySelector('.sliding-panel--open')).not.toBeInTheDocument()
  })

  it('calls onClose when overlay is clicked', async () => {
    const onClose = vi.fn()
    const user = userEvent.setup()
    const { baseElement } = render(
      <SlidingPanel isOpen={true} onClose={onClose} header={<span />}><span /></SlidingPanel>
    )
    await user.click(baseElement.querySelector('.sliding-panel-overlay')!)
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('applies custom width via style', () => {
    const { baseElement } = render(
      <SlidingPanel isOpen={true} onClose={vi.fn()} header={<span />} width='800px'><span /></SlidingPanel>
    )
    const panel = baseElement.querySelector('.sliding-panel') as HTMLElement
    expect(panel.style.width).toBe('800px')
  })
})
