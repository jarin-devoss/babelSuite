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
  updateCronJob,
  type CronEmailConfig,
  type CronJob,
  type CronSlackConfig,
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
    email: { recipients: [], subject: '', body: '' },
    slack: { webhookUrl: '', message: '' },
  }
}

export default function CronJobs() {
  const [jobs, setJobs] = useState<CronJob[]>([])
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
        const data = await listCronJobs()
        if (!cancelled) startTransition(() => setJobs(data))
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
      const payload = { name: draft.name, schedule: draft.schedule, enabled: draft.enabled, email: draft.email, slack: draft.slack }
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
      description='Schedule recurring email and Slack notifications.'
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
              <p>Create a job to send scheduled email or Slack notifications.</p>
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
              <p className='white-box__section-header'>Email</p>
              <div className='bs-form-row'>
                <label>Recipients <span className='bs-label-note'>(comma-separated)</span></label>
                <input value={recipientsStr} onChange={(e) => setRecipients(e.target.value)} placeholder='alice@example.com, bob@example.com' />
              </div>
              <div className='bs-form-row'>
                <label>Subject</label>
                <input value={draft.email.subject} onChange={(e) => patchEmail('subject', e.target.value)} placeholder='Weekly digest' />
              </div>
              <div className='bs-form-row'>
                <label>Body</label>
                <textarea value={draft.email.body} onChange={(e) => patchEmail('body', e.target.value)} rows={5} placeholder='Hello, here is your weekly report…' />
              </div>
            </div>

            <div className='white-box'>
              <p className='white-box__section-header'>Slack</p>
              <div className='bs-form-row'>
                <label>Webhook URL</label>
                <input value={draft.slack.webhookUrl} onChange={(e) => patchSlack('webhookUrl', e.target.value)} placeholder='https://hooks.slack.com/services/…' />
              </div>
              <div className='bs-form-row'>
                <label>Message</label>
                <textarea value={draft.slack.message} onChange={(e) => patchSlack('message', e.target.value)} rows={4} placeholder='Hello from BabelSuite!' />
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
