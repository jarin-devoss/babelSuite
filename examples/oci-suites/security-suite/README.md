# Security Suite

A reference for all eight `security.*` modes, run against a real service.

## What runs

- `api` is a small API (`tasks/api.py`) that does what the checks look for: it sends
  security headers on every response, answers 404 for paths that must not be public,
  refuses `PUT`, `DELETE`, `TRACE` and `TRACK`, has no GraphQL endpoint, trusts one CORS
  origin and rate-limits `/api/v1/resource` to 20 requests a second.
- Each `security.*` step scans it with `target="http://api:8000"`. The scanner runs in
  the suite's APISIX sidecar, which joins the run's network to reach the service.
- Every step exports its report (`findings/<step>.json`). `verify-findings` reads them
  from the workspace and fails on a missing report, on checks that could not be
  determined, and on any finding at or above `SEVERITY_GATE` (default `high`).

## What a pass means

The checks pass because `api` is careful. Nothing is hidden behind the gateway. To see
a check fail, remove a header or allow `PUT` in `tasks/api.py` and run the suite again:
the matching step fails, and so does `verify-findings`.

Scanning a suite's own `mock.api` instead (no `target=`) measures the gateway and the
mock, not a service of yours, and `harden=True` makes headers and flood pass because of
the gateway. The step says so in its log.

## Profiles

- `local.yaml`: the default run.
- `hardware.yaml`: runs `verify-findings` on an agent with a USB serial device attached.

## Structure

- `suite.star`: declarative topology
- `tasks/api.py`: the service under test
- `tasks/verify_findings.py`: the gate
- `profiles/`: environment-specific runtime overrides and launch profiles
