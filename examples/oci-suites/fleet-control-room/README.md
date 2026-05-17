# Fleet Control Room

End-to-end vehicle orchestration environment with Redis, gRPC contracts, and a telemetry mock for simulating route spikes and GPS fault injection.

## Overview

This suite models a fleet management control plane: a Redis cache backs a dispatcher API, a planner service computes routes, and a control room aggregates both. A telemetry mock feeds simulated GPS frames and spike scenarios throughout the run, and smoke tests validate the control room's response to degraded conditions.

Use it as a reference for systems that combine a shared cache layer, multi-tier service dependencies, and mock-driven fault injection.

## Execution Order

```
redis_cache
├── telemetry_mock
└── seed_routes ──┐
                  dispatcher_api
                  ├── planner
                  └── control_room
                           │
                       fleet_smoke  ← tests against control_room + telemetry_mock
```

## Structure

| Path | Description |
|---|---|
| `suite.star` | Declarative topology — cache, mock, seeder, dispatcher, planner, control room, and tests |
| `profiles/` | Driver-specific runtime settings for `local`, `perf`, and `staging` |
| `api/` | gRPC protobuf definitions and REST gateway overlays for the dispatcher and control room |
| `mock/` | Telemetry playback feeds and GPS fault injection scenarios |
| `tasks/` | `seed_routes.sh` — Redis route seeding and topology bootstrap |
| `tests/` | `fleet_smoke.py` — control room smoke tests and degraded GPS scenarios |
| `fixtures/` | Vehicle manifests and pre-recorded GPS frame sequences |
| `policies/` | Route SLA thresholds and forbidden-zone violation rules |

## Running

```bash
# Run locally with in-memory telemetry mock and dev Redis
babelctl run localhost:5000/babelsuite/fleet-control-room:latest --profile local

# Run under performance conditions with production-scale fixture sets
babelctl run localhost:5000/babelsuite/fleet-control-room:latest --profile perf
```

## Key Concepts Demonstrated

- **Three-tier service chain** — `dispatcher_api` → `planner` → `control_room` shows how BabelSuite sequences a deep dependency graph without manual ordering
- **Mock-driven fault injection** — the telemetry mock replays GPS anomaly feeds on a schedule, enabling deterministic fault scenarios without modifying application code
- **Cache as a shared dependency** — Redis is declared once and referenced by both the dispatcher and the seeder, with BabelSuite ensuring it is healthy before either starts
- **gRPC contract coverage** — protobuf definitions in `api/` are used by both the mock layer and the smoke tests for schema-consistent request/response validation
