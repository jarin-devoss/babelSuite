import { startTransition, useEffect, useMemo, useState } from 'react'
import {
  ApiError,
  getPlatformSettings,
  updatePlatformSettings,
  type PlatformSettings,
} from '../../lib/api'
import AppShell from '../../components/AppShell'
import '../PlatformSettings.css'

export default function Notifications() {
  const [savedSettings, setSavedSettings] = useState<PlatformSettings | null>(null)
  const [draft, setDraft] = useState<PlatformSettings | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [message, setMessage] = useState<{ tone: 'success' | 'error'; text: string } | null>(null)

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

  const patchSMTP = (field: string, value: string | number) => {
    setDraft((current) => {
      if (!current) return current
      const next = structuredClone(current)
      ;(next.notifications.smtp as Record<string, unknown>)[field] = value
      return next
    })
  }

  const save = async () => {
    if (!draft) return
    setSaving(true)
    setMessage(null)
    try {
      const updated = await updatePlatformSettings(draft)
      startTransition(() => {
        setDraft(updated)
        setSavedSettings(updated)
        setMessage({ tone: 'success', text: 'Notification settings saved.' })
      })
    } catch (reason) {
      setMessage({ tone: 'error', text: reason instanceof ApiError ? reason.message : 'Could not save settings.' })
    } finally {
      setSaving(false)
    }
  }

  const dirty = useMemo(
    () => JSON.stringify(draft) !== JSON.stringify(savedSettings),
    [draft, savedSettings],
  )

  if (loading) {
    return (
      <AppShell section='Settings' title='Notifications' description=''>
        <div className='platform-page platform-page--loading'>
          <div className='platform-loading-card'>
            <p className='platform-loading-card__eyebrow'>Settings</p>
            <h1>Loading notification settings…</h1>
          </div>
        </div>
      </AppShell>
    )
  }

  if (!draft) {
    return (
      <AppShell section='Settings' title='Notifications' description=''>
        <div className='platform-page'>
          {message && <div className={`platform-alert platform-alert--${message.tone}`}>{message.text}</div>}
          <div className='platform-loading-card'>
            <p className='platform-loading-card__eyebrow'>Settings</p>
            <h1>Notification settings unavailable</h1>
            <p>The frontend could not load platform settings from the backend.</p>
          </div>
        </div>
      </AppShell>
    )
  }

  const smtp = draft.notifications?.smtp ?? { host: '', port: 587, username: '', password: '', from: '' }

  return (
    <AppShell
      section='Settings'
      sectionTo='/settings'
      title='Notifications'
      description='Configure the outbound email server used by cron job reports. Changes take effect on the next scheduled run — no restart required.'
      actions={(
        <button type='button' className='platform-button' onClick={save} disabled={!dirty || saving}>
          {saving ? 'Saving…' : dirty ? 'Save Changes' : 'Saved'}
        </button>
      )}
    >
      <div className='platform-page'>
        {message && <div className={`platform-alert platform-alert--${message.tone}`}>{message.text}</div>}

        <div className='bs-settings-panel'>
          <p className='bs-settings-panel__header'>Outbound Email (SMTP)</p>

          <div className='bs-settings-panel__row'>
            <div className='bs-settings-panel__label'>SMTP Host</div>
            <div className='bs-settings-panel__control'>
              <input
                className='bs-field'
                value={smtp.host}
                onChange={(e) => patchSMTP('host', e.target.value)}
                placeholder='smtp.example.com'
              />
            </div>
          </div>

          <div className='bs-settings-panel__row'>
            <div className='bs-settings-panel__label'>Port</div>
            <div className='bs-settings-panel__control'>
              <input
                className='bs-field'
                type='number'
                value={smtp.port || ''}
                onChange={(e) => patchSMTP('port', parseInt(e.target.value, 10) || 0)}
                placeholder='587'
              />
            </div>
          </div>

          <div className='bs-settings-panel__row'>
            <div className='bs-settings-panel__label'>Username</div>
            <div className='bs-settings-panel__control'>
              <input
                className='bs-field'
                value={smtp.username}
                onChange={(e) => patchSMTP('username', e.target.value)}
                placeholder='notifications@example.com'
                autoComplete='off'
              />
            </div>
          </div>

          <div className='bs-settings-panel__row'>
            <div className='bs-settings-panel__label'>Password</div>
            <div className='bs-settings-panel__control'>
              <input
                className='bs-field'
                type='password'
                value={smtp.password}
                onChange={(e) => patchSMTP('password', e.target.value)}
                placeholder={savedSettings?.notifications?.smtp?.host ? '(unchanged)' : ''}
                autoComplete='new-password'
              />
            </div>
          </div>

          <div className='bs-settings-panel__row'>
            <div className='bs-settings-panel__label'>From Address</div>
            <div className='bs-settings-panel__control'>
              <input
                className='bs-field'
                value={smtp.from}
                onChange={(e) => patchSMTP('from', e.target.value)}
                placeholder='BabelSuite <no-reply@example.com>'
              />
            </div>
          </div>
        </div>
      </div>
    </AppShell>
  )
}
