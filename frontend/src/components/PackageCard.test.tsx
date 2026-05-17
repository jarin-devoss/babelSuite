import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { PackageCard } from './PackageCard'
import type { PackageCardProps } from './PackageCard'
import type { CatalogPackage } from '../lib/api'

const mockItem: CatalogPackage = {
  id: 'pkg-1',
  title: 'My Package',
  owner: 'acme',
  repository: 'acme/my-package',
  version: 'v1.2.3',
  description: 'A test package',
  provider: 'docker.io',
  kind: 'suite',
  status: 'Official',
  modules: ['@babelsuite/http', '@babelsuite/grpc'],
  tags: ['v1.2.3', 'stable'],
  pullCommand: 'bs pull acme/my-package',
  forkCommand: 'bs fork acme/my-package',
  score: 0,
  inspectable: true,
  starred: false,
}

function renderCard(overrides: Partial<PackageCardProps> = {}) {
  const props: PackageCardProps = {
    item: mockItem,
    starred: false,
    copiedId: '',
    favoriteBusy: false,
    onInspect: vi.fn(),
    onCopyRun: vi.fn(),
    onCopyFork: vi.fn(),
    onToggleFavorite: vi.fn(),
    ...overrides,
  }
  return render(<PackageCard {...props} />)
}

describe('PackageCard', () => {
  it('renders the package title', () => {
    renderCard()
    expect(screen.getByText('My Package')).toBeInTheDocument()
  })

  it('renders owner and repository', () => {
    renderCard()
    expect(screen.getByText('acme')).toBeInTheDocument()
    expect(screen.getByText('acme/my-package')).toBeInTheDocument()
  })

  it('renders the description', () => {
    renderCard()
    expect(screen.getByText('A test package')).toBeInTheDocument()
  })

  it('renders Inspect button for suite kind', () => {
    renderCard()
    expect(screen.getByRole('button', { name: /inspect/i })).toBeInTheDocument()
  })

  it('calls onInspect when inspect button is clicked', async () => {
    const onInspect = vi.fn()
    const user = userEvent.setup()
    renderCard({ onInspect })
    await user.click(screen.getByRole('button', { name: /inspect/i }))
    expect(onInspect).toHaveBeenCalledTimes(1)
  })

  it('calls onCopyRun when run command button clicked', async () => {
    const onCopyRun = vi.fn()
    const user = userEvent.setup()
    renderCard({ onCopyRun })
    await user.click(screen.getByRole('button', { name: /run command/i }))
    expect(onCopyRun).toHaveBeenCalledTimes(1)
  })

  it('calls onCopyFork when fork button clicked', async () => {
    const onCopyFork = vi.fn()
    const user = userEvent.setup()
    renderCard({ onCopyFork })
    await user.click(screen.getByRole('button', { name: /fork/i }))
    expect(onCopyFork).toHaveBeenCalledTimes(1)
  })

  it('shows star button with unstarred state', () => {
    renderCard({ starred: false })
    expect(screen.getByRole('button', { name: /star my package/i })).toBeInTheDocument()
  })

  it('shows unstar button when starred', () => {
    renderCard({ starred: true })
    expect(screen.getByRole('button', { name: /unstar my package/i })).toBeInTheDocument()
  })

  it('calls onToggleFavorite when star button clicked', async () => {
    const onToggleFavorite = vi.fn()
    const user = userEvent.setup()
    renderCard({ onToggleFavorite })
    await user.click(screen.getByRole('button', { name: /star my package/i }))
    expect(onToggleFavorite).toHaveBeenCalledTimes(1)
  })

  it('disables star button when favoriteBusy', () => {
    renderCard({ favoriteBusy: true })
    expect(screen.getByRole('button', { name: /star my package/i })).toBeDisabled()
  })

  it('shows Copied! on run command when copiedId matches', () => {
    renderCard({ copiedId: 'pkg-1-run' })
    expect(screen.getByRole('button', { name: /copied!/i })).toBeInTheDocument()
  })
})
