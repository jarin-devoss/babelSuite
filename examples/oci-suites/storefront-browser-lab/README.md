# Storefront Browser Lab

Browser-first commerce environment with Kafka event streams, mocked catalog and order APIs, and Playwright end-to-end checkout journeys.

## Overview

This suite demonstrates how to combine backend event infrastructure with a full UI stack in a single BabelSuite topology. A Kafka broker drives an event consumer, two mock APIs back the storefront, and Playwright runs real browser tests against the live UI — all resolved from a single `suite.star` file.

Use it as a reference for any setup that requires a real browser, event-driven consumers, and API mocks running together in the same environment.

## Execution Order

```
broker
├── catalog_mock
├── orders_mock ──┐
│                 event_consumer
│                 storefront_api ──┐
│                                  storefront_ui
└── seed_topics ──┘                │
                                playwright_checkout  ← Playwright tests
```

## Structure

| Path | Description |
|---|---|
| `suite.star` | Declarative topology — broker, mocks, consumer, UI, and Playwright tests |
| `profiles/` | Browser, Kafka, and mock dispatch settings for `local`, `ci`, and campaign traffic |
| `api/` | Order and catalog OpenAPI contracts exposed to the UI and background consumer |
| `mock/` | Mock API payloads for product catalog and order submission paths |
| `tasks/` | `seed_topics.sh` — Kafka topic bootstrap and browser fixture warm-up |
| `tests/` | `playwright_checkout.spec.ts` — checkout success and cart abandonment journeys |
| `fixtures/` | Seeded products, campaigns, and browser-side user sessions |
| `policies/` | Event schema and checkout latency validation rules |

## Running

```bash
# Pull and run with the local profile
babelctl run localhost:5000/babelsuite/storefront-browser-lab:latest --profile local

# Run against CI settings (headless browser, smaller fixture set)
babelctl run localhost:5000/babelsuite/storefront-browser-lab:latest --profile ci
```

## Key Concepts Demonstrated

- **Browser tests in a suite** — Playwright runs inside a container (`mcr.microsoft.com/playwright`) with direct network access to the UI service
- **Mock API composition** — `catalog_mock` and `orders_mock` both depend on the broker, wiring event production into the mock layer
- **UI after API** — `storefront_ui` only starts after both mocks and the API are healthy, ensuring the UI has live backends on first load
- **Event consumer alongside tests** — `event_consumer` runs as a long-lived service during the Playwright session, validating downstream event handling in real time
