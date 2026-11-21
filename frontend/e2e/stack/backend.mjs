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

import { spawn, spawnSync } from 'node:child_process'
import { randomBytes } from 'node:crypto'
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const frontendDir = resolve(here, '..', '..')
const repoRoot = resolve(frontendDir, '..')
const stackDir = join(frontendDir, 'e2e', '.stack')
const authDir = join(frontendDir, 'e2e', '.auth')

const port = process.env.E2E_API_PORT ?? '8090'
const registryUrl = process.env.E2E_REGISTRY_URL ?? 'http://localhost:5000'
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

function pushSuites() {
  const script = join(repoRoot, 'scripts', 'push-to-zot.py')
  const args = [script, '--registry', registryUrl, '--suites-dir', join(repoRoot, 'e2e', 'suites')]
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
    repositoryScope: "e2e"
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
