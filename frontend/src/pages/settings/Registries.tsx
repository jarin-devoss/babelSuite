import { startTransition, useEffect, useMemo, useRef, useState } from 'react'
import {
  FaArrowRotateRight,
  FaCircleCheck,
  FaCircleXmark,
  FaClock,
  FaDatabase,
  FaPlus,
  FaTrash,
} from 'react-icons/fa6'
import {
  ApiError,
  getPlatformSettings,
  syncRegistry,
  updatePlatformSettings,
  type OCIRegistry,
  type PlatformSettings,
} from '../../lib/api'
import AppShell from '../../components/AppShell'
import { RowMenu } from '../../components/RowMenu'
import SlidingPanel from '../../components/SlidingPanel'
import '../PlatformSettings.css'

const REGISTRY_PROVIDERS = ['Zot', 'Generic OCI', 'Harbor', 'JFrog Artifactory', 'AWS ECR'] as const

export default function Registries() {
  const [savedSettings, setSavedSettings] = useState<PlatformSettings | null>(null)
  const [draft, setDraft] = useState<PlatformSettings | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [syncingId, setSyncingId] = useState('')
  const [message, setMessage] = useState<{ tone: 'success' | 'error'; text: string } | null>(null)
  const [panelRegistry, setPanelRegistry] = useState<OCIRegistry | null>(null)
  const [panelOpen, setPanelOpen] = useState(false)
  const [isNewRegistry, setIsNewRegistry] = useState(false)

  useEffect(() => {
    let cancelled = false
    const load = async () => {
      setLoading(true)
      try {
        const settings = await getPlatformSettings()
        if (cancelled) return
        startTransition(() => {
          setSavedSettings(settings)
          setDraft(settings)
        })
      } catch (reason) {
        if (cancelled) return
        setMessage({ tone: 'error', text: reason instanceof ApiError ? reason.message : 'Could not load settings.' })
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    void load()
    return () => { cancelled = true }
  }, [])

  const patchDraft = (mutator: (next: PlatformSettings) => void) => {
    setDraft((current) => {
      if (!current) return current
      const next = structuredClone(current)
      mutator(next)
      return next
    })
  }

  const updatePanelRegistry = <K extends keyof OCIRegistry>(field: K, value: OCIRegistry[K]) => {
    if (!panelRegistry) return
    const updated = { ...panelRegistry, [field]: value }
    setPanelRegistry(updated)
    if (!isNewRegistry) {
      patchDraft((next) => {
        next.registries = next.registries.map((r) =>
          r.registryId === panelRegistry.registryId ? updated : r,
        )
      })
    }
  }

  const openPanel = (registry: OCIRegistry) => {
    setPanelRegistry(structuredClone(registry))
    setIsNewRegistry(false)
    setPanelOpen(true)
  }

  const closePanelTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  useEffect(() => () => { if (closePanelTimerRef.current !== null) clearTimeout(closePanelTimerRef.current) }, [])

  const closePanel = () => {
    setPanelOpen(false)
    setIsNewRegistry(false)
    if (closePanelTimerRef.current !== null) clearTimeout(closePanelTimerRef.current)
    closePanelTimerRef.current = setTimeout(() => { closePanelTimerRef.current = null; setPanelRegistry(null) }, 300)
  }

  const addRegistry = () => {
    if (!draft) return
    const registry = emptyRegistry(draft.registries.length + 1)
    setPanelRegistry(registry)
    setIsNewRegistry(true)
    setPanelOpen(true)
  }

  const removeRegistry = (registryId: string) => {
    if (isNewRegistry) { closePanel(); return }
    if (!draft || draft.registries.length === 1) return
    patchDraft((next) => { next.registries = next.registries.filter((r) => r.registryId !== registryId) })
    closePanel()
  }

  const syncSelected = async (registryId: string) => {
    setSyncingId(registryId)
    setMessage(null)
    try {
      const updated = await syncRegistry(registryId)
      startTransition(() => {
        setDraft(updated)
        setSavedSettings(updated)
        const refreshed = updated.registries.find((r) => r.registryId === registryId)
        if (refreshed) setPanelRegistry(structuredClone(refreshed))
        setMessage({ tone: 'success', text: 'Registry sync completed and catalog index refreshed.' })
      })
    } catch (reason) {
      setMessage({ tone: 'error', text: reason instanceof ApiError ? reason.message : 'Could not sync registry.' })
    } finally {
      setSyncingId('')
    }
  }

  const save = async () => {
    if (!draft) return
    setSaving(true)
    setMessage(null)
    const wasNew = isNewRegistry
    const newRegistryId = panelRegistry?.registryId
    try {
      const payload = (isNewRegistry && panelRegistry)
        ? { ...draft, registries: [...draft.registries, panelRegistry] }
        : draft
      const updated = await updatePlatformSettings(payload)
      startTransition(() => {
        setDraft(updated)
        setSavedSettings(updated)
        setIsNewRegistry(false)
        setMessage({ tone: 'success', text: 'OCI registries saved.' })
      })
      if (wasNew && newRegistryId) {
        await syncSelected(newRegistryId)
      }
    } catch (reason) {
      setMessage({ tone: 'error', text: reason instanceof ApiError ? reason.message : 'Could not save settings.' })
    } finally {
      setSaving(false)
    }
  }

  const dirty = useMemo(
    () => isNewRegistry || JSON.stringify(draft) !== JSON.stringify(savedSettings),
    [isNewRegistry, draft, savedSettings],
  )

  if (loading) {
    return (
      <AppShell section='Settings' title='OCI Registries' description=''>
        <div className='platform-page platform-page--loading'>
          <div className='platform-loading-card'>
            <p className='platform-loading-card__eyebrow'>Settings</p>
            <h1>Loading OCI registries…</h1>
          </div>
        </div>
      </AppShell>
    )
  }

  if (!draft) {
    return (
      <AppShell section='Settings' title='OCI Registries' description=''>
        <div className='platform-page'>
          {message && <div className={`platform-alert platform-alert--${message.tone}`}>{message.text}</div>}
          <div className='platform-loading-card'>
            <p className='platform-loading-card__eyebrow'>Settings</p>
            <h1>OCI registries unavailable</h1>
            <p>The frontend could not load platform settings from the backend.</p>
          </div>
        </div>
      </AppShell>
    )
  }

  return (
    <AppShell
      section='Settings'
      sectionTo='/settings'
      title='OCI Registries'
      description='Control where BabelSuite discovers suites and native modules.'
      actions={(
        <>
          <button type='button' className='platform-button platform-button--secondary' onClick={addRegistry}>
            <FaPlus /> <span>Add Registry</span>
          </button>
          <button type='button' className='platform-button' onClick={save} disabled={!dirty || saving}>
            {saving ? 'Saving…' : dirty ? 'Save Changes' : 'Saved'}
          </button>
        </>
      )}
    >
      <div className='platform-page'>
        {message && <div className={`platform-alert platform-alert--${message.tone}`}>{message.text}</div>}

        <div className='bs-table-list bs-table-list--clickable'>
          <div className='bs-table-list__head'>
            <div className='bs-table-row'>
              <div className='bs-table-cell bs-table-cell--shrink' />
              <div className='bs-table-cell'>Name</div>
              <div className='bs-table-cell'>Provider</div>
              <div className='bs-table-cell bs-table-cell--wide'>URL</div>
              <div className='bs-table-cell'>Status</div>
              <div className='bs-table-cell'>Last Synced</div>
              <div className='bs-table-cell bs-table-cell--shrink' />
            </div>
          </div>

          {draft.registries.length === 0 && (
            <div className='bs-table-list__empty'>
              <FaDatabase className='bs-table-list__empty-icon' />
              <h4>No OCI registries</h4>
              <p>Connect a registry to discover suites and native modules from your catalog.</p>
              <button type='button' className='platform-button' onClick={addRegistry}><FaPlus /> Add Registry</button>
            </div>
          )}

          {draft.registries.map((registry) => (
            <div className='bs-table-list__row' key={registry.registryId} onClick={() => openPanel(registry)}>
              <div className='bs-table-row'>
                <div className='bs-table-cell bs-table-cell--shrink'>
                  {registry.syncStatus?.toLowerCase().includes('indexed')
                    ? <FaCircleCheck className='bs-status-icon bs-status-icon--ok' />
                    : registry.syncStatus?.toLowerCase().includes('pending')
                      ? <FaClock className='bs-status-icon bs-status-icon--warn' />
                      : <FaCircleXmark className='bs-status-icon bs-status-icon--off' />}
                </div>
                <div className='bs-table-cell'>
                  <strong>{registry.name}</strong>
                  <p className='bs-table-cell__sub'>{registry.registryId}</p>
                </div>
                <div className='bs-table-cell'>
                  <span className='bs-tag'>{registry.provider}</span>
                </div>
                <div className='bs-table-cell bs-table-cell--wide'>
                  <span className='bs-table-cell__mono'>{registry.registryUrl || '—'}</span>
                </div>
                <div className='bs-table-cell'>
                  <span className={`bs-status-badge bs-status-badge--${syncTone(registry.syncStatus)}`}>
                    {registry.syncStatus || 'Unknown'}
                  </span>
                </div>
                <div className='bs-table-cell'>
                  {registry.lastSyncedAt
                    ? <span>{new Date(registry.lastSyncedAt).toLocaleString()}</span>
                    : <span className='bs-table-cell__muted'>Never</span>}
                </div>
                <div className='bs-table-cell bs-table-cell--shrink'>
                  <RowMenu items={[
                    { label: 'Sync Now', action: () => void syncSelected(registry.registryId) },
                    { label: 'Delete', danger: true, action: () => removeRegistry(registry.registryId) },
                  ]} />
                </div>
              </div>
            </div>
          ))}
        </div>
      </div>

      <SlidingPanel
        isOpen={panelOpen}
        onClose={closePanel}
        header={(
          <>
            <button type='button' className='platform-button' onClick={save} disabled={!dirty || saving}>
              {saving ? 'Saving…' : 'Save'}
            </button>
            {panelRegistry && (
              <button
                type='button'
                className='platform-button platform-button--secondary'
                onClick={() => void syncSelected(panelRegistry.registryId)}
                disabled={syncingId === panelRegistry.registryId}
              >
                <FaArrowRotateRight />
                <span>{syncingId === panelRegistry.registryId ? 'Syncing…' : 'Sync Now'}</span>
              </button>
            )}
            <button type='button' className='platform-button platform-button--secondary' onClick={closePanel}>Cancel</button>
            <button
              type='button'
              className='platform-icon-button platform-header-spacer'
              onClick={() => panelRegistry && removeRegistry(panelRegistry.registryId)}
              disabled={draft.registries.length <= 1}
              title='Remove registry'
            >
              <FaTrash />
            </button>
          </>
        )}
      >
        {panelRegistry && (
          <>
            <div className='white-box'>
              <p className='white-box__section-header'>Identity</p>
              <div className='bs-form-row bs-form-row--two'>
                <div>
                  <label>Registry ID</label>
                  <input value={panelRegistry.registryId} onChange={(e) => updatePanelRegistry('registryId', e.target.value)} />
                </div>
                <div>
                  <label>Display Name</label>
                  <input value={panelRegistry.name} onChange={(e) => updatePanelRegistry('name', e.target.value)} />
                </div>
              </div>
            </div>

            <div className='white-box'>
              <p className='white-box__section-header'>Connection</p>
              <div className='bs-form-row bs-form-row--two'>
                <div>
                  <label>Provider</label>
                  <select value={panelRegistry.provider} onChange={(e) => updatePanelRegistry('provider', e.target.value)}>
                    {REGISTRY_PROVIDERS.map((p) => <option key={p} value={p}>{p}</option>)}
                  </select>
                </div>
                <div>
                  <label>Region <span className='bs-label-note'>(cloud only)</span></label>
                  <input value={panelRegistry.region} onChange={(e) => updatePanelRegistry('region', e.target.value)} placeholder='eu-west-1' />
                </div>
              </div>
              <div className='bs-form-row'>
                <label>Registry URL</label>
                <input value={panelRegistry.registryUrl} onChange={(e) => updatePanelRegistry('registryUrl', e.target.value)} placeholder='http://localhost:5000' />
              </div>
              <div className='bs-form-row bs-form-row--two'>
                <div>
                  <label>Username / Service Account</label>
                  <input value={panelRegistry.username} onChange={(e) => updatePanelRegistry('username', e.target.value)} />
                </div>
                <div>
                  <label>Access Token / Password</label>
                  <input type='password' value={panelRegistry.secret} onChange={(e) => updatePanelRegistry('secret', e.target.value)} />
                </div>
              </div>
              <div className='bs-form-row'>
                <label>Repository Scope</label>
                <input value={panelRegistry.repositoryScope} onChange={(e) => updatePanelRegistry('repositoryScope', e.target.value)} placeholder='platform/catalog' />
              </div>
            </div>

            <div className='white-box'>
              <p className='white-box__section-header'>Sync Status</p>
              <div className='bs-form-row bs-form-row--two'>
                <div>
                  <label>Status</label>
                  <input value={panelRegistry.syncStatus} onChange={(e) => updatePanelRegistry('syncStatus', e.target.value)} />
                </div>
                <div>
                  <label>Last Indexed</label>
                  <input
                    readOnly
                    value={panelRegistry.lastSyncedAt ? new Date(panelRegistry.lastSyncedAt).toLocaleString() : 'Never'}
                    className='bs-field--readonly'
                  />
                </div>
              </div>
            </div>
          </>
        )}
      </SlidingPanel>
    </AppShell>
  )
}

function syncTone(status: string) {
  const s = (status || '').toLowerCase()
  if (s.includes('indexed') || s.includes('ready')) return 'ok'
  if (s.includes('pending')) return 'warn'
  return 'off'
}

function emptyRegistry(index: number): OCIRegistry {
  return {
    registryId: `registry-${index}`,
    name: `Registry ${index}`,
    provider: 'Generic OCI',
    registryUrl: '',
    username: '',
    secret: '',
    repositoryScope: '',
    region: '',
    allowLocalNetwork: false,
    syncStatus: 'Pending',
  }
}
