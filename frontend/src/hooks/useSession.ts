import { useEffect, useState } from 'react'
import { getSession, SESSION_CHANGED_EVENT } from '../lib/api'
import type { AuthResponse } from '../lib/api'

export function useSession(): AuthResponse | null {
  const [session, setSession] = useState<AuthResponse | null>(() => getSession())

  useEffect(() => {
    const refresh = () => setSession(getSession())
    window.addEventListener(SESSION_CHANGED_EVENT, refresh)
    window.addEventListener('storage', refresh)
    return () => {
      window.removeEventListener(SESSION_CHANGED_EVENT, refresh)
      window.removeEventListener('storage', refresh)
    }
  }, [])

  return session
}
