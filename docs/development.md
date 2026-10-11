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
