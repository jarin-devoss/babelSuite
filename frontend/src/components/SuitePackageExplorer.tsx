import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { FaCopy, FaCubes, FaDownload, FaFile, FaFolderOpen } from 'react-icons/fa6'
import { useClipboardFeedback } from '../hooks/useClipboardFeedback'
import type { SuiteDefinition } from '../lib/api'
import { renderSourceLine, renderStarlarkLine } from '../lib/codeHighlight'
import { SUITE_STAR_PATH } from '../lib/suites'

/** A file the explorer can display: either one of the suite's own source files
 *  or a stand-in rendered when the suite could not be loaded. */
export interface ExplorerFile {
  path: string
  language: string
  content: string
}

interface SuitePackageExplorerProps {
  suite: SuiteDefinition | null
  loading: boolean
  error: string
  /** Shown in the tree and the pane when there is no suite to show. */
  fallbackFile?: ExplorerFile | null
  /** Footer commands used when there is no suite to read them from. */
  fallbackPullCommand?: string
  fallbackForkCommand?: string
  fallbackModules?: string[]
  /** Changing this re-selects the entrypoint — pass whatever identifies the package. */
  resetKey?: string
}

/**
 * The body of a package inspect modal: the file tree, the source pane and the
 * pull/fork footer. The header differs between callers (the catalog shows a
 * registry entry and a star, a run shows the suite it executed) and stays
 * theirs; everything below it is the same view of the same package and lives
 * here, so the two cannot drift apart again.
 */
export function SuitePackageExplorer({
  suite,
  loading,
  error,
  fallbackFile = null,
  fallbackPullCommand,
  fallbackForkCommand,
  fallbackModules,
  resetKey,
}: SuitePackageExplorerProps) {
  const [selected, setSelected] = useState(SUITE_STAR_PATH)
  const { copiedId, copyToClipboard } = useClipboardFeedback(1600)

  // Memoised so the lookups below keep a stable identity: `?? []` would hand
  // them a fresh array on every render with no suite loaded.
  const suiteSourceFiles = useMemo(() => suite?.sourceFiles ?? [], [suite])
  const suiteFolders = suite?.folders ?? []
  const suiteProfiles = suite?.profiles ?? []

  const sourceFileByPath = useMemo(
    () => new Map(suiteSourceFiles.map((file) => [file.path, file])),
    [suiteSourceFiles],
  )
  // The entrypoint has its own entry at the top of the tree and its own pane,
  // so excluding it here is what stops it being listed twice.
  const rootSourceFiles = useMemo(
    () => suiteSourceFiles.filter((file) => !file.path.includes('/') && file.path !== SUITE_STAR_PATH),
    [suiteSourceFiles],
  )

  const fallbackPath = fallbackFile?.path
  useEffect(() => {
    setSelected(suite ? SUITE_STAR_PATH : (fallbackPath ?? SUITE_STAR_PATH))
  }, [suite, fallbackPath, resetKey])

  const activeSourceFile = sourceFileByPath.get(selected)
  const visibleSourceFile = activeSourceFile ?? (suite ? null : fallbackFile)
  const showSourcePane = !loading && Boolean(visibleSourceFile) && selected !== SUITE_STAR_PATH
  const showRichSourceUnavailable = !loading && Boolean(suite) && selected !== SUITE_STAR_PATH && !activeSourceFile
  // With no suite the pane is a stand-in, so the reason it is a stand-in goes
  // inside it; otherwise the error stands on its own. Either way it shows once.
  const errorInPane = Boolean(error) && !suite && showSourcePane

  const pullCommand = suite?.pullCommand ?? fallbackPullCommand
  const forkCommand = suite?.forkCommand ?? fallbackForkCommand
  const visibleModules = suite?.modules ?? fallbackModules ?? []
  const showFooter = !loading && Boolean(suite || pullCommand || forkCommand)

  return (
    <div className='ci-body'>
      <aside className='ci-tree'>
        <p className='ci-tree__label'>Package Files</p>
        {loading
          ? [1, 2, 3, 4, 5].map((n) => <div key={n} className='ci-tree__skeleton' />)
          : (
              <>
                {suite ? (
                  <TreeItem
                    label={SUITE_STAR_PATH}
                    active={selected === SUITE_STAR_PATH}
                    onSelect={() => setSelected(SUITE_STAR_PATH)}
                  />
                ) : fallbackFile && (
                  <TreeItem
                    label={fallbackFile.path}
                    active={selected === fallbackFile.path}
                    onSelect={() => setSelected(fallbackFile.path)}
                  />
                )}
                {rootSourceFiles.map((file) => (
                  <TreeItem
                    key={file.path}
                    label={file.path}
                    active={selected === file.path}
                    onSelect={() => setSelected(file.path)}
                  />
                ))}
                {suiteFolders.map((folder) => (
                  <div key={folder.name} className='ci-tree__group'>
                    <div className='ci-tree__folder'>
                      <span className='ci-tree__item-icon'><FaFolderOpen /></span>
                      <span>{folder.name}/</span>
                    </div>
                    {folder.files.map((filePath) => {
                      const sourcePath = `${folder.name}/${filePath}`
                      return (
                        <TreeItem
                          key={sourcePath}
                          label={filePath}
                          child
                          active={selected === sourcePath}
                          disabled={!sourceFileByPath.has(sourcePath)}
                          onSelect={() => setSelected(sourcePath)}
                        />
                      )
                    })}
                  </div>
                ))}
              </>
            )}
        {suiteProfiles.length > 0 && (
          <>
            <p className='ci-tree__label ci-tree__label--profiles'>Profiles</p>
            {suiteProfiles.map((profile) => (
              <div key={profile.fileName} className='ci-tree__profile'>
                <span>{profile.label}</span>
                {profile.default && <span className='ci-tree__default'>default</span>}
              </div>
            ))}
          </>
        )}
      </aside>

      <div className='ci-content'>
        {loading && (
          <div className='ci-content__loading'>
            <div className='ci-spinner' />
            <p>Loading package…</p>
          </div>
        )}

        {!loading && !error && suite && selected === SUITE_STAR_PATH && (
          <CodePane
            path={SUITE_STAR_PATH}
            content={suite.suiteStar}
            copyId='star'
            copiedId={copiedId}
            onCopy={copyToClipboard}
            renderLine={(line) => renderStarlarkLine(line)}
          />
        )}

        {showSourcePane && visibleSourceFile && (
          <CodePane
            path={visibleSourceFile.path}
            language={visibleSourceFile.language}
            content={visibleSourceFile.content}
            copyId={visibleSourceFile.path}
            copiedId={copiedId}
            onCopy={copyToClipboard}
            renderLine={(line) => renderSourceLine(line, visibleSourceFile.language)}
            notice={errorInPane ? error : ''}
          />
        )}

        {showRichSourceUnavailable && (
          <div className='ci-content__empty'>
            Source content for this file is not available yet. The suite published the folder entry, but not the file preview.
          </div>
        )}

        {error && !errorInPane && <div className='ci-content__error'>{error}</div>}

        {showFooter && (
          <div className='ci-footer'>
            {pullCommand && (
              <button
                type='button'
                className={`ci-cmd-btn${copiedId === 'pull' ? ' ci-cmd-btn--ok' : ''}`}
                onClick={() => void copyToClipboard('pull', pullCommand)}
              >
                <FaCopy />
                <span>{copiedId === 'pull' ? 'Copied!' : 'Copy pull command'}</span>
              </button>
            )}
            {forkCommand && (
              <button
                type='button'
                className={`ci-cmd-btn ci-cmd-btn--ghost${copiedId === 'fork' ? ' ci-cmd-btn--ok' : ''}`}
                onClick={() => void copyToClipboard('fork', forkCommand)}
              >
                <FaDownload />
                <span>{copiedId === 'fork' ? 'Copied!' : 'Fork'}</span>
              </button>
            )}
            {visibleModules.length > 0 && (
              <div className='ci-modules'>
                {visibleModules.map((module) => (
                  <span key={module} className='ci-module-pill'>
                    <FaCubes />{module.replace('@babelsuite/', '')}
                  </span>
                ))}
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  )
}

function TreeItem({
  label,
  active,
  disabled = false,
  child = false,
  onSelect,
}: {
  label: string
  active: boolean
  disabled?: boolean
  child?: boolean
  onSelect: () => void
}) {
  return (
    <button
      type='button'
      className={`ci-tree__item${child ? ' ci-tree__item--child' : ''}${active ? ' ci-tree__item--active' : ''}`}
      onClick={onSelect}
      disabled={disabled}
    >
      <span className='ci-tree__item-icon'><FaFile /></span>
      <span>{label}</span>
    </button>
  )
}

function CodePane({
  path,
  language,
  content,
  copyId,
  copiedId,
  onCopy,
  renderLine,
  notice = '',
}: {
  path: string
  language?: string
  content: string
  copyId: string
  copiedId: string
  onCopy: (id: string, value: string) => void | Promise<void>
  renderLine: (line: string) => ReactNode
  notice?: string
}) {
  return (
    <>
      <div className='ci-content__toolbar'>
        <span className='ci-content__filename'><FaFile /> {path}</span>
        {language && <span className='ci-folder-role'>{language}</span>}
        <button
          type='button'
          className={`ci-copy-btn${copiedId === copyId ? ' ci-copy-btn--ok' : ''}`}
          onClick={() => void onCopy(copyId, content)}
        >
          <FaCopy />
          <span>{copiedId === copyId ? 'Copied!' : 'Copy'}</span>
        </button>
      </div>
      {notice && <div className='ci-content__notice'>{notice}</div>}
      <div className='ci-code'>
        {content.split('\n').map((line, index) => (
          <div key={`${path}-${index + 1}`} className='ci-code__line'>
            <span className='ci-code__num'>{String(index + 1).padStart(3, ' ')}</span>
            <code className='ci-code__text'>{renderLine(line)}</code>
          </div>
        ))}
      </div>
    </>
  )
}
