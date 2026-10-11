import { createPortal } from 'react-dom'
import { useMemo } from 'react'
import {
  FaCircleCheck,
  FaRegStar,
  FaShieldHalved,
  FaStar,
  FaXmark,
} from 'react-icons/fa6'
import { useEscapeKey } from '../hooks/useEscapeKey'
import type { CatalogPackage, SuiteDefinition } from '../lib/api'
import { logoGradient } from './logoGradient'
import { SuitePackageExplorer, type ExplorerFile } from './SuitePackageExplorer'
import './CatalogInspectModal.css'

interface InspectModalProps {
  item: CatalogPackage
  suite: SuiteDefinition | null
  starred: boolean
  loading: boolean
  error: string
  favoriteBusy: boolean
  onClose: () => void
  onToggleFavorite: () => void
}

export function CatalogInspectModal({ item, suite, starred, loading, error, favoriteBusy, onClose, onToggleFavorite }: InspectModalProps) {
  const fallbackSourceFile = useMemo(() => buildRegistryPreviewFile(item, error), [item, error])

  useEscapeKey(onClose)

  return createPortal(
    <div className='ci-backdrop' onClick={onClose}>
      <div className='ci-modal' onClick={(e) => e.stopPropagation()}>

        {/* Header */}
        <div className='ci-header'>
          <div className='ci-header__logo' style={{ background: logoGradient(item.id) }}>
            {item.title.slice(0, 2).toUpperCase()}
          </div>
          <div className='ci-header__meta'>
            <div className='ci-header__title-row'>
              <h2 className='ci-header__title'>{item.title}</h2>
              {item.status === 'Official' && (
                <span className='ci-badge ci-badge--official'><FaShieldHalved /> Official</span>
              )}
              {item.status === 'Verified' && (
                <span className='ci-badge ci-badge--verified'><FaCircleCheck /> Verified</span>
              )}
            </div>
            <p className='ci-header__origin'>
              {item.owner} · {item.repository} · <strong>{item.version}</strong>
            </p>
          </div>
          <button
            type='button'
            className={`ci-header__star${starred ? ' ci-header__star--active' : ''}`}
            aria-pressed={starred}
            aria-label={starred ? `Unstar ${item.title}` : `Star ${item.title}`}
            title={starred ? 'Unstar' : 'Star'}
            disabled={favoriteBusy}
            onClick={onToggleFavorite}
          >
            {starred ? <FaStar /> : <FaRegStar />}
          </button>
          <button type='button' className='ci-close' onClick={onClose} aria-label='Close'><FaXmark /></button>
        </div>

        <SuitePackageExplorer
          suite={suite}
          loading={loading}
          error={error}
          fallbackFile={fallbackSourceFile}
          fallbackPullCommand={item.pullCommand}
          fallbackForkCommand={item.forkCommand}
          fallbackModules={item.modules}
          resetKey={item.id}
        />
      </div>
    </div>,
    document.body,
  )
}

function buildRegistryPreviewFile(item: CatalogPackage, error: string): ExplorerFile {
  const modules = item.modules.length > 0 ? item.modules.map((module) => `  - ${module}`) : ['  - none published']
  const tags = item.tags.length > 0 ? item.tags.map((tag) => `  - ${tag}`) : ['  - latest']
  const lines = [
    '# BabelSuite registry preview',
    `title: ${item.title}`,
    `repository: ${item.repository}`,
    `provider: ${item.provider}`,
    `version: ${item.version}`,
    `status: ${item.status}`,
    'modules:',
    ...modules,
    'publishedTags:',
    ...tags,
    'commands:',
    `  pull: ${item.pullCommand}`,
    `  fork: ${item.forkCommand}`,
    'description: |',
    ...item.description.split('\n').map((line) => `  ${line}`),
  ]

  if (error.trim()) {
    lines.push(
      'note: |',
      `  ${error}`,
      '  Rich suite files are not available from the backend for this registry package yet.',
    )
  }

  return {
    path: 'registry-preview.yaml',
    language: 'yaml',
    content: lines.join('\n') + '\n',
  }
}
