import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { SuitePackageExplorer } from './SuitePackageExplorer'
import type { SuiteDefinition } from '../lib/api'

const suite: SuiteDefinition = {
  id: 'gateway-check',
  title: 'Gateway Check',
  repository: 'localhost:5000/qa/gateway-check',
  owner: 'QA',
  provider: 'Zot',
  version: 'v1.0.0',
  tags: ['latest'],
  description: '',
  modules: ['@babelsuite/postgres'],
  status: 'Verified',
  score: 80,
  pullCommand: 'babelctl run localhost:5000/qa/gateway-check',
  forkCommand: 'babelctl fork localhost:5000/qa/gateway-check',
  suiteStar: 'load("@babelsuite/runtime", "service")\n',
  profiles: [],
  folders: [{ name: 'tests', role: 'Core', description: 'Verification assets', files: ['reach.sh', 'absent.sh'] }],
  sourceFiles: [
    { path: 'suite.star', language: 'starlark', content: 'load("@babelsuite/runtime", "service")\n' },
    { path: 'README.md', language: 'markdown', content: '# Gateway Check\n' },
    { path: 'metadata.yaml', language: 'yaml', content: 'kind: OCIExampleSuite\n' },
    { path: 'tests/reach.sh', language: 'bash', content: 'echo reached\n' },
  ],
  topology: [],
  contracts: [],
  apiSurfaces: [],
}

function treeLabels() {
  return Array.from(document.querySelectorAll('.ci-tree__item')).map((el) => el.textContent?.trim())
}

describe('SuitePackageExplorer — tree', () => {
  it('lists the entrypoint exactly once', () => {
    render(<SuitePackageExplorer suite={suite} loading={false} error='' />)
    expect(treeLabels().filter((label) => label === 'suite.star')).toHaveLength(1)
  })

  // The catalog modal used to render only the entrypoint and the folders, so
  // root files published next to suite.star were missing from its tree.
  it('lists root source files alongside the entrypoint', () => {
    render(<SuitePackageExplorer suite={suite} loading={false} error='' />)
    const labels = treeLabels()
    expect(labels).toContain('suite.star')
    expect(labels).toContain('README.md')
    expect(labels).toContain('metadata.yaml')
  })

  it('lists folder children and disables the ones with no published content', () => {
    render(<SuitePackageExplorer suite={suite} loading={false} error='' />)
    expect(screen.getByRole('button', { name: 'reach.sh' })).toBeEnabled()
    expect(screen.getByRole('button', { name: 'absent.sh' })).toBeDisabled()
  })

  it('shows skeletons instead of files while loading', () => {
    render(<SuitePackageExplorer suite={null} loading error='' />)
    expect(document.querySelectorAll('.ci-tree__skeleton').length).toBeGreaterThan(0)
    expect(treeLabels()).toHaveLength(0)
  })
})

describe('SuitePackageExplorer — content', () => {
  it('renders the entrypoint body first', () => {
    render(<SuitePackageExplorer suite={suite} loading={false} error='' />)
    expect(screen.getByText(/@babelsuite\/runtime/)).toBeInTheDocument()
  })

  it('renders a selected root file', async () => {
    const user = userEvent.setup()
    render(<SuitePackageExplorer suite={suite} loading={false} error='' />)
    await user.click(screen.getByRole('button', { name: 'README.md' }))
    expect(screen.getByText('# Gateway Check')).toBeInTheDocument()
  })

  it('renders a selected folder file', async () => {
    const user = userEvent.setup()
    render(<SuitePackageExplorer suite={suite} loading={false} error='' />)
    await user.click(screen.getByRole('button', { name: 'reach.sh' }))
    expect(screen.getByText('echo reached')).toBeInTheDocument()
  })

  it('re-selects the entrypoint when the package changes', async () => {
    const user = userEvent.setup()
    const { rerender } = render(<SuitePackageExplorer suite={suite} loading={false} error='' resetKey='a' />)
    await user.click(screen.getByRole('button', { name: 'README.md' }))
    expect(screen.getByText('# Gateway Check')).toBeInTheDocument()

    const other: SuiteDefinition = { ...suite, id: 'other', suiteStar: 'load("@babelsuite/kafka", "kafka")\n' }
    rerender(<SuitePackageExplorer suite={other} loading={false} error='' resetKey='other' />)

    expect(screen.getByText(/@babelsuite\/kafka/)).toBeInTheDocument()
    expect(screen.queryByText('# Gateway Check')).toBeNull()
  })
})

describe('SuitePackageExplorer — no suite', () => {
  const fallback = { path: 'registry-preview.yaml', language: 'yaml', content: 'title: Gateway Check\n' }

  it('falls back to the stand-in file and shows the error beside it', () => {
    render(
      <SuitePackageExplorer
        suite={null}
        loading={false}
        error='Could not load suite.'
        fallbackFile={fallback}
      />,
    )
    expect(treeLabels()).toContain('registry-preview.yaml')
    expect(screen.getByText('title: Gateway Check')).toBeInTheDocument()
    expect(document.querySelector('.ci-content__notice')?.textContent).toBe('Could not load suite.')
    expect(document.querySelector('.ci-content__error')).toBeNull()
  })

  it('shows the error on its own when there is nothing to stand in', () => {
    render(<SuitePackageExplorer suite={null} loading={false} error='Could not load suite.' />)
    expect(document.querySelector('.ci-content__error')?.textContent).toBe('Could not load suite.')
    expect(document.querySelector('.ci-content__notice')).toBeNull()
  })

  it('uses the caller commands in the footer when there is no suite', () => {
    render(
      <SuitePackageExplorer
        suite={null}
        loading={false}
        error=''
        fallbackFile={fallback}
        fallbackPullCommand='babelctl run example'
        fallbackForkCommand='babelctl fork example'
        fallbackModules={['@babelsuite/kafka']}
      />,
    )
    expect(screen.getByRole('button', { name: /copy pull command/i })).toBeInTheDocument()
    expect(screen.getByText('kafka')).toBeInTheDocument()
  })

  it('hides the footer when there is neither a suite nor caller commands', () => {
    render(<SuitePackageExplorer suite={null} loading={false} error='' fallbackFile={fallback} />)
    expect(document.querySelector('.ci-footer')).toBeNull()
  })
})
