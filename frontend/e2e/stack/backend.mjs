// Starts a throw-away BabelSuite control plane for the Playwright run.
//
// It pushes the fixture suites from e2e/suites into the registry, writes a
// temporary platform configuration, picks a unique Mongo database and a random
// admin password, then runs the server in the foreground. Playwright's
// `webServer` keeps it alive for the whole run and stops it afterwards; the
// teardown in global-teardown.ts removes what the run left behind.
//
// The session facts every other file needs (admin credentials, database name,
// temp directory) are written to e2e/.auth/session.json.
//
// The sidecar reaches the control plane at host.docker.internal:8090, so the
// server has to listen on 8090 for mock-backed steps to work. Set E2E_API_PORT
// to move it anyway when you do not run any suite that needs a mock.
//
// The fixture suites go into a registry of their own. Left alone, the stack
// starts (or reuses) a Zot container named babelsuite-e2e-zot on port 5001, so
// a test run can never put its suites into the demo registry on port 5000 that
// a developer browses. CI sets E2E_REGISTRY_URL to the registry its job started.

import { spawn, spawnSync } from 'node:child_process'
import { get } from 'node:http'
import { randomBytes } from 'node:crypto'
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { setTimeout as sleep } from 'node:timers/promises'
import { fileURLToPath, URL } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const frontendDir = resolve(here, '..', '..')
const repoRoot = resolve(frontendDir, '..')
const stackDir = join(frontendDir, 'e2e', '.stack')
const authDir = join(frontendDir, 'e2e', '.auth')

const port = process.env.E2E_API_PORT ?? '8090'
const ownRegistry = !process.env.E2E_REGISTRY_URL
const registryUrl = process.env.E2E_REGISTRY_URL ?? 'http://localhost:5001'
const registryContainer = 'babelsuite-e2e-zot'
const demoRegistry = 'http://localhost:5000'
// Which suites to push and which registry repositories to show. The load-testing
// runner (e2e/load/run.py) overrides both to use its own suites.
const suitesDir = process.env.E2E_SUITES_DIR ?? join(repoRoot, 'e2e', 'suites')
const registryScope = process.env.E2E_REGISTRY_SCOPE ?? 'e2e'
const mongoUri = process.env.E2E_MONGO_URI ?? 'mongodb://localhost:27017'
const exe = process.platform === 'win32' ? '.exe' : ''

function log(message) {
  process.stdout.write(`[e2e-stack] ${message}\n`)
}

function run(command, args, options = {}) {
  const result = spawnSync(command, args, { stdio: 'inherit', ...options })
  if (result.error || result.status !== 0) {
    throw new Error(`${command} ${args.join(' ')} failed (${result.error?.message ?? `exit ${result.status}`})`)
  }
}

// The demo registry is the one a developer browses. Pushing the fixture suites
// into it is what filled the New Execution list with verify-*, e2e-* and
// e2e-load-* entries nobody asked for, so it is refused unless CI says it is
// a throw-away one.
function refuseTheDemoRegistry() {
  if (registryUrl.replace(/\/$/, '') === demoRegistry && !process.env.CI && !process.env.E2E_ALLOW_DEMO_REGISTRY) {
    throw new Error(
      `E2E_REGISTRY_URL points at the demo registry (${demoRegistry}). The e2e run would push its fixture suites ` +
        'into the list you browse. Leave E2E_REGISTRY_URL unset to use the registry the stack manages for itself, ' +
        'or set E2E_ALLOW_DEMO_REGISTRY=1 if you really mean it.',
    )
  }
}

async function ensureRegistry() {
  if (!ownRegistry) {
    return
  }
  const port = new URL(registryUrl).port || '5001'
  const state = spawnSync('docker', ['inspect', '--format', '{{.State.Running}}', registryContainer], { encoding: 'utf-8' })
  if (state.status === 0) {
    if (state.stdout.trim() !== 'true') {
      log(`starting ${registryContainer}`)
      run('docker', ['start', registryContainer])
    }
  } else {
    log(`creating ${registryContainer} on port ${port}`)
    run('docker', [
      'run', '-d', '--name', registryContainer, '-p', `${port}:5000`,
      '-v', `${join(repoRoot, 'demo', 'zot-config.json')}:/etc/zot/config.json:ro`,
      `ghcr.io/project-zot/zot-linux-amd64:${process.env.DEMO_ZOT_TAG ?? 'latest'}`,
    ])
  }
  const deadline = Date.now() + 60_000
  while (!(await registryAnswers())) {
    if (Date.now() > deadline) {
      throw new Error(`${registryUrl} did not answer; check \`docker logs ${registryContainer}\``)
    }
    await sleep(1000)
  }
}

function registryAnswers() {
  return new Promise((done) => {
    const request = get(`${registryUrl}/v2/`, (response) => {
      response.resume()
      done(response.statusCode === 200)
    })
    request.on('error', () => done(false))
    request.setTimeout(2000, () => {
      request.destroy()
      done(false)
    })
  })
}

function pushSuites() {
  const script = join(repoRoot, 'scripts', 'push-to-zot.py')
  const args = [script, '--registry', registryUrl, '--suites-dir', suitesDir]
  for (const python of ['python', 'python3']) {
    const probe = spawnSync(python, ['--version'], { stdio: 'ignore' })
    if (!probe.error && probe.status === 0) {
      log(`pushing e2e suites to ${registryUrl}`)
      run(python, args)
      return
    }
  }
  throw new Error('python is required to push the e2e suites to the registry')
}

function buildServer() {
  if (process.env.E2E_SERVER_BIN) {
    return process.env.E2E_SERVER_BIN
  }
  mkdirSync(stackDir, { recursive: true })
  const out = join(stackDir, `babelsuite-server${exe}`)
  log('building the control plane')
  run('go', ['build', '-o', out, './cmd/server'], { cwd: join(repoRoot, 'backend') })
  return out
}

function writeSettings(dir) {
  const configuration = join(dir, 'configuration.yaml')
  writeFileSync(
    configuration,
    `mode: local
description: Playwright end-to-end run.
agents:
  - agentId: local-docker
    name: Local Docker
    type: local
    enabled: true
    default: true
    description: The Docker daemon on this machine.
    routingTags: []
    dockerSocket: /var/run/docker.sock
    apisixSidecar:
      image: apache/apisix:3.9.0-debian
      configMountPath: /usr/local/apisix/conf/apisix.yaml
      listenPort: 9080
      adminPort: 9180
      capabilities: [rest, grpc, async]
registries:
  - registryId: e2e-zot
    name: E2E Registry
    provider: zot
    registryUrl: ${registryUrl}
    username: ""
    secret: ""
    repositoryScope: "${registryScope}"
    region: ""
    allowLocalNetwork: true
secrets:
  provider: none
  globalOverrides: []
notifications:
  smtp:
    host: ""
    port: 587
    username: ""
    password: ""
    from: ""
plugins: []
`,
  )
  const profiles = join(dir, 'babelsuite-profiles.yaml')
  writeFileSync(profiles, 'suites: {}\n')
  return { configuration, profiles, agents: join(dir, 'babelsuite-agents.yaml') }
}

refuseTheDemoRegistry()
await ensureRegistry()
const bin = (pushSuites(), buildServer())
if (!existsSync(bin)) {
  throw new Error(`server binary not found at ${bin}`)
}

const workDir = mkdtempSync(join(tmpdir(), 'babelsuite-e2e-'))
const files = writeSettings(workDir)
const jwtSecret = randomBytes(24).toString('hex')
const session = {
  email: process.env.E2E_ADMIN_EMAIL ?? 'admin@e2e.local',
  password: process.env.E2E_ADMIN_PASSWORD ?? randomBytes(12).toString('hex'),
  mongoUri,
  mongoDb: `babelsuite_e2e_${randomBytes(4).toString('hex')}`,
  registryUrl,
  apiUrl: `http://127.0.0.1:${port}`,
  jwtSecret,
  workDir,
  startedAt: new Date().toISOString(),
}
mkdirSync(authDir, { recursive: true })
// Remove the previous session's temp directory, which the old server held open.
try {
  const previous = JSON.parse(readFileSync(join(authDir, 'session.json'), 'utf-8'))
  if (previous.workDir && previous.workDir.includes('babelsuite-e2e-')) {
    rmSync(previous.workDir, { recursive: true, force: true })
  }
} catch {
  // no previous session
}
// Nothing from an earlier session may leak into this one.
for (const stale of ['runs.jsonl', 'visited.jsonl', 'admin.json', 'user.json', 'user-credentials.json', 'restart.flag', 'server.pid', 'admin-token.txt', 'user-token.txt']) {
  rmSync(join(authDir, stale), { force: true })
}
writeFileSync(join(authDir, 'session.json'), JSON.stringify(session, null, 2))

const serverEnv = {
  ...process.env,
  PORT: port,
  JWT_SECRET: jwtSecret,
  ADMIN_EMAIL: session.email,
  ADMIN_PASSWORD: session.password,
  MONGO_URI: mongoUri,
  MONGO_DB: session.mongoDb,
  PLATFORM_SETTINGS_FILE: files.configuration,
  PROFILES_FILE: files.profiles,
  AGENT_RUNTIME_FILE: files.agents,
  FRONTEND_URL: process.env.E2E_BASE_URL ?? 'http://127.0.0.1:4173',
  PUBLIC_API_URL: session.apiUrl,
  AUTH_PASSWORD_LOGIN_ENABLED: 'true',
  AUTH_SIGNUP_ENABLED: 'true',
  REDIS_ADDR: '',
}

const restartFlag = join(authDir, 'restart.flag')
let server
let stopping = false

function startServer() {
  log(`starting the control plane on :${port} (database ${session.mongoDb})`)
  server = spawn(bin, [], { cwd: workDir, stdio: 'inherit', env: serverEnv })
  writeFileSync(join(authDir, 'server.pid'), String(server.pid))
  server.on('exit', (code) => {
    // A test that wants a restart leaves restart.flag behind, kills the server
    // and expects the same one (same database, same settings) to come back.
    if (!stopping && existsSync(restartFlag)) {
      rmSync(restartFlag, { force: true })
      startServer()
      return
    }
    process.exit(code ?? 0)
  })
}

const stop = () => {
  stopping = true
  server?.kill()
}
process.on('SIGINT', stop)
process.on('SIGTERM', stop)
process.on('exit', stop)

startServer()
