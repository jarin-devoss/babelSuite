import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))

/** Facts about the throw-away control plane, written by stack/backend.mjs. */
export interface StackSession {
  email: string
  password: string
  mongoUri: string
  mongoDb: string
  /** The registry holding the fixture suites; 5001 locally, the job's own in CI. */
  registryUrl: string
  apiUrl: string
  jwtSecret: string
  workDir: string
  startedAt: string
}

export const AUTH_DIR = resolve(here, '..', '.auth')
export const SESSION_FILE = resolve(AUTH_DIR, 'session.json')
export const RUNS_FILE = resolve(AUTH_DIR, 'runs.jsonl')
export const VISITED_FILE = resolve(AUTH_DIR, 'visited.jsonl')
export const ADMIN_STATE = resolve(AUTH_DIR, 'admin.json')
export const USER_STATE = resolve(AUTH_DIR, 'user.json')
export const ADMIN_TOKEN_FILE = resolve(AUTH_DIR, 'admin-token.txt')
export const USER_TOKEN_FILE = resolve(AUTH_DIR, 'user-token.txt')
export const USER_FILE = resolve(AUTH_DIR, 'user-credentials.json')

export function readSession(): StackSession {
  return JSON.parse(readFileSync(SESSION_FILE, 'utf-8')) as StackSession
}

/** host:port of the fixture registry, as a suite reference spells it. */
export function registryHost(): string {
  return new URL(readSession().registryUrl).host
}

/** The suites pushed from e2e/suites, by id. */
export const SUITES = {
  pass: 'ui-pass',
  fail: 'ui-fail',
  continue: 'ui-continue',
  junit: 'ui-junit',
  junitMissing: 'ui-junit-missing',
  junitEmpty: 'ui-junit-empty',
  junitInvalid: 'ui-junit-invalid',
  junitSoft: 'ui-junit-soft',
  ctrf: 'ui-ctrf',
  slow: 'ui-slow',
  env: 'ui-env',
  mock: 'ui-mock',
} as const
