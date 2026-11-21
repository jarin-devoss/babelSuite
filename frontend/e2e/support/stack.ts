import { execFileSync, spawn, type ChildProcess } from 'node:child_process'
import { existsSync, readFileSync, writeFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { AUTH_DIR, readSession } from './session'

const here = dirname(fileURLToPath(import.meta.url))
const FLAG = join(AUTH_DIR, 'restart.flag')
const PID_FILE = join(AUTH_DIR, 'server.pid')
const BIN = resolve(here, '..', '.stack', process.platform === 'win32' ? 'babelsuite-server.exe' : 'babelsuite-server')

async function ready(url: string, timeoutMs: number): Promise<void> {
  const deadline = Date.now() + timeoutMs
  while (Date.now() < deadline) {
    try {
      if ((await fetch(`${url}/readyz`)).ok) {
        return
      }
    } catch {
      // not up yet
    }
    await new Promise((r) => setTimeout(r, 500))
  }
  throw new Error(`${url} was not ready within ${timeoutMs} ms`)
}

/**
 * Restarts the control plane that Playwright started. stack/backend.mjs
 * respawns the server when it exits while restart.flag exists, with the same
 * database and settings, so what survives a restart is what is really stored.
 */
export async function restartControlPlane(): Promise<void> {
  const session = readSession()
  const pid = Number(readFileSync(PID_FILE, 'utf-8'))
  writeFileSync(FLAG, 'restart')
  if (process.platform === 'win32') {
    execFileSync('taskkill', ['/PID', String(pid), '/F'], { stdio: 'ignore' })
  } else {
    process.kill(pid, 'SIGKILL')
  }
  // The old server is gone; wait for the respawned one to answer.
  await new Promise((r) => setTimeout(r, 1_500))
  await ready(session.apiUrl, 90_000)
}

/** Starts a second control plane on another port against the same database. */
export async function startSecondControlPlane(port = 8091): Promise<{ url: string; stop: () => void }> {
  const session = readSession()
  if (!existsSync(BIN)) {
    throw new Error(`server binary not found at ${BIN}`)
  }
  const child: ChildProcess = spawn(BIN, [], {
    cwd: session.workDir,
    stdio: 'ignore',
    env: {
      ...process.env,
      PORT: String(port),
      JWT_SECRET: session.jwtSecret,
      ADMIN_EMAIL: session.email,
      ADMIN_PASSWORD: session.password,
      MONGO_URI: session.mongoUri,
      MONGO_DB: session.mongoDb,
      PLATFORM_SETTINGS_FILE: join(session.workDir, 'configuration.yaml'),
      PROFILES_FILE: join(session.workDir, 'babelsuite-profiles.yaml'),
      AGENT_RUNTIME_FILE: join(session.workDir, 'babelsuite-agents-b.yaml'),
      PUBLIC_API_URL: `http://127.0.0.1:${port}`,
      REDIS_ADDR: '',
    },
  })
  const url = `http://127.0.0.1:${port}`
  await ready(url, 90_000)
  return { url, stop: () => void child.kill() }
}
