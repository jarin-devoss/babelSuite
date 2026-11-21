import { appendFileSync, mkdirSync } from 'node:fs'
import { request, type APIRequestContext } from '@playwright/test'
import { existsSync, readFileSync } from 'node:fs'
import { ADMIN_TOKEN_FILE, AUTH_DIR, RUNS_FILE, USER_TOKEN_FILE, readSession } from '../support/session'

export interface RunEvent {
  source: string
  status: string
  text: string
  level: string
}

export interface RunArtifact {
  name: string
  stepName: string
  format?: string
  testSummary?: { total: number; passed: number; failures: number; errors: number; skipped: number }
}

export interface RunRecord {
  id: string
  profile: string
  status: string
  trigger: string
  suite: { id: string; title: string }
  events: RunEvent[]
  artifacts?: RunArtifact[]
}

export interface RunSummary {
  id: string
  suiteId: string
  profile: string
  status: string
  trigger: string
}

export interface Environment {
  sandboxId: string
  runId: string
  suite: string
  profile: string
  owner: string
  status: string
  orchestratorPid?: number
  containers: { name: string }[]
  networks: { name: string }[]
}

export interface PlatformSettings {
  mode?: string
  description?: string
  agents: { agentId: string; name?: string; type: string; enabled?: boolean; hostUrl?: string }[]
  registries: { registryId: string; name?: string; registryUrl?: string; repositoryScope?: string; syncStatus?: string }[]
  secrets: { provider: string; globalOverrides: { key: string; value: string; description?: string; sensitive?: boolean }[] }
  notifications?: { smtp?: { host?: string; port?: number; username?: string; password?: string; from?: string } }
  plugins: { name: string; trigger?: string; operations?: string[]; lua?: string; star?: string }[]
}

const TERMINAL = new Set(['healthy', 'failed', 'cancelled', 'error', 'completed', 'success', 'passed'])

/** The final status of each step, derived from its events (the record has no steps key). */
export function stepStatuses(record: RunRecord): Record<string, string> {
  const out: Record<string, string> = {}
  for (const event of record.events ?? []) {
    if (event.source && event.status) {
      out[event.source] = event.status.toLowerCase()
    }
  }
  return out
}

/**
 * A signed-in admin client for the control plane. It exists to prepare state
 * and to read back what the UI is supposed to have changed, never to stand in
 * for a UI action under test.
 */
export class Api {
  private constructor(
    readonly http: APIRequestContext,
    readonly baseUrl: string,
    readonly token: string,
  ) {}

  /** Signs in over HTTP: fetches the CSRF cookie first, then trades credentials for a Bearer token. */
  static async signIn(email: string, password: string, baseUrl = readSession().apiUrl): Promise<Api> {
    const anonymous = await request.newContext({ baseURL: baseUrl })
    await anonymous.get('/api/v1/auth/config')
    const cookies = (await anonymous.storageState()).cookies
    const csrf = cookies.find((c) => c.name === 'csrf_token')?.value ?? ''
    const response = await anonymous.post('/api/v1/auth/sign-in', {
      data: { email, password },
      headers: { 'X-CSRF-Token': csrf },
    })
    if (!response.ok()) {
      throw new Error(`sign-in as ${email} failed: ${response.status()} ${await response.text()}`)
    }
    const body = (await response.json()) as { token?: string }
    await anonymous.dispose()
    if (!body.token) {
      throw new Error('sign-in returned no token')
    }
    const http = await request.newContext({
      baseURL: baseUrl,
      extraHTTPHeaders: { Authorization: `Bearer ${body.token}` },
    })
    return new Api(http, baseUrl, body.token)
  }

  /** A client for a token that is already held, so no sign-in (and no rate limit) is spent. */
  static async fromToken(token: string, baseUrl = readSession().apiUrl): Promise<Api> {
    const http = await request.newContext({
      baseURL: baseUrl,
      extraHTTPHeaders: { Authorization: `Bearer ${token}` },
    })
    return new Api(http, baseUrl, token)
  }

  /**
   * The admin client. The sign-in endpoint allows ten attempts a minute per
   * address, so the token the setup project obtained is reused; only when it
   * is missing does this sign in.
   */
  static admin(): Promise<Api> {
    if (existsSync(ADMIN_TOKEN_FILE)) {
      return Api.fromToken(readFileSync(ADMIN_TOKEN_FILE, 'utf-8').trim())
    }
    const session = readSession()
    return Api.signIn(session.email, session.password, session.apiUrl)
  }

  /** The non-admin user created by the setup project. */
  static user(): Promise<Api> {
    return Api.fromToken(readFileSync(USER_TOKEN_FILE, 'utf-8').trim())
  }

  async dispose(): Promise<void> {
    await this.http.dispose()
  }

  async get<T>(path: string): Promise<T> {
    const response = await this.http.get(path)
    if (!response.ok()) {
      throw new Error(`GET ${path} -> ${response.status()} ${await response.text()}`)
    }
    return (await response.json()) as T
  }

  async send<T = unknown>(method: 'POST' | 'PUT' | 'DELETE', path: string, data?: unknown): Promise<T | undefined> {
    const response = await this.http.fetch(path, { method, data })
    if (!response.ok()) {
      throw new Error(`${method} ${path} -> ${response.status()} ${await response.text()}`)
    }
    const text = await response.text()
    return text ? (JSON.parse(text) as T) : undefined
  }

  /** The status code of a request, for assertions about forbidden or missing resources. */
  async status(method: 'GET' | 'POST' | 'PUT' | 'DELETE', path: string, data?: unknown): Promise<number> {
    const response = await this.http.fetch(path, { method, data })
    return response.status()
  }

  // ── executions ────────────────────────────────────────────────────────────

  /** Launches a run and records its id so teardown can reap it. */
  async launch(suiteId: string, profile = 'local.yaml'): Promise<string> {
    const created = await this.send<{ id: string }>('POST', '/api/v1/executions', { suiteId, profile })
    if (!created?.id) {
      throw new Error(`launching ${suiteId} returned no id`)
    }
    recordRun(created.id)
    return created.id
  }

  run(id: string): Promise<RunRecord> {
    return this.get<RunRecord>(`/api/v1/executions/${encodeURIComponent(id)}`)
  }

  async runs(): Promise<RunSummary[]> {
    const body = await this.get<RunSummary[] | { executions: RunSummary[] }>('/api/v1/executions')
    return Array.isArray(body) ? body : (body.executions ?? [])
  }

  /** Polls until the run reaches a terminal status; returns the record. */
  async waitForRun(id: string, timeoutMs = 180_000): Promise<RunRecord> {
    const deadline = Date.now() + timeoutMs
    let last: RunRecord | undefined
    while (Date.now() < deadline) {
      last = await this.run(id)
      if (TERMINAL.has(last.status.toLowerCase())) {
        return last
      }
      await new Promise((resolve) => setTimeout(resolve, 1500))
    }
    const tail = (last?.events ?? []).slice(-20).map((e) => `[${e.source}] ${e.text}`).join('\n')
    throw new Error(`run ${id} did not finish within ${timeoutMs} ms (status ${last?.status}); last events:\n${tail}`)
  }

  async logs(id: string): Promise<string[]> {
    const body = await this.get<unknown>(`/api/v1/executions/${encodeURIComponent(id)}/logs/snapshot`)
    const items = Array.isArray(body) ? body : ((body as { lines?: unknown[]; logs?: unknown[] }).lines ?? (body as { logs?: unknown[] }).logs ?? [])
    return items.map((item) =>
      typeof item === 'string' ? item : String((item as { text?: string; message?: string }).text ?? (item as { message?: string }).message ?? ''),
    )
  }

  // ── platform settings ─────────────────────────────────────────────────────

  settings(): Promise<PlatformSettings> {
    return this.get<PlatformSettings>('/api/v1/platform-settings')
  }

  /** Replaces the settings. Used to restore a snapshot after a test changed them. */
  async putSettings(settings: PlatformSettings): Promise<void> {
    await this.send('PUT', '/api/v1/platform-settings', settings)
  }

  // ── environments ──────────────────────────────────────────────────────────

  async environments(): Promise<Environment[]> {
    const body = await this.get<Environment[] | { sandboxes: Environment[] }>('/api/v1/sandboxes')
    return Array.isArray(body) ? body : (body.sandboxes ?? [])
  }

  async reap(sandboxId: string): Promise<void> {
    await this.http.post(`/api/v1/sandboxes/${encodeURIComponent(sandboxId)}/reap`)
  }
}

/** Appends a run id to the session's run log; teardown reaps every id in it. */
export function recordRun(id: string): void {
  mkdirSync(AUTH_DIR, { recursive: true })
  appendFileSync(RUNS_FILE, `${id}\n`)
}
