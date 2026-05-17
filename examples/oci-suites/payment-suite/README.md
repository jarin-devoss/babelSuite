# Payment Suite

Bank-grade reference environment covering a full payment flow: Postgres, Kafka, a Stripe mock, fraud detection, baseline traffic, and smoke tests with exported JUnit and coverage reports.

## Overview

This suite exercises the core BabelSuite primitives across a realistic payment topology. It shows how to compose services with external mock dependencies, sequence database migrations before application startup, run a baseline traffic plan against a live API, and export test artifacts on every run regardless of outcome.

Use it as a starting point for integrating any service that depends on a payment gateway, a message broker, and a relational database.

## Execution Order

```
db
├── stripe_mock
├── migrations ──┐
│               payment_gateway
kafka            │
└── bootstrap_topics ──┐
                   fraud_worker
                       │
                   checkout_baseline  ← traffic plan against payment_gateway
                       │
                   checkout_smoke     ← exports junit.xml + cobertura.xml
```

## Structure

| Path | Description |
|---|---|
| `suite.star` | Declarative topology — services, mocks, tasks, traffic, and tests |
| `profiles/` | Environment overlays for `local`, `ci`, `staging`, and `production` |
| `api/` | OpenAPI contracts for the payment gateway and fraud service |
| `mock/` | Stripe mock schemas, response examples, and state machine definitions |
| `tasks/` | `bootstrap_topics.sh` (Kafka setup) and `migrate.py` (schema migrations) |
| `tests/` | `checkout_smoke.py` — payment flow smoke tests |
| `traffic/` | `checkout_baseline.star` — baseline load plan targeting the payment gateway |
| `fixtures/` | Seed data: payment methods, merchants, and test card numbers |
| `policies/` | Latency thresholds and fraud-score validation rules |

## Running

```bash
# Pull and run with the local profile
babelctl run localhost:5000/babelsuite/payment-suite:latest --profile local

# Run a specific version
babelctl run localhost:5000/babelsuite/payment-suite:2.1.0 --profile ci
```

## Key Concepts Demonstrated

- **Mock with database dependency** — `stripe_mock` waits for `db` to be healthy before accepting connections
- **Parallel task execution** — `bootstrap_topics` and `migrations` run concurrently since neither depends on the other
- **Traffic before tests** — the smoke test only starts after the baseline traffic plan completes
- **Always-export artifacts** — JUnit and Cobertura reports are exported even when tests fail (`"on": "always"`)
