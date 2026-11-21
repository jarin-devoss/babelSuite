import { execFileSync } from 'node:child_process'
import { appendFileSync, mkdirSync } from 'node:fs'
import { test as base, expect } from '@playwright/test'
import { Api } from './api/client'
import { AUTH_DIR, VISITED_FILE } from './support/session'

type Cleanup = () => Promise<void> | void

export interface DockerHelper {
  /** Names of the containers labelled with a run id, running or not. */
  containers(runId: string): string[]
  /** Names of the networks whose name carries a run id. */
  networks(runId: string): string[]
  /** The host URL a suite's sidecar is published on, or '' when it is not running. */
  sidecarUrl(suiteId: string, profileFile?: string): string
}

interface TestFixtures {
  /** A unique, readable name for anything the test creates. */
  uniq: (kind: string) => string
  /** Registers a deletion that runs after the test, newest first, even when it failed. */
  cleanup: { add: (fn: Cleanup) => void }
  docker: DockerHelper
  /** Records each route the page visits, for route-coverage.spec.ts. */
  visited: void
  /** On a failure, prints where the page really sent its first API request. */
  apiOrigin: void
}

interface WorkerFixtures {
  api: Api
}

function docker(args: string[]): string[] {
  try {
    const out = execFileSync('docker', args, { encoding: 'utf-8', stdio: ['ignore', 'pipe', 'ignore'] })
    return out.split('\n').map((line) => line.trim()).filter(Boolean)
  } catch {
    return []
  }
}

export const test = base.extend<TestFixtures, WorkerFixtures>({
  api: [
    async ({}, use) => {
      const api = await Api.admin()
      await use(api)
      await api.dispose()
    },
    { scope: 'worker' },
  ],

  uniq: async ({}, use, testInfo) => {
    let counter = 0
    await use((kind) => `e2e-w${testInfo.workerIndex}-${Date.now().toString(36)}-${counter++}-${kind}`)
  },

  cleanup: async ({}, use) => {
    const stack: Cleanup[] = []
    await use({ add: (fn) => void stack.push(fn) })
    for (const fn of stack.reverse()) {
      await fn()
    }
  },

  docker: async ({}, use) => {
    await use({
      containers: (runId) => docker(['ps', '-a', '--filter', `label=babelsuite.run_id=${runId}`, '--format', '{{.Names}}']),
      networks: (runId) => docker(['network', 'ls', '--filter', `name=${runId}`, '--format', '{{.Name}}']),
      sidecarUrl: (suiteId, profileFile = 'local.yaml') => {
        const slug = profileFile.replace(/[./ ]/g, '-')
        const [binding] = docker(['port', `babel-sidecar-${suiteId}-${slug}`, '9080/tcp'])
        return binding ? `http://${binding.replace('0.0.0.0', '127.0.0.1')}` : ''
      },
    })
  },

  apiOrigin: [
    async ({ page }, use, testInfo) => {
      let first = ''
      page.on('request', (request) => {
        if (!first && request.url().includes('/api/v1/')) {
          first = request.url()
        }
      })
      await use()
      if (testInfo.status !== testInfo.expectedStatus) {
        console.log(`[e2e] "${testInfo.title}": page at ${page.url()}, first API request went to ${first || '(none)'}`)
      }
    },
    { auto: true },
  ],

  visited: [
    async ({ page }, use) => {
      mkdirSync(AUTH_DIR, { recursive: true })
      page.on('framenavigated', (frame) => {
        if (frame === page.mainFrame()) {
          appendFileSync(VISITED_FILE, `${new URL(frame.url()).pathname}\n`)
        }
      })
      await use()
    },
    { auto: true },
  ],
})

/**
 * Marks the running test as a known, documented bug: it is expected to fail and
 * turns red the day the bug is fixed, which is the cue to delete this call.
 * Run with E2E_SHOW_BUGS=1 to see how each one really fails.
 */
export function knownBug(reason: string): void {
  test.fail(!process.env.E2E_SHOW_BUGS, reason)
}

export { expect }
