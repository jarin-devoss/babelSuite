import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import AuthLayout from './AuthLayout'

function renderInRouter(ui: React.ReactElement) {
  return render(<MemoryRouter>{ui}</MemoryRouter>)
}

describe('AuthLayout', () => {
  it('renders the title', () => {
    renderInRouter(
      <AuthLayout title='Sign in' subtitle='Welcome back.' footer={<span>footer</span>}>
        <div />
      </AuthLayout>
    )
    expect(screen.getByRole('heading', { name: 'Sign in' })).toBeInTheDocument()
  })

  it('renders the subtitle', () => {
    renderInRouter(
      <AuthLayout title='Sign in' subtitle='Welcome back.' footer={<span>footer</span>}>
        <div />
      </AuthLayout>
    )
    expect(screen.getByText('Welcome back.')).toBeInTheDocument()
  })

  it('renders children', () => {
    renderInRouter(
      <AuthLayout title='T' subtitle='S' footer={<span>footer</span>}>
        <p>child content</p>
      </AuthLayout>
    )
    expect(screen.getByText('child content')).toBeInTheDocument()
  })

  it('renders footer content', () => {
    renderInRouter(
      <AuthLayout title='T' subtitle='S' footer={<span>footer text</span>}>
        <div />
      </AuthLayout>
    )
    expect(screen.getByText('footer text')).toBeInTheDocument()
  })

  it('renders the BabelSuite brand link', () => {
    renderInRouter(
      <AuthLayout title='T' subtitle='S' footer={<span>f</span>}>
        <div />
      </AuthLayout>
    )
    expect(screen.getByRole('link', { name: 'BabelSuite home' })).toBeInTheDocument()
  })
})
