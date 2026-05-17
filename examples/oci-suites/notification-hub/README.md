# Notification Hub

Redis-backed notification service with Postgres, a mock email provider, database migrations, template seeding, and a background dispatcher worker.

## Overview

This suite models a notification platform that sends transactional emails through a mocked provider. A Postgres database and Redis cache back a notification API; a separate dispatcher worker handles async delivery. Migrations and template seeding run before the API starts, ensuring the database is fully prepared before any requests arrive.

Use it as a reference for any service that combines a relational store, a cache layer, an external provider mock, and an async background worker.

## Execution Order

```
db                         cache
├── email_mock              │
├── seed_templates          │
└── migrations ─────────────┤
                       notification_api
                            └── dispatcher
                                    │
                               notify_smoke  ← exports junit.xml
```

## Structure

| Path | Description |
|---|---|
| `suite.star` | Declarative topology — cache, database, mock, seeders, API, dispatcher, and tests |
| `profiles/` | Environment-specific overrides for SMTP settings, cache TTLs, and delivery concurrency |
| `api/` | OpenAPI contracts for the notification API |
| `mock/` | Email provider mock schemas and delivery receipt fixtures |
| `tasks/` | `seed_templates.sql` (notification templates) and `migrate.sh` (schema migrations) |
| `tests/` | `notify_smoke.py` — send, delivery confirmation, and bounce handling tests |
| `fixtures/` | Pre-built notification payloads and recipient lists |
| `policies/` | Delivery SLA thresholds and schema validation rules |

## Running

```bash
# Run locally with the mock email provider and dev database
babelctl run localhost:5000/babelsuite/notification-hub:latest --profile local

# Run in CI with a smaller fixture set and faster delivery timeouts
babelctl run localhost:5000/babelsuite/notification-hub:latest --profile ci
```

## Key Concepts Demonstrated

- **Parallel pre-flight tasks** — `seed_templates` and `migrations` both wait on the database but run concurrently, shortening the startup critical path
- **Cache and database as independent roots** — `cache` and `db` start in parallel since neither depends on the other, with the API waiting for both
- **Background worker in test scope** — `dispatcher` runs as a live service during the smoke test, validating that async delivery completes within the test window
- **Artifact export** — `notify_smoke` exports a JUnit report on every run for CI test result tracking
