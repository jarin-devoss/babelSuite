import { existsSync, readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { test, expect } from '@playwright/test'
import { VISITED_FILE } from './support/session'

const here = dirname(fileURLToPath(import.meta.url))
const APP = resolve(here, '..', 'src', 'App.tsx')

/** The path of every <Route path='...'> in App.tsx. */
function declaredRoutes(): string[] {
  const source = readFileSync(APP, 'utf-8')
  return [...source.matchAll(/<Route path='([^']+)'/g)].map((m) => m[1])
}

function matcher(route: string): RegExp {
  const pattern = route.replace(/[.*+?^${}()|[\]\\]/g, '\\$&').replace(/:[A-Za-z]+/g, '[^/]+')
  return new RegExp(`^${pattern}$`)
}

// Runs after every other project (see the config). A route nobody visited is a
// route no test would notice breaking.
test('every route declared in App.tsx was visited by at least one test', async () => {
  expect(existsSync(VISITED_FILE), 'no spec recorded a visit; did the other projects run?').toBe(true)
  const visited = new Set(readFileSync(VISITED_FILE, 'utf-8').split('\n').map((l) => l.trim()).filter(Boolean))

  const routes = declaredRoutes()
  expect(routes.length, 'App.tsx should declare routes').toBeGreaterThan(15)

  // '*' is the catch-all: it is visited by any path that no other route claims.
  const specific = routes.filter((route) => route !== '*')
  const claimed = (path: string) => specific.some((route) => matcher(route).test(path))
  const missed = routes.filter((route) =>
    route === '*' ? ![...visited].some((path) => !claimed(path)) : ![...visited].some((path) => matcher(route).test(path)),
  )
  expect(missed, `routes no test visited: ${missed.join(', ')}`).toEqual([])
})
