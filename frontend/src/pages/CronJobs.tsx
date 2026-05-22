import { startTransition, useEffect, useRef, useState } from 'react'
import {
  FaBell,
  FaCircleCheck,
  FaCircleXmark,
  FaClock,
  FaEnvelope,
  FaPlus,
  FaToggleOff,
  FaToggleOn,
  FaTrash,
} from 'react-icons/fa6'
import {
  ApiError,
  createCronJob,
  deleteCronJob,
  listCronJobs,
  listExecutionLaunchSuites,
  updateCronJob,
  type CronEmailConfig,
  type CronJob,
  type CronSlackConfig,
  type CronSuiteTarget,
  type ExecutionLaunchSuite,
} from '../lib/api'
import AppShell from '../components/AppShell'
import { RowMenu } from '../components/RowMenu'
import SlidingPanel from '../components/SlidingPanel'
import './CronJobs.css'

function emptyJob(): Omit<CronJob, 'id' | 'lastRunAt' | 'nextRunAt' | 'lastError' | 'createdAt' | 'updatedAt'> {
  return {
    name: '',
    schedule: '0 9 * * 1',
    enabled: true,
    suites: [],
    email: { recipients: [], subject: '' },
    slack: { webhookUrl: '' },
  }
}

export default function CronJobs() {
  const [jobs, setJobs] = useState<CronJob[]>([])
  const [availableSuites, setAvailableSuites] = useState<ExecutionLaunchSuite[]>([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [message, setMessage] = useState<{ tone: 'success' | 'error'; text: string } | null>(null)
  const [panelOpen, setPanelOpen] = useState(false)
  const [isNew, setIsNew] = useState(false)
  const [draft, setDraft] = useState<CronJob | null>(null)
  const closePanelTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => () => { if (closePanelTimer.current) clearTimeout(closePanelTimer.current) }, [])

  useEffect(() => {
    let cancelled = false
    const load = async () => {
      setLoading(true)
      try {
        const [data, suites] = await Promise.all([listCronJobs(), listExecutionLaunchSuites()])
        if (!cancelled) {
          startTransition(() => setJobs(data))
          setAvailableSuites(suites)
        }
      } catch (reason) {
        if (!cancelled) setMessage({ tone: 'error', text: reason instanceof ApiError ? reason.message : 'Could not load cron jobs.' })
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    void load()
    return () => { cancelled = true }
  }, [])

  const patchDraft = <K extends keyof CronJob>(field: K, value: CronJob[K]) => {
    setDraft((curr) => curr ? { ...curr, [field]: value } : curr)
  }

  const patchEmail = <K extends keyof CronEmailConfig>(field: K, value: CronEmailConfig[K]) => {
    setDraft((curr) => curr ? { ...curr, email: { ...curr.email, [field]: value } } : curr)
  }

  const patchSlack = <K extends keyof CronSlackConfig>(field: K, value: CronSlackConfig[K]) => {
    setDraft((curr) => curr ? { ...curr, slack: { ...curr.slack, [field]: value } } : curr)
  }

  const addSuite = () => {
    const first = availableSuites[0]
    const defaultBackend = first?.backends.find((b) => b.default) ?? first?.backends[0]
    const target: CronSuiteTarget = {
      suiteId: first?.id ?? '',
      profile: first?.profiles[0]?.fileName ?? '',
      backendId: defaultBackend?.id ?? '',
    }
    setDraft((curr) => curr ? { ...curr, suites: [...curr.suites, target] } : curr)
  }

  const updateSuite = (idx: number, patch: Partial<CronSuiteTarget>) => {
    setDraft((curr) => {
      if (!curr) return curr
      const suites = curr.suites.map((s, i) => i === idx ? { ...s, ...patch } : s)
      return { ...curr, suites }
    })
  }

  const removeSuite = (idx: number) => {
    setDraft((curr) => curr ? { ...curr, suites: curr.suites.filter((_, i) => i !== idx) } : curr)
  }

  const openNew = () => {
    setDraft({ id: '', lastRunAt: undefined, nextRunAt: undefined, lastError: '', createdAt: '', updatedAt: '', ...emptyJob() })
    setIsNew(true)
    setPanelOpen(true)
  }

  const openEdit = (job: CronJob) => {
    setDraft(structuredClone(job))
    setIsNew(false)
    setPanelOpen(true)
  }

  const closePanel = () => {
    setPanelOpen(false)
    if (closePanelTimer.current) clearTimeout(closePanelTimer.current)
    closePanelTimer.current = setTimeout(() => { closePanelTimer.current = null; setDraft(null); setIsNew(false) }, 300)
  }

  const save = async () => {
    if (!draft) return
    setSaving(true)
    setMessage(null)
    try {
      const payload = { name: draft.name, schedule: draft.schedule, enabled: draft.enabled, suites: draft.suites, email: draft.email, slack: draft.slack }
      if (isNew) {
        const created = await createCronJob(payload)
        startTransition(() => setJobs((curr) => [...curr, created]))
      } else {
        const updated = await updateCronJob(draft.id, payload)
        startTransition(() => setJobs((curr) => curr.map((j) => j.id === updated.id ? updated : j)))
      }
      setMessage({ tone: 'success', text: isNew ? 'Cron job created.' : 'Cron job updated.' })
      closePanel()
    } catch (reason) {
      setMessage({ tone: 'error', text: reason instanceof ApiError ? reason.message : 'Could not save cron job.' })
    } finally {
      setSaving(false)
    }
  }

  const remove = async (id: string) => {
    setMessage(null)
    try {
      await deleteCronJob(id)
      startTransition(() => setJobs((curr) => curr.filter((j) => j.id !== id)))
      closePanel()
    } catch (reason) {
      setMessage({ tone: 'error', text: reason instanceof ApiError ? reason.message : 'Could not delete cron job.' })
    }
  }

  const recipientsStr = draft?.email.recipients.join(', ') ?? ''
  const setRecipients = (raw: string) => patchEmail('recipients', raw.split(',').map((s) => s.trim()).filter(Boolean))

  if (loading) {
    return (
      <AppShell section='Notifications' title='Cron Jobs' description=''>
        <div className='cronjobs-page cronjobs-page--loading'>
          <div className='cronjobs-loading-card'><h1>Loading cron jobs…</h1></div>
        </div>
      </AppShell>
    )
  }

  return (
    <AppShell
      section='Notifications'
      title='Cron Jobs'
      description='Schedule recurring suite runs with email and Slack notifications.'
      actions={<button type='button' className='cj-btn' onClick={openNew}><FaPlus /> <span>New Job</span></button>}
    >
      <div className='cronjobs-page'>
        {message && <div className={`cj-alert cj-alert--${message.tone}`}>{message.text}</div>}

        <div className='bs-table-list bs-table-list--clickable'>
          <div className='bs-table-list__head'>
            <div className='bs-table-row'>
              <div className='bs-table-cell bs-table-cell--shrink' />
              <div className='bs-table-cell'>Name</div>
              <div className='bs-table-cell'>Schedule</div>
              <div className='bs-table-cell'>Suites</div>
              <div className='bs-table-cell'>Channels</div>
              <div className='bs-table-cell'>Last Run</div>
              <div className='bs-table-cell'>Last Error</div>
              <div className='bs-table-cell bs-table-cell--shrink' />
            </div>
          </div>

          {jobs.length === 0 && (
            <div className='bs-table-list__empty'>
              <FaBell className='bs-table-list__empty-icon' />
              <h4>No cron jobs</h4>
              <p>Create a job to run suites on a schedule and send results via email or Slack.</p>
              <button type='button' className='cj-btn' onClick={openNew}><FaPlus /> New Job</button>
            </div>
          )}

          {jobs.map((job) => (
            <div className='bs-table-list__row' key={job.id} onClick={() => openEdit(job)}>
              <div className='bs-table-row'>
                <div className='bs-table-cell bs-table-cell--shrink'>
                  {job.enabled
                    ? <FaCircleCheck className='bs-status-icon bs-status-icon--ok' />
                    : <FaCircleXmark className='bs-status-icon bs-status-icon--off' />}
                </div>
                <div className='bs-table-cell'><strong>{job.name || '—'}</strong></div>
                <div className='bs-table-cell'><span className='bs-table-cell__mono'>{job.schedule}</span></div>
                <div className='bs-table-cell'>
                  <span className='bs-table-cell__muted'>{job.suites.length} suite{job.suites.length !== 1 ? 's' : ''}</span>
                </div>
                <div className='bs-table-cell'>
                  <span className='cj-channel-badges'>
                    {job.email.recipients.length > 0 && <span className='bs-tag'><FaEnvelope /> Email</span>}
                    {job.slack.webhookUrl && <span className='bs-tag'><FaBell /> Slack</span>}
                  </span>
                </div>
                <div className='bs-table-cell'>
                  {job.lastRunAt ? <span>{new Date(job.lastRunAt).toLocaleString()}</span> : <span className='bs-table-cell__muted'>Never</span>}
                </div>
                <div className='bs-table-cell'>
                  {job.lastError ? <span className='cj-error-snippet' title={job.lastError}>{job.lastError.slice(0, 60)}</span> : <span className='bs-table-cell__muted'>—</span>}
                </div>
                <div className='bs-table-cell bs-table-cell--shrink'>
                  <RowMenu items={[
                    { label: 'Edit', action: () => openEdit(job) },
                    { label: 'Delete', danger: true, action: () => void remove(job.id) },
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
            <button type='button' className='cj-btn' onClick={save} disabled={saving || !draft?.name}>
              {saving ? 'Saving…' : isNew ? 'Create' : 'Save'}
            </button>
            <button type='button' className='cj-btn cj-btn--secondary' onClick={closePanel}>Cancel</button>
            {!isNew && draft && (
              <button type='button' className='platform-icon-button platform-header-spacer' title='Delete job' onClick={() => void remove(draft.id)}>
                <FaTrash />
              </button>
            )}
          </>
        )}
      >
        {draft && (
          <>
            <div className='white-box'>
              <p className='white-box__section-header'>Identity</p>
              <div className='bs-form-row'>
                <label>Name</label>
                <input value={draft.name} onChange={(e) => patchDraft('name', e.target.value)} placeholder='Weekly report' />
              </div>
              <div className='bs-form-row bs-form-row--two'>
                <div>
                  <label>Cron Schedule</label>
                  <input value={draft.schedule} onChange={(e) => patchDraft('schedule', e.target.value)} placeholder='0 9 * * 1' className='bs-table-cell__mono' />
                  <p className='cj-hint'><FaClock /> minute hour day month weekday</p>
                </div>
                <div className='cj-toggle-row'>
                  <label>Enabled</label>
                  <button type='button' className={`cj-toggle${draft.enabled ? ' cj-toggle--on' : ''}`} onClick={() => patchDraft('enabled', !draft.enabled)}>
                    {draft.enabled ? <FaToggleOn /> : <FaToggleOff />}
                    <span>{draft.enabled ? 'Enabled' : 'Disabled'}</span>
                  </button>
                </div>
              </div>
            </div>

            <div className='white-box'>
              <p className='white-box__section-header'>Suites</p>
              {draft.suites.length === 0 && (
                <p className='cj-hint'>No suites added. Results will be empty when this job runs.</p>
              )}
              {draft.suites.map((target, idx) => {
                const suite = availableSuites.find((s) => s.id === target.suiteId)
                return (
                  <div key={idx} className='cj-suite-row'>
                    <div className='cj-suite-row__fields'>
                      <select
                        value={target.suiteId}
                        onChange={(e) => {
                          const s = availableSuites.find((x) => x.id === e.target.value)
                          const defaultBackend = s?.backends.find((b) => b.default) ?? s?.backends[0]
                          updateSuite(idx, { suiteId: e.target.value, profile: s?.profiles[0]?.fileName ?? '', backendId: defaultBackend?.id ?? '' })
                        }}
                      >
                        {availableSuites.length === 0 && <option value=''>No suites available</option>}
                        {availableSuites.map((s) => (
                          <option key={s.id} value={s.id}>{s.title}</option>
                        ))}
                      </select>
                      <select
                        value={target.profile}
                        onChange={(e) => updateSuite(idx, { profile: e.target.value })}
                        disabled={!suite || suite.profiles.length === 0}
                      >
                        {(suite?.profiles ?? []).map((p) => (
                          <option key={p.fileName} value={p.fileName}>{p.label || p.fileName}</option>
                        ))}
                        {(!suite || suite.profiles.length === 0) && <option value=''>default</option>}
                      </select>
                      <select
                        value={target.backendId}
                        onChange={(e) => updateSuite(idx, { backendId: e.target.value })}
                        disabled={!suite || suite.backends.length === 0}
                      >
                        {(suite?.backends ?? []).map((b) => (
                          <option key={b.id} value={b.id} disabled={!b.available}>
                            {b.label}{!b.available ? ' (unavailable)' : ''}
                          </option>
                        ))}
                        {(!suite || suite.backends.length === 0) && <option value=''>default agent</option>}
                      </select>
                    </div>
                    <button type='button' className='platform-icon-button' title='Remove suite' onClick={() => removeSuite(idx)}>
                      <FaTrash />
                    </button>
                  </div>
                )
              })}
              <button type='button' className='cj-btn cj-btn--secondary cj-btn--sm' onClick={addSuite} disabled={availableSuites.length === 0}>
                <FaPlus /> Add Suite
              </button>
            </div>

            <div className='white-box'>
              <p className='white-box__section-header'>Email</p>
              <div className='bs-form-row'>
                <label>Recipients <span className='bs-label-note'>(comma-separated)</span></label>
                <input value={recipientsStr} onChange={(e) => setRecipients(e.target.value)} placeholder='alice@example.com, bob@example.com' />
              </div>
              <div className='bs-form-row'>
                <label>Subject</label>
                <input value={draft.email.subject} onChange={(e) => patchEmail('subject', e.target.value)} placeholder='Weekly suite report' />
              </div>
            </div>

            <div className='white-box'>
              <p className='white-box__section-header'>Slack</p>
              <div className='bs-form-row'>
                <label>Webhook URL</label>
                <input value={draft.slack.webhookUrl} onChange={(e) => patchSlack('webhookUrl', e.target.value)} placeholder='https://hooks.slack.com/services/…' />
              </div>
            </div>

            {!isNew && (
              <div className='white-box'>
                <p className='white-box__section-header'>History</p>
                <div className='bs-form-row bs-form-row--two'>
                  <div>
                    <label>Last Run</label>
                    <input readOnly className='bs-field--readonly' value={draft.lastRunAt ? new Date(draft.lastRunAt).toLocaleString() : 'Never'} />
                  </div>
                  <div>
                    <label>Next Run</label>
                    <input readOnly className='bs-field--readonly' value={draft.nextRunAt ? new Date(draft.nextRunAt).toLocaleString() : '—'} />
                  </div>
                </div>
                {draft.lastError && (
                  <div className='bs-form-row'>
                    <label>Last Error</label>
                    <textarea readOnly className='bs-field--readonly cj-error-area' rows={3} value={draft.lastError} />
                  </div>
                )}
              </div>
            )}
          </>
        )}
      </SlidingPanel>
    </AppShell>
  )
}
