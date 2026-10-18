import { startTransition, useEffect, useMemo, useRef, useState } from 'react'
import {
  FaCircleCheck,
  FaCircleXmark,
  FaCloudArrowUp,
  FaDocker,
  FaPlus,
  FaServer,
  FaTrash,
} from 'react-icons/fa6'
import { create } from '@bufbuild/protobuf'
import { timestampDate } from '@bufbuild/protobuf/wkt'
import {
  ApiError,
  getPlatformSettings,
  listBackends,
  updatePlatformSettings,
  type APISIXSidecarConfig,
  type BackendOption,
  type ExecutionAgent,
  type PlatformSettings,
  type RuntimeAgent,
} from '../../lib/api'
import { agentRegistryClient } from '../../lib/connect'
import { ListAgentsRequestSchema, type Registration } from '../../types/proto/agent/v1/agent_pb'
import AppShell from '../../components/AppShell'
import { RowMenu } from '../../components/RowMenu'
import SlidingPanel from '../../components/SlidingPanel'
import '../PlatformSettings.css'

const AGENT_TYPES = [
  { value: 'local', label: 'Local (Default)' },
  { value: 'remote-agent', label: 'Remote Worker' },
  { value: 'remote-docker', label: 'Remote Docker' },
  { value: 'kubernetes', label: 'Kubernetes' },
] as const

export default function Agents() {
  const [savedSettings, setSavedSettings] = useState<PlatformSettings | null>(null)
  const [draft, setDraft] = useState<PlatformSettings | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [message, setMessage] = useState<{ tone: 'success' | 'error'; text: string } | null>(null)
  const [panelAgent, setPanelAgent] = useState<ExecutionAgent | null>(null)
  const [panelOpen, setPanelOpen] = useState(false)
  const [isNewAgent, setIsNewAgent] = useState(false)
  const [runtimeAgents, setRuntimeAgents] = useState<RuntimeAgent[]>([])
  const [backends, setBackends] = useState<BackendOption[]>([])

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

  useEffect(() => {
    let cancelled = false

    const loadRuntime = async () => {
      try {
        const response = await agentRegistryClient.list(create(ListAgentsRequestSchema))
        if (!cancelled) {
          startTransition(() => setRuntimeAgents(response.agents.map(registrationToRuntimeAgent)))
        }
      } catch {
        // ignore errors from cancelled or background load
      }
      try {
        const options = await listBackends()
        if (!cancelled) startTransition(() => setBackends(options))
      } catch {
        // capacity is informational; the table renders without it
      }
    }

    void loadRuntime()
    const timer = window.setInterval(() => { void loadRuntime() }, 10000)
    return () => {
      cancelled = true
      window.clearInterval(timer)
    }
  }, [])

  const patchDraft = (mutator: (next: PlatformSettings) => void) => {
    setDraft((current) => {
      if (!current) return current
      const next = structuredClone(current)
      mutator(next)
      return next
    })
  }

  const updatePanelAgent = <K extends keyof ExecutionAgent>(field: K, value: ExecutionAgent[K]) => {
    if (!panelAgent) return
    const updated = { ...panelAgent, [field]: value }
    setPanelAgent(updated)
    if (!isNewAgent) {
      patchDraft((next) => {
        next.agents = next.agents.map((a) => {
          if (a.agentId === panelAgent.agentId) return updated
          if (field === 'default' && value === true) return { ...a, default: false }
          return a
        })
      })
    }
  }

  const updatePanelSidecar = <K extends keyof APISIXSidecarConfig>(field: K, value: APISIXSidecarConfig[K]) => {
    if (!panelAgent) return
    const updated = {
      ...panelAgent,
      apisixSidecar: {
        ...panelAgent.apisixSidecar,
        [field]: value,
      },
    }
    setPanelAgent(updated)
    if (!isNewAgent) {
      patchDraft((next) => {
        next.agents = next.agents.map((agent) => agent.agentId === panelAgent.agentId ? updated : agent)
      })
    }
  }

  const openPanel = (agent: ExecutionAgent) => {
    setPanelAgent(structuredClone(agent))
    setIsNewAgent(false)
    setPanelOpen(true)
  }

  const closePanelTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  useEffect(() => () => { if (closePanelTimerRef.current !== null) clearTimeout(closePanelTimerRef.current) }, [])

  const closePanel = () => {
    setPanelOpen(false)
    setIsNewAgent(false)
    if (closePanelTimerRef.current !== null) clearTimeout(closePanelTimerRef.current)
    closePanelTimerRef.current = setTimeout(() => { closePanelTimerRef.current = null; setPanelAgent(null) }, 300)
  }

  const addAgent = () => {
    if (!draft) return
    const agent = emptyAgent(draft.agents.length + 1)
    setPanelAgent(agent)
    setIsNewAgent(true)
    setPanelOpen(true)
  }

  const removeAgent = (agentId: string) => {
    if (isNewAgent) { closePanel(); return }
    if (!draft || draft.agents.length === 1) return
    patchDraft((next) => { next.agents = next.agents.filter((a) => a.agentId !== agentId) })
    closePanel()
  }

  const save = async () => {
    if (!draft) return
    setSaving(true)
    setMessage(null)
    try {
      const payload = (isNewAgent && panelAgent)
        ? { ...draft, agents: [...draft.agents, panelAgent] }
        : draft
      const updated = await updatePlatformSettings(payload)
      startTransition(() => {
        setDraft(updated)
        setSavedSettings(updated)
        setIsNewAgent(false)
        setMessage({ tone: 'success', text: 'Execution agents saved.' })
      })
    } catch (reason) {
      setMessage({ tone: 'error', text: reason instanceof ApiError ? reason.message : 'Could not save settings.' })
    } finally {
      setSaving(false)
    }
  }

  const dirty = useMemo(
    () => isNewAgent || JSON.stringify(draft) !== JSON.stringify(savedSettings),
    [isNewAgent, draft, savedSettings],
  )

  if (loading) {
    return (
      <AppShell section='Settings' title='Execution Agents' description=''>
        <div className='platform-page platform-page--loading'>
          <div className='platform-loading-card'>
            <p className='platform-loading-card__eyebrow'>Settings</p>
            <h1>Loading execution agents…</h1>
          </div>
        </div>
      </AppShell>
    )
  }

  if (!draft) {
    return (
      <AppShell section='Settings' title='Execution Agents' description=''>
        <div className='platform-page'>
          {message && <div className={`platform-alert platform-alert--${message.tone}`}>{message.text}</div>}
          <div className='platform-loading-card'>
            <p className='platform-loading-card__eyebrow'>Settings</p>
            <h1>Execution agents unavailable</h1>
            <p>The frontend could not load platform settings from the backend.</p>
          </div>
        </div>
      </AppShell>
    )
  }

  const runtimeById = new Map(runtimeAgents.map((agent) => [agent.agentId, agent]))
  const backendById = new Map(backends.map((backend) => [backend.id, backend]))

  return (
    <AppShell
      section='Settings'
      sectionTo='/settings'
      title='Execution Agents'
      description='Configure the physical environments where suites execute and where the APISIX sidecar fronts API-facing suites.'
      actions={(
        <>
          <button type='button' className='platform-button platform-button--secondary' onClick={addAgent}>
            <FaPlus /> <span>Add Agent</span>
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
              <div className='bs-table-cell'>Type</div>
              <div className='bs-table-cell'>Status</div>
              <div className='bs-table-cell'>Capacity</div>
              <div className='bs-table-cell'>Capabilities</div>
              <div className='bs-table-cell'>Tags</div>
              <div className='bs-table-cell bs-table-cell--shrink' />
            </div>
          </div>

          {draft.agents.length === 0 && (
            <div className='bs-table-list__empty'>
              <FaServer className='bs-table-list__empty-icon' />
              <h4>No execution agents</h4>
              <p>Configure where your suites run and how the APISIX sidecar should front mock APIs on each agent.</p>
              <button type='button' className='platform-button' onClick={addAgent}><FaPlus /> Add Agent</button>
            </div>
          )}

          {draft.agents.map((agent) => (
            <div className='bs-table-list__row' key={agent.agentId} onClick={() => openPanel(agent)}>
              {(() => {
                const runtime = runtimeById.get(agent.agentId)
                const backend = backendById.get(agent.agentId)
                const displayStatus = reportedStatus(agent, runtime, backend)
                const heartbeatAt = runtime?.lastHeartbeatAt ?? agent.lastHeartbeatAt
                const sidecarProtocols = agent.apisixSidecar?.capabilities ?? []
                return (
              <div className='bs-table-row'>
                <div className='bs-table-cell bs-table-cell--shrink'>
                  {agent.enabled
                    ? <FaCircleCheck className='bs-status-icon bs-status-icon--ok' />
                    : <FaCircleXmark className='bs-status-icon bs-status-icon--off' />}
                </div>
                <div className='bs-table-cell'>
                  <strong>{agent.name}</strong>
                  {agent.default && <span className='bs-tag bs-tag--default'>default</span>}
                  {agent.description && <p className='bs-table-cell__sub'>{agent.description}</p>}
                  {heartbeatAt && <p className='bs-table-cell__sub'>Last heartbeat {new Date(heartbeatAt).toLocaleString()}</p>}
                </div>
                <div className='bs-table-cell'>
                  <span className='bs-tag'>{labelForAgentType(agent.type)}</span>
                </div>
                <div className='bs-table-cell'>
                  <span className={`bs-status-badge bs-status-badge--${statusTone(displayStatus)}`}>
                    {displayStatus}
                  </span>
                </div>
                <div className='bs-table-cell'>
                  {backend
                    ? <span className='bs-tag'>{capacityLabel(backend.capacity)}</span>
                    : <span className='bs-table-cell__muted' title='Not scheduling: the agent is disabled'>—</span>}
                </div>
                <div className='bs-table-cell'>
                  {runtime && runtime.capabilities.length > 0
                    ? runtime.capabilities.map((capability) => <span key={capability} className='bs-tag'>{capability}</span>)
                    : sidecarProtocols.length > 0
                      ? <span className='bs-table-cell__muted' title={sidecarProtocols.join(', ')}>{sidecarProtocols.length} sidecar protocols</span>
                      : <span className='bs-table-cell__muted'>—</span>}
                </div>
                <div className='bs-table-cell'>
                  {agent.routingTags.length > 0
                    ? agent.routingTags.map((tag) => <span key={tag} className='bs-tag'>{tag}</span>)
                    : <span className='bs-table-cell__muted'>—</span>}
                </div>
                <div className='bs-table-cell bs-table-cell--shrink'>
                  <RowMenu items={[
                    { label: 'Delete', danger: true, action: () => removeAgent(agent.agentId) },
                  ]} />
                </div>
              </div>
                )
              })()}
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
            <button type='button' className='platform-button platform-button--secondary' onClick={closePanel}>Cancel</button>
            <button
              type='button'
              className='platform-icon-button platform-header-spacer'
              onClick={() => panelAgent && removeAgent(panelAgent.agentId)}
              disabled={draft.agents.length <= 1}
              title='Remove agent'
            >
              <FaTrash />
            </button>
          </>
        )}
      >
        {panelAgent && (
          (() => {
            const runtime = runtimeById.get(panelAgent.agentId)
            const displayStatus = reportedStatus(panelAgent, runtime, backendById.get(panelAgent.agentId))
            const registeredAt = runtime?.registeredAt ?? panelAgent.registeredAt
            const lastHeartbeatAt = runtime?.lastHeartbeatAt ?? panelAgent.lastHeartbeatAt
            // Neither source is guaranteed: an agent that never registered at
            // runtime has no live entry, and the API omits an empty list.
            const runtimeCapabilities = runtime?.capabilities ?? panelAgent.runtimeCapabilities ?? []
            return (
          <>
            <div className='white-box'>
              <p className='white-box__section-header'>Identity</p>
              <div className='bs-form-row'>
                <label>Agent ID</label>
                <input value={panelAgent.agentId} onChange={(e) => updatePanelAgent('agentId', e.target.value)} />
              </div>
              <div className='bs-form-row'>
                <label>Display Name</label>
                <input value={panelAgent.name} onChange={(e) => updatePanelAgent('name', e.target.value)} />
              </div>
              <div className='bs-form-row'>
                <label>Description</label>
                <textarea value={panelAgent.description} onChange={(e) => updatePanelAgent('description', e.target.value)} />
              </div>
            </div>

            <div className='white-box'>
              <p className='white-box__section-header'>Configuration</p>
              <div className='bs-form-row bs-form-row--two'>
                <div>
                  <label>Agent Type</label>
                  <select value={panelAgent.type} onChange={(e) => updatePanelAgent('type', e.target.value as ExecutionAgent['type'])}>
                    {AGENT_TYPES.map((opt) => (
                      <option key={opt.value} value={opt.value}>{opt.label}</option>
                    ))}
                  </select>
                </div>
                <div>
                  {/* Reported by the agent — online on register and heartbeat,
                      offline when heartbeats stop — so it is shown, not edited:
                      a status typed here would only misreport the agent. */}
                  <label>Status <span className='bs-label-note'>(reported by the agent)</span></label>
                  <div className='bs-readonly-field'>
                    <span className={`bs-status-badge bs-status-badge--${statusTone(displayStatus)}`}>{displayStatus}</span>
                  </div>
                </div>
              </div>
              <div className='bs-form-row bs-form-row--two'>
                <div>
                  <label>Registered</label>
                  <input value={registeredAt ? new Date(registeredAt).toLocaleString() : 'Never'} readOnly />
                </div>
                <div>
                  <label>Last Heartbeat</label>
                  <input value={lastHeartbeatAt ? new Date(lastHeartbeatAt).toLocaleString() : 'Never'} readOnly />
                </div>
              </div>
              <div className='bs-form-row'>
                <label>Routing Tags <span className='bs-label-note'>(comma-separated — a launch with no backend chosen goes to an agent carrying every tag in its profile's agent.tags)</span></label>
                <input
                  value={panelAgent.routingTags.join(', ')}
                  onChange={(e) => updatePanelAgent('routingTags', splitList(e.target.value))}
                  placeholder='gpu-enabled, high-memory, ci-only'
                />
              </div>
              <div className='bs-form-row'>
                <label>Runtime Capabilities <span className='bs-label-note'>(reported when the agent registers)</span></label>
                <div className='bs-readonly-field'>
                  {runtimeCapabilities.length > 0
                    ? runtimeCapabilities.map((capability) => <span key={capability} className='bs-tag'>{capability}</span>)
                    : <span className='bs-table-cell__muted'>Not registered yet</span>}
                </div>
              </div>
              <div className='bs-form-checks'>
                <label>
                  <input type='checkbox' checked={panelAgent.enabled} onChange={(e) => updatePanelAgent('enabled', e.target.checked)} />
                  Enabled
                </label>
                <label>
                  <input type='checkbox' checked={panelAgent.default} onChange={(e) => updatePanelAgent('default', e.target.checked)} />
                  Default route
                </label>
              </div>
            </div>

            {panelAgent.type === 'local' && (
              <div className='white-box'>
                <p className='white-box__section-header'><FaDocker className='bs-section-header-icon' />Local Docker</p>
                <div className='bs-form-row'>
                  <label>Docker Socket</label>
                  <input value={panelAgent.dockerSocket} onChange={(e) => updatePanelAgent('dockerSocket', e.target.value)} />
                </div>
              </div>
            )}

            {panelAgent.type === 'remote-agent' && (
              <div className='white-box'>
                <p className='white-box__section-header'><FaCloudArrowUp className='bs-section-header-icon' />Remote Worker</p>
                <div className='bs-form-row'>
                  <label>Worker URL</label>
                  <input value={panelAgent.hostUrl} onChange={(e) => updatePanelAgent('hostUrl', e.target.value)} />
                </div>
                <div className='bs-form-row bs-form-row--two'>
                  <div>
                    <label>TLS Cert</label>
                    <input value={panelAgent.tlsCert} onChange={(e) => updatePanelAgent('tlsCert', e.target.value)} />
                  </div>
                  <div>
                    <label>TLS Key</label>
                    <input value={panelAgent.tlsKey} onChange={(e) => updatePanelAgent('tlsKey', e.target.value)} />
                  </div>
                </div>
              </div>
            )}

            {panelAgent.type === 'remote-docker' && (
              <div className='white-box'>
                <p className='white-box__section-header'><FaCloudArrowUp className='bs-section-header-icon' />Remote Docker</p>
                <div className='bs-form-row'>
                  <label>TCP Host URL</label>
                  <input value={panelAgent.hostUrl} onChange={(e) => updatePanelAgent('hostUrl', e.target.value)} />
                </div>
                <div className='bs-form-row bs-form-row--two'>
                  <div>
                    <label>TLS Cert</label>
                    <input value={panelAgent.tlsCert} onChange={(e) => updatePanelAgent('tlsCert', e.target.value)} />
                  </div>
                  <div>
                    <label>TLS Key</label>
                    <input value={panelAgent.tlsKey} onChange={(e) => updatePanelAgent('tlsKey', e.target.value)} />
                  </div>
                </div>
              </div>
            )}

            {panelAgent.type === 'kubernetes' && (
              <div className='white-box'>
                <p className='white-box__section-header'><FaCloudArrowUp className='bs-section-header-icon' />Kubernetes</p>
                <div className='bs-form-row'>
                  <label>Kubeconfig File Path</label>
                  <input value={panelAgent.kubeconfigPath} onChange={(e) => updatePanelAgent('kubeconfigPath', e.target.value)} />
                </div>
                <div className='bs-form-row bs-form-row--two'>
                  <div>
                    <label>Target Namespace</label>
                    <input value={panelAgent.targetNamespace} onChange={(e) => updatePanelAgent('targetNamespace', e.target.value)} />
                  </div>
                  <div>
                    <label>Service Account Token</label>
                    <input value={panelAgent.serviceAccountToken} onChange={(e) => updatePanelAgent('serviceAccountToken', e.target.value)} />
                  </div>
                </div>
              </div>
            )}

            <div className='white-box'>
              <p className='white-box__section-header'>APISIX Sidecar</p>
              <div className='bs-form-row'>
                <label>Runtime Image</label>
                <input value={panelAgent.apisixSidecar.image} onChange={(e) => updatePanelSidecar('image', e.target.value)} />
              </div>
              <div className='bs-form-row'>
                <label>Config Mount Path</label>
                <input value={panelAgent.apisixSidecar.configMountPath} onChange={(e) => updatePanelSidecar('configMountPath', e.target.value)} />
              </div>
              <div className='bs-form-row bs-form-row--two'>
                <div>
                  <label>Listen Port</label>
                  <input
                    type='number'
                    value={panelAgent.apisixSidecar.listenPort}
                    onChange={(e) => updatePanelSidecar('listenPort', parseNumberInput(e.target.value))}
                  />
                </div>
                <div>
                  <label>Admin Port</label>
                  <input
                    type='number'
                    value={panelAgent.apisixSidecar.adminPort}
                    onChange={(e) => updatePanelSidecar('adminPort', parseNumberInput(e.target.value))}
                  />
                </div>
              </div>
              <div className='bs-form-row'>
                <label>Capabilities <span className='bs-label-note'>(comma-separated)</span></label>
                <input
                  value={panelAgent.apisixSidecar.capabilities.join(', ')}
                  onChange={(e) => updatePanelSidecar('capabilities', splitList(e.target.value))}
                  placeholder='rest, graphql, grpc, soap, kafka'
                />
              </div>
            </div>
          </>
            )
          })()
        )}
      </SlidingPanel>
    </AppShell>
  )
}

function labelForAgentType(type: ExecutionAgent['type']) {
  switch (type) {
    case 'local': return 'Local Docker'
    case 'remote-agent': return 'Remote Worker'
    case 'remote-docker': return 'Remote Docker'
    case 'kubernetes': return 'Kubernetes'
    default: return type
  }
}

// reportedStatus is the agent's real state, never the placeholder stored in
// settings: a live registration's own status when the agent has one, else
// whether the control plane can reach the backend — local Docker and
// Kubernetes never register — and Disabled for one that is switched off.
function reportedStatus(agent: ExecutionAgent, runtime?: RuntimeAgent, backend?: BackendOption) {
  if (runtime?.status) return runtime.status
  if (!agent.enabled) return 'Disabled'
  if (backend) return backend.available ? 'Available' : 'Unreachable'
  return 'Not registered'
}

function statusTone(status: string) {
  const s = status.toLowerCase()
  if (s.includes('ready') || s.includes('indexed') || s.includes('online') || s === 'available') return 'ok'
  if (s.includes('pending') || s.includes('standby') || s.includes('not registered')) return 'warn'
  if (s.includes('disconnect') || s.includes('error') || s.includes('unreachable')) return 'err'
  if (s.includes('offline')) return 'off'
  return 'off'
}

function splitList(value: string) {
  return value.split(',').map((s) => s.trim()).filter(Boolean)
}

function parseNumberInput(value: string) {
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : 0
}

function emptyAgent(index: number): ExecutionAgent {
  return {
    agentId: `agent-${index}`,
    name: `New Agent ${index}`,
    type: 'local',
    description: '',
    enabled: true,
    default: false,
    status: 'Ready',
    routingTags: [],
    runtimeCapabilities: [],
    dockerSocket: '/var/run/docker.sock',
    hostUrl: '',
    tlsCert: '',
    tlsKey: '',
    kubeconfigPath: '',
    targetNamespace: '',
    serviceAccountToken: '',
    apisixSidecar: defaultAPISIXSidecar(),
  }
}

function registrationToRuntimeAgent(reg: Registration): RuntimeAgent {
  return {
    agentId: reg.agentId,
    name: reg.name,
    hostUrl: reg.hostUrl,
    status: reg.status,
    capabilities: [...reg.capabilities],
    registeredAt: reg.registeredAt ? timestampDate(reg.registeredAt).toISOString() : '',
    lastHeartbeatAt: reg.lastHeartbeatAt ? timestampDate(reg.lastHeartbeatAt).toISOString() : '',
    capacity: reg.capacity,
  }
}

// capacityLabel reads a backend's step limit, where 0 means the backend sets
// none — Kubernetes, which leaves it to the cluster scheduler.
function capacityLabel(capacity: number) {
  if (capacity <= 0) return 'Unlimited'
  return capacity === 1 ? '1 step at a time' : `${capacity} steps at a time`
}

function defaultAPISIXSidecar(): APISIXSidecarConfig {
  return {
    image: 'apache/apisix:latest',
    configMountPath: '/usr/local/apisix/conf/apisix.yaml',
    listenPort: 9080,
    adminPort: 9180,
    capabilities: ['rest', 'graphql', 'grpc', 'soap', 'kafka'],
  }
}
