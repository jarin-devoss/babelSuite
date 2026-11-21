import { execFileSync } from 'node:child_process'
import { existsSync, readFileSync, rmSync } from 'node:fs'
import { MongoClient } from 'mongodb'
import { Api } from './api/client'
import { RUNS_FILE, SESSION_FILE, VISITED_FILE, readSession } from './support/session'

function docker(args: string[]): string[] {
  try {
    const out = execFileSync('docker', args, { encoding: 'utf-8', stdio: ['ignore', 'pipe', 'ignore'] })
    return out.split('\n').map((line) => line.trim()).filter(Boolean)
  } catch {
    return []
  }
}

/**
 * Removes what the run left behind: the environments of every run the session
 * launched, the sidecars of the fixture suites, the leaked networks, the
 * throw-away database and the temp directory. It runs even after failures.
 */
export default async function globalTeardown(): Promise<void> {
  if (!existsSync(SESSION_FILE)) {
    return
  }
  const session = readSession()
  const runIds = existsSync(RUNS_FILE)
    ? [...new Set(readFileSync(RUNS_FILE, 'utf-8').split('\n').map((l) => l.trim()).filter(Boolean))]
    : []

  try {
    const api = await Api.admin()
    for (const environment of await api.environments()) {
      if (runIds.includes(environment.runId)) {
        await api.reap(environment.sandboxId)
      }
    }
    await api.dispose()
  } catch (error) {
    console.warn(`[e2e-teardown] could not reap through the API: ${String(error)}`)
  }

  // The API may already be gone; remove what Docker still holds for the session.
  for (const runId of runIds) {
    const containers = docker(['ps', '-aq', '--filter', `label=babelsuite.run_id=${runId}`])
    if (containers.length > 0) {
      docker(['rm', '-f', ...containers])
    }
    for (const network of docker(['network', 'ls', '--filter', `name=${runId}`, '--format', '{{.Name}}'])) {
      docker(['network', 'rm', network])
    }
  }
  for (const sidecar of docker(['ps', '-aq', '--filter', 'name=babel-sidecar-ui-'])) {
    docker(['rm', '-f', sidecar])
  }

  try {
    const mongo = await MongoClient.connect(session.mongoUri, { serverSelectionTimeoutMS: 5_000 })
    await mongo.db(session.mongoDb).dropDatabase()
    await mongo.close()
  } catch (error) {
    console.warn(`[e2e-teardown] could not drop ${session.mongoDb}: ${String(error)}`)
  }

  try {
    rmSync(session.workDir, { recursive: true, force: true })
  } catch {
    // The control plane still has this directory as its working directory until
    // Playwright stops it; the next start removes it (see stack/backend.mjs).
  }
  rmSync(RUNS_FILE, { force: true })
  // visited.jsonl is read by route-coverage.spec.ts, which runs before teardown;
  // clear it so the next session starts with an empty record.
  rmSync(VISITED_FILE, { force: true })
}
