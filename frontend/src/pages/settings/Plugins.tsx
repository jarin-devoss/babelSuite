import { useEffect, useRef, useState } from 'react'
import { FaCircleCheck, FaCircleXmark, FaCubesStacked, FaPlus, FaTrash } from 'react-icons/fa6'
import {
  ApiError,
  checkPlugin,
  createPlugin,
  deletePlugin,
  listPlugins,
  type CustomPlugin,
} from '../../lib/api'
import AppShell from '../../components/AppShell'
import { RowMenu } from '../../components/RowMenu'
import SlidingPanel from '../../components/SlidingPanel'
import '../PlatformSettings.css'

const EMPTY_PLUGIN: CustomPlugin = {
  name: '',
  trigger: '',
  kind: 'plugin',
  variants: [],
  operations: [],
  lua: '',
  star: '',
  schema: '',
  version: '',
}

export default function Plugins() {
  const [plugins, setPlugins] = useState<CustomPlugin[]>([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [message, setMessage] = useState<{ tone: 'success' | 'error'; text: string } | null>(null)
  const [panelPlugin, setPanelPlugin] = useState<CustomPlugin | null>(null)
  const [panelOpen, setPanelOpen] = useState(false)
  const [checkResults, setCheckResults] = useState<Record<string, { ok: boolean; issues?: string[] }>>({})

  useEffect(() => {
    let cancelled = false
    const load = async () => {
      setLoading(true)
      try {
        const list = await listPlugins()
        if (cancelled) return
        setPlugins(list)
      } catch (reason) {
        if (cancelled) return
        setMessage({ tone: 'error', text: reason instanceof ApiError ? reason.message : 'Could not load plugins.' })
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    void load()
    return () => { cancelled = true }
  }, [])

  const closePanelTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  useEffect(() => () => { if (closePanelTimerRef.current !== null) clearTimeout(closePanelTimerRef.current) }, [])

  const closePanel = () => {
    setPanelOpen(false)
    if (closePanelTimerRef.current !== null) clearTimeout(closePanelTimerRef.current)
    closePanelTimerRef.current = setTimeout(() => { closePanelTimerRef.current = null; setPanelPlugin(null) }, 300)
  }

  const openAddPanel = () => {
    setPanelPlugin(structuredClone(EMPTY_PLUGIN))
    setPanelOpen(true)
  }

  const updatePanel = <K extends keyof CustomPlugin>(field: K, value: CustomPlugin[K]) => {
    setPanelPlugin((cur) => cur ? { ...cur, [field]: value } : cur)
  }

  const save = async () => {
    if (!panelPlugin) return
    setSaving(true)
    setMessage(null)
    try {
      const created = await createPlugin(panelPlugin)
      setPlugins((prev) => [...prev, created])
      setMessage({ tone: 'success', text: `Plugin "${created.name}" registered.` })
      closePanel()
    } catch (reason) {
      setMessage({ tone: 'error', text: reason instanceof ApiError ? reason.message : 'Could not register plugin.' })
    } finally {
      setSaving(false)
    }
  }

  const remove = async (name: string) => {
    try {
      await deletePlugin(name)
      setPlugins((prev) => prev.filter((p) => p.name !== name))
      setCheckResults((prev) => { const next = { ...prev }; delete next[name]; return next })
      setMessage({ tone: 'success', text: `Plugin "${name}" removed.` })
      closePanel()
    } catch (reason) {
      setMessage({ tone: 'error', text: reason instanceof ApiError ? reason.message : 'Could not remove plugin.' })
    }
  }

  const runCheck = async (name: string) => {
    try {
      const result = await checkPlugin(name)
      setCheckResults((prev) => ({ ...prev, [name]: result }))
    } catch {
      setCheckResults((prev) => ({ ...prev, [name]: { ok: false, issues: ['Check request failed.'] } }))
    }
  }

  if (loading) {
    return (
      <AppShell section='Settings' title='Plugins' description=''>
        <div className='platform-page platform-page--loading'>
          <div className='platform-loading-card'>
            <p className='platform-loading-card__eyebrow'>Settings</p>
            <h1>Loading plugins…</h1>
          </div>
        </div>
      </AppShell>
    )
  }

  return (
    <AppShell
      section='Settings'
      sectionTo='/settings'
      title='Plugins'
      description='Register user-authored APISIX Lua plugins. Each plugin is embedded into the gateway sidecar and callable from suite steps via plugin.run().'
      actions={(
        <button type='button' className='platform-button platform-button--secondary' onClick={openAddPanel}>
          <FaPlus /> <span>Register Plugin</span>
        </button>
      )}
    >
      <div className='platform-page'>
        {message && <div className={`platform-alert platform-alert--${message.tone}`}>{message.text}</div>}

        <div className='bs-table-list bs-table-list--clickable'>
          <div className='bs-table-list__head'>
            <div className='bs-table-row'>
              <div className='bs-table-cell bs-table-cell--shrink' />
              <div className='bs-table-cell'>Name</div>
              <div className='bs-table-cell'>Trigger Route</div>
              <div className='bs-table-cell'>Kind</div>
              <div className='bs-table-cell'>Operations</div>
              <div className='bs-table-cell bs-table-cell--shrink' />
            </div>
          </div>

          {plugins.length === 0 && (
            <div className='bs-table-list__empty'>
              <FaCubesStacked className='bs-table-list__empty-icon' />
              <h4>No plugins registered</h4>
              <p>Register a Lua plugin to extend the APISIX gateway sidecar with custom step types.</p>
              <button type='button' className='platform-button' onClick={openAddPanel}><FaPlus /> Register Plugin</button>
            </div>
          )}

          {plugins.map((plugin) => {
            const check = checkResults[plugin.name]
            return (
              <div className='bs-table-list__row' key={plugin.name}>
                <div className='bs-table-row'>
                  <div className='bs-table-cell bs-table-cell--shrink'>
                    {check
                      ? check.ok
                        ? <FaCircleCheck className='bs-status-icon bs-status-icon--ok' />
                        : <FaCircleXmark className='bs-status-icon bs-status-icon--err' />
                      : <span className='bs-status-icon bs-status-icon--off' />}
                  </div>
                  <div className='bs-table-cell'>
                    <strong>{plugin.name}</strong>
                    {plugin.version && <p className='bs-table-cell__sub'>v{plugin.version}</p>}
                    {plugin.deprecated && <span className='bs-tag bs-tag--warn'>deprecated</span>}
                  </div>
                  <div className='bs-table-cell'>
                    <span className='bs-table-cell__mono'>{plugin.trigger}</span>
                  </div>
                  <div className='bs-table-cell'>
                    <span className='bs-tag'>{plugin.kind || 'plugin'}</span>
                  </div>
                  <div className='bs-table-cell'>
                    {plugin.operations && plugin.operations.length > 0
                      ? plugin.operations.map((op) => <span key={op} className='bs-tag'>{op}</span>)
                      : <span className='bs-table-cell__muted'>any</span>}
                  </div>
                  <div className='bs-table-cell bs-table-cell--shrink'>
                    <RowMenu items={[
                      { label: 'Check', action: () => void runCheck(plugin.name) },
                      { label: 'Delete', danger: true, action: () => void remove(plugin.name) },
                    ]} />
                  </div>
                </div>
                {check && !check.ok && check.issues && check.issues.length > 0 && (
                  <div className='bs-table-list__issues'>
                    {check.issues.map((issue, i) => (
                      <span key={i} className='bs-table-list__issue'>{issue}</span>
                    ))}
                  </div>
                )}
              </div>
            )
          })}
        </div>
      </div>

      <SlidingPanel
        isOpen={panelOpen}
        onClose={closePanel}
        header={(
          <>
            <button type='button' className='platform-button' onClick={save} disabled={saving || !panelPlugin?.name || !panelPlugin?.trigger || !panelPlugin?.lua}>
              {saving ? 'Registering…' : 'Register'}
            </button>
            <button type='button' className='platform-button platform-button--secondary' onClick={closePanel}>Cancel</button>
          </>
        )}
      >
        {panelPlugin && (
          <div className='white-box'>
            <p className='white-box__section-header'>Identity</p>
            <div className='bs-form-row bs-form-row--two'>
              <div>
                <label>Plugin Name <span className='bs-form-required'>*</span></label>
                <input
                  value={panelPlugin.name}
                  onChange={(e) => updatePanel('name', e.target.value)}
                  placeholder='spice-sim'
                />
              </div>
              <div>
                <label>Version</label>
                <input
                  value={panelPlugin.version ?? ''}
                  onChange={(e) => updatePanel('version', e.target.value)}
                  placeholder='1.0.0'
                />
              </div>
            </div>
            <div className='bs-form-row bs-form-row--two'>
              <div>
                <label>Kind</label>
                <input
                  value={panelPlugin.kind}
                  onChange={(e) => updatePanel('kind', e.target.value)}
                  placeholder='plugin'
                />
              </div>
              <div>
                <label>Trigger Route <span className='bs-form-required'>*</span></label>
                <input
                  value={panelPlugin.trigger}
                  onChange={(e) => updatePanel('trigger', e.target.value)}
                  placeholder='/_babelsuite/plugins/spice-sim/start'
                />
              </div>
            </div>
            <div className='bs-form-row bs-form-row--two'>
              <div>
                <label>Variants <span className='bs-form-hint'>(comma-separated)</span></label>
                <input
                  value={(panelPlugin.variants ?? []).join(', ')}
                  onChange={(e) => updatePanel('variants', e.target.value.split(',').map((s) => s.trim()).filter(Boolean))}
                  placeholder='spice-sim'
                />
              </div>
              <div>
                <label>Operations <span className='bs-form-hint'>(comma-separated)</span></label>
                <input
                  value={(panelPlugin.operations ?? []).join(', ')}
                  onChange={(e) => updatePanel('operations', e.target.value.split(',').map((s) => s.trim()).filter(Boolean))}
                  placeholder='run, watch'
                />
              </div>
            </div>

            <p className='white-box__section-header'>Source</p>
            <div className='bs-form-row'>
              <label>Lua Plugin Source <span className='bs-form-required'>*</span></label>
              <textarea
                className='bs-field bs-field--code'
                rows={12}
                value={panelPlugin.lua}
                onChange={(e) => updatePanel('lua', e.target.value)}
                placeholder={'local _M = {}\n_M.version  = 0.1\n_M.priority = 10\n_M.name     = "my-plugin"\n_M.schema   = { type = "object", properties = {}, additionalProperties = false }\n\nfunction _M.access(conf, ctx)\n  -- plugin logic here\nend\n\nreturn _M'}
                spellCheck={false}
              />
            </div>
            <div className='bs-form-row'>
              <label>Starlark Module Source <span className='bs-form-hint'>(optional — exposes plugin() calls in suite.star)</span></label>
              <textarea
                className='bs-field bs-field--code'
                rows={6}
                value={panelPlugin.star ?? ''}
                onChange={(e) => updatePanel('star', e.target.value)}
                placeholder='def run(name, **kwargs):\n    return plugin.run(name=name, plugin="my-plugin", op="run", **kwargs)'
                spellCheck={false}
              />
            </div>
            <div className='bs-form-row'>
              <label>CUE Schema <span className='bs-form-hint'>(optional — validates step config before dispatch)</span></label>
              <textarea
                className='bs-field bs-field--code'
                rows={6}
                value={panelPlugin.schema ?? ''}
                onChange={(e) => updatePanel('schema', e.target.value)}
                placeholder='{\n  sim_url?: string\n  timeout_ms?: number\n}'
                spellCheck={false}
              />
            </div>

            <p className='white-box__section-header'>Options</p>
            <div className='bs-form-row'>
              <label className='bs-checkbox-label'>
                <input
                  type='checkbox'
                  checked={panelPlugin.deprecated ?? false}
                  onChange={(e) => updatePanel('deprecated', e.target.checked)}
                />
                Mark as deprecated
              </label>
            </div>

            <div className='bs-form-row bs-form-row--danger'>
              <button type='button' className='platform-button platform-button--danger' onClick={closePanel}>
                <FaTrash /> Discard
              </button>
            </div>
          </div>
        )}
      </SlidingPanel>
    </AppShell>
  )
}
