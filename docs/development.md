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

The backend has unit tests in the packages below. All tests are table-driven and use `t.Parallel()`.

| Package | What is tested |
|---------|----------------|
| `internal/cachehub` | Key composition, scope stamps, disabled-hub no-ops, unreachable-address errors |
| `internal/cronjobs` | CRUD lifecycle, scheduler start/stop, suite execution dispatch, Slack and email notifications, result formatting |
| `internal/logstream` | Subscribe, snapshot, live fan-out to multiple subscribers, context cancellation, race-free concurrent appends |
| `internal/eventstream` | Subscribe, length tracking, since-filtering, live fan-out, context cancellation |
| `internal/runner` | Step expectation evaluation — exit code, log presence/absence, multi-rule combinations |
| `internal/httpserver` | CSRF middleware — cookie issuance, token validation, Bearer exemption |
| `internal/runner` (native security) | All six native OWASP check variants via httptest.NewServer |

When adding a new package, follow the same pattern: table-driven cases in `*_test.go`, fakes inline in the test file, no network or database access.

## Useful Backend Commands

Sync example content:

```powershell
go run ./cmd/sync-examples
```

Seed the local registry:

```powershell
go run ./cmd/seed-zot
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
