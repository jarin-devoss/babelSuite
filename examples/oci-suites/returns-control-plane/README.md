# Returns Control Plane

Reverse-logistics suite demonstrating BabelSuite's richer mock runtime: split metadata, response templating, state machines, fallback modes, and multi-protocol mock surfaces.

## Overview

This suite models a returns and refund system backed by a relational database and a message broker. Three separate mocks (refunds, pricing, events) exercise different facets of BabelSuite's mock engine. Kafka topic seeding and route bootstrapping run in parallel before the returns API starts, and a background refund worker processes events throughout the test run.

Use it as a reference when your system under test depends on multiple distinct downstream APIs with stateful behavior, or when you need to validate event-schema contracts alongside HTTP responses.

## Execution Order

```
returns_db                    broker
├── refunds_mock              └── events_mock
├── pricing_mock              └── seed_topics ──┐
└── seed_routes ──────────────────────────────┐  │
                              returns_api      │  │
                                   └── refund_worker
                                              │
                                       returns_smoke
```

## Structure

| Path | Description |
|---|---|
| `suite.star` | Declarative topology — database, broker, three mocks, two seeders, API, worker, and tests |
| `profiles/` | Launch profiles for `local`, `canary`, and peak-season refund traffic |
| `api/` | OpenAPI and protobuf contracts for the returns API and refund pricing service |
| `mock/` | Mock payloads, dispatch metadata, state machines, and fallback definitions |
| `tasks/` | `seed_topics.sh` (Kafka) and `seed_routes.ts` (refund routing tables) |
| `tests/` | `returns_smoke.py` — submit, review, and refund approval flows |
| `fixtures/` | Seeded return cases and customer profiles |
| `policies/` | Refund-limit thresholds and event-schema validation rules |

## Running

```bash
# Run locally with in-process mocks and a local broker
babelctl run localhost:5000/babelsuite/returns-control-plane:latest --profile local

# Run under canary traffic conditions
babelctl run localhost:5000/babelsuite/returns-control-plane:latest --profile canary
```

## Key Concepts Demonstrated

- **Multiple concurrent mocks** — `refunds_mock`, `pricing_mock`, and `events_mock` each model a different downstream surface with independent state machines
- **Parallel seeding** — `seed_topics` and `seed_routes` run concurrently across broker and database, reducing startup time
- **Worker alongside tests** — `refund_worker` remains running during the smoke test, processing events produced by the API under test
- **Fallback modes** — mock definitions include fallback responses for unmatched requests, preventing test failures from incomplete fixture coverage
