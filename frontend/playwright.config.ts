import { defineConfig, devices } from '@playwright/test'

const CI = !!process.env.CI
const apiPort = process.env.E2E_API_PORT ?? '8090'
const previewPort = process.env.E2E_PREVIEW_PORT ?? '4173'
const baseURL = process.env.E2E_BASE_URL ?? `http://127.0.0.1:${previewPort}`

export default defineConfig({
  testDir: './e2e',
  testMatch: /.*\.(spec|setup)\.ts/,
  // Specs that launch runs share one Docker daemon and one sidecar per
  // suite+profile, so they run one at a time; see e2e/README.md.
  fullyParallel: false,
  forbidOnly: CI,
  retries: CI ? 1 : 0,
  failOnFlakyTests: !!process.env.E2E_NIGHTLY,
  workers: 1,
  timeout: 60_000,
  expect: { timeout: 15_000 },
  reporter: CI
    ? [['list'], ['github'], ['html', { outputFolder: 'e2e-report', open: 'never' }]]
    : [['list'], ['html', { outputFolder: 'e2e-report', open: 'never' }]],
  globalTeardown: './e2e/global-teardown.ts',
  use: {
    baseURL,
    // On the first failure, not only on the retry: the trace is what shows where
    // a request really went (a cross-site API call showed up only there).
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
  },
  webServer: [
    {
      // The throw-away control plane: unique database, fixture suites, random admin.
      command: 'node e2e/stack/backend.mjs',
      url: `http://127.0.0.1:${apiPort}/readyz`,
      reuseExistingServer: false,
      timeout: 240_000,
      stdout: 'pipe',
      stderr: 'pipe',
    },
    {
      command: `npm run build && npx vite preview --host 127.0.0.1 --port ${previewPort} --strictPort`,
      url: baseURL,
      reuseExistingServer: !CI,
      timeout: 240_000,
      env: {
        BABELSUITE_API_URL: `http://127.0.0.1:${apiPort}`,
        // Call the API through this server's proxy, same-origin. Without it the
        // build falls back to http://localhost:8090 when there is no repo-root
        // .env, which is cross-site, so the SameSite=Strict CSRF cookie is never
        // sent and sign-in fails with "CSRF token missing".
        VITE_API_URL: '/',
      },
    },
  ],
  projects: [
    { name: 'setup', testMatch: /admin\.setup\.ts/ },
    { name: 'setup-user', testMatch: /user\.setup\.ts/, dependencies: ['setup'] },
    {
      name: 'chromium',
      testIgnore: [/.*\.setup\.ts/, /rbac\//, /auth\//, /route-coverage/],
      use: {
        ...devices['Desktop Chrome'],
        storageState: 'e2e/.auth/admin.json',
        permissions: ['clipboard-read', 'clipboard-write'],
      },
      dependencies: ['setup-user'],
    },
    {
      // Anonymous sessions: the auth specs start signed out.
      name: 'anonymous',
      testMatch: /auth\/.*\.spec\.ts/,
      use: { ...devices['Desktop Chrome'], storageState: { cookies: [], origins: [] } },
      dependencies: ['setup-user'],
    },
    {
      // Last: every route declared in App.tsx must have been visited by now.
      name: 'coverage',
      testMatch: /route-coverage\.spec\.ts/,
      use: { ...devices['Desktop Chrome'], storageState: { cookies: [], origins: [] } },
      dependencies: ['chromium', 'anonymous', 'rbac'],
    },
    {
      name: 'rbac',
      testMatch: /rbac\/.*\.spec\.ts/,
      use: { ...devices['Desktop Chrome'], storageState: 'e2e/.auth/user.json' },
      dependencies: ['setup-user'],
    },
  ],
})
