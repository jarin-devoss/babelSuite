---
title: Development
---

# Development

[Back to index](index.md)

## Main Processes

During local development you usually run:

- backend server
- frontend dev server
- optional remote worker
- optional docs server

## Backend

From `backend/`:

```powershell
go run ./cmd/server
```

Optional worker:

```powershell
go run ./cmd/agent
```

## Frontend

From `frontend/`:

```powershell
npm install
npm run dev
```

## Docs

From the repo root:

```powershell
pip install -r docs/requirements.txt
mkdocs serve
```

## Testing

Run all backend tests:

```powershell
go test ./...
```

Run with the race detector (recommended before submitting changes):

```powershell
go test -race ./...
```

Run a single package:

```powershell
go test ./internal/cronjobs/...
```

The traffic cannon has tests that drive a real APISIX sidecar. They need Docker and are kept out of the default run by a build tag:

```powershell
go test -tags integration ./internal/runner -run Integration -count=1
```

The example plugins have tests too. They load every plugin under `examples/oci-plugins/` into a real APISIX sidecar and run each operation against stub servers and, for the Kafka and schema-registry plugins, against real Kafka, Confluent REST Proxy and Redpanda containers (the first run pulls their images and takes a few minutes):

```powershell
go test -tags integration ./internal/runner -run IntegrationPlugin -count=1
```

The security checks are tested the same way: a careful service must pass all eight, a weak one must fail each, and an unreachable target must come back unknown, never passed:

```powershell
go test -tags integration ./internal/runner -run IntegrationSecurity -count=1
```

A plain `go test ./internal/suites` also checks, without Docker, that each plugin's wrapper, manifest and CUE schema agree.

The load-testing proof suites in `e2e/load/` launch real runs through the API and check what a traffic step reports against what its target counted:

```powershell
python e2e/load/run.py
```

### Test coverage

Tests live beside the code they cover, in most `internal/` packages. They are
table-driven and use `t.Parallel()`. Some highlights rather than a full list:

| Package | What is tested |
|---------|----------------|
| `internal/cachehub` | Key composition, scope stamps, disabled-hub no-ops, unreachable-address errors |
| `internal/cronjobs` | CRUD lifecycle, scheduler start/stop, suite execution dispatch, Slack and email notifications, result formatting |
| `internal/logstream` | Subscribe, snapshot, live fan-out to multiple subscribers, context cancellation, race-free concurrent appends |
| `internal/eventstream` | Subscribe, length tracking, since-filtering, live fan-out, context cancellation |
| `internal/runner` | Step expectation evaluation, the `file=` resolution guard, plugin response decoding, span attribute truncation, and the eight native security checks via `httptest.NewServer` |
| `internal/execution` | Profile runtime overlays, per-step memory parsing, contested service names across composed suites, dependency package file resolution |
| `internal/httpserver` | CSRF middleware — cookie issuance, token validation, Bearer exemption |
| `internal/platform` | Plugin registration, and that a plugin's sidecar route is derived from its name |
| `internal/suites` | Starlark evaluation, topology resolution, nested suite expansion |

Note that `internal/execution` tests drive real Docker containers, so they are
slower than the rest and need a working daemon.

When adding a new package, follow the same pattern: table-driven cases in `*_test.go`, fakes inline in the test file, no network or database access.

## End-to-end tests

Playwright drives the real UI against a throw-away control plane, Docker and a
registry of its own. It needs MongoDB from the demo stack, then one command
starts the rest:

```powershell
docker compose -f demo/docker-compose.yml up -d mongo
cd frontend
npm run e2e          # everything
npm run e2e:smoke    # the subset a pull request runs
```

The fixture suites live in `e2e/suites/`, outside `examples/`. They are pushed
to a Zot container the stack starts for itself, `babelsuite-e2e-zot` on port
5001, and reuses on later runs, so a test run never adds suites to the demo
registry on port 5000 that you browse. Remove it with
`docker rm -f babelsuite-e2e-zot`. Set `E2E_REGISTRY_URL` to use another
registry (CI points it at the one its job starts); pointing it at the demo
registry is refused outside CI. The control plane
the tests start listens on port 8090, which the APISIX sidecar needs to reach
mocks, so stop any other one first. See [`frontend/e2e/README.md`](https://github.com/jarin-devoss/babelSuite/blob/main/frontend/e2e/README.md)
for the layout, the rules the tests follow, and the known bugs they pin.

## Useful Backend Commands

Publish example suites, modules and plugins to the local registry. The control
plane serves suites from the OCI catalog, so an edit under `examples/` does not
reach a run until it is pushed and the backend restarted:

```bash
python scripts/push-to-zot.py
python scripts/push-to-zot.py --suite payment-suite
```

## CLI

Build or run the CLI from:

- `backend/cmd/ctl`

Example:

```powershell
go run ./cmd/ctl -- version
```

## Frontend Quality Checks

From `frontend/`:

```powershell
npm run typecheck
npm run build
```

## Docs Deployment

The repository now includes:

- `.github/workflows/docs.yml`

That workflow installs the docs dependencies and deploys the generated site.
