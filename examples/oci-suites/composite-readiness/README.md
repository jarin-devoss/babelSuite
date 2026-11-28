# Composite Readiness

Imports three suites and asks each imported service whether it is ready. A thin layer over environments other suites own: it adds no service or mock of its own.

## Execution order

```
payment-suite ─┐
identity-broker ├── readiness-smoke-<suite> (one per suite) ── composite-health-summary ── done
soap-claims-hub ┘
```

## What breaks it

Each `readiness-smoke-*` step asks its imported service for `/health` and expects `{"status": "ok"}`. A suite that does not become ready within `READINESS_TIMEOUT_SECONDS` fails the run, and so does one that answers anything else.

A name in `SUITES_TO_CHECK` that this suite does not import stops it at evaluation, with a message that lists the ones it does.

## Profiles

| Profile | What it does |
|---|---|
| `local.yaml` | The three suites, 120 seconds each. |

## Structure

| Path | Description |
|---|---|
| `suite.star` | The imports and the checks. |
| `dependencies.yaml` | Where the imported suites come from. |
| `tests/` | `readiness_smoke.py`, `composite_summary.py`. |

## Running

```bash
python scripts/push-to-zot.py --suite composite-readiness --sync http://localhost:8090
```

Then launch it from the UI, or `POST /api/v1/executions` with `{"suiteId": "composite-readiness", "profile": "local.yaml"}`.

Only suites whose mocks are HTTP can be imported: the gateway serves an imported suite's HTTP and SOAP mocks, not its gRPC ones.
