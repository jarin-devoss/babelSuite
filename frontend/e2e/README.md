# End-to-end tests

Playwright drives the real UI against a real control plane, Docker and registry.
Nothing here mocks the backend, apart from a few error states that cannot be
produced any other way.

## Running them

You need Docker, Go, Node and Python (with `pyyaml`), and MongoDB from the demo
stack. The registry is not needed from it: the stack starts one of its own.

```bash
docker compose -f demo/docker-compose.yml up -d mongo   # from the repo root
cd frontend
npm ci
npx playwright install chromium
npm run e2e            # everything
npm run e2e:smoke      # the @smoke subset, which is what a pull request runs
npm run e2e:ui         # Playwright's UI mode
```

One command starts everything it needs (`playwright.config.ts` → `webServer`):

1. `e2e/stack/backend.mjs` starts (or reuses) a Zot container of its own,
   `babelsuite-e2e-zot` on port **5001**, pushes the fixture suites from
   `e2e/suites/` to it, then builds and starts a **throw-away control plane**
   with its own Mongo database, a random admin password and a registry scope of
   `e2e`. The demo registry on port 5000 is never touched, so the suites you
   browse do not fill up with fixtures. `E2E_REGISTRY_URL` overrides the
   registry (CI sets it), and the demo registry is refused outside CI.
   `docker rm -f babelsuite-e2e-zot` removes it.
2. `vite preview` serves the built frontend, proxying `/api` to it.

The control plane listens on **:8090**. The APISIX sidecar reaches it at
`host.docker.internal:8090`, so a mock-backed step only works there. Stop any
other control plane first.

When the run ends, `global-teardown.ts` reaps the environments the session
started, removes the fixture sidecars and networks, and drops the database.

## Layout

```
e2e/
  stack/backend.mjs      starts the throw-away control plane
  admin.setup.ts         signs in as the admin once, saves state and token
  user.setup.ts          creates a non-admin user the same way
  fixtures.ts            api, uniq, cleanup, docker, knownBug, route recording
  api/client.ts          REST client for setting up and checking state
  pages/                 one page object per area
  support/               session, users, field locator, stack restarts
  tests/<area>/          the specs
  route-coverage.spec.ts fails if a route in App.tsx was never visited
../../e2e/suites/        the fixture suites (ui-pass, ui-fail, ui-mock, ...)
../../e2e/plugins/       the fixture plugin (e2e-echo)
```

## Rules

Enforced by `eslint-plugin-playwright` (`npm run lint`) and review.

1. **Every test checks an outcome.** After a UI action, read the backend back
   (`api`) or check the run's result. "The page rendered" is not an assertion.
2. **No fixed sleeps**, no `try/catch`, no `if` inside a test, no OR-assertions.
   Wait for a state. The two places that must let time pass say why.
3. **Role and label selectors first**; `data-testid` for repeated structures.
4. **Isolation.** Each test creates its own data with `uniq()` and removes it
   through `cleanup`. No test reads demo data or depends on another's data.
5. **One sidecar per suite and profile.** Mock state is per suite, so two specs
   must not launch the same suite and profile at the same time. The config runs
   one worker.
6. **Sign-in is rate limited** (ten a minute per address). The setups sign in
   once and save the token; use `Api.admin()` / `Api.user()` for the rest.
7. **A known bug is `knownBug('reason')`**: the test is expected to fail and
   turns red the day the bug is fixed, which is the cue to delete the call.
   Run with `E2E_SHOW_BUGS=1` to see how each one really fails.

## Known bugs these tests pin

| Id | Where | What |
|----|-------|------|
| E-N1 | auth | Password reset can never send mail (the SMTP config never reaches the auth handler) and the page tells the user a link is coming |
| E-N6 | executions | `BABELSUITE_GATEWAY_URL` is the host-published `127.0.0.1` address, unreachable from inside a step container |
| E-N8 | suites | `GET /api/v1/suites/{id}` returns operations with no exchanges, so the suite page never shows a mock |
| E-N10 | cron | `nextRunAt` is declared but never set |
| E-N11 | cron | Runs started by a cron job have no workspace, so they never appear in the run list |
| E-N13 | settings | The row menu of the last table row is clipped (`overflow: hidden`) |
| E-N15 | rbac | Any signed-in user can list the plugins, including their Lua source |
| FIX-PLAN #7 | executions | A JUnit export with no file is shown as one passing test |
| FIX-PLAN #27/#28 | resilience | Runs are lost on restart and not shared between instances |
