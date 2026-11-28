# Notification Hub

A service that renders a template for a locale and sends it to a channel provider (one mock per channel), keeps what was delivered and dead-letters what a provider refuses. It only serves a caller the identity broker vouches for, and announces payment events it has checked against the payment service. `identity-broker` and `payment-suite` are imported with `suite.run`: their services and mocks are on this run's gateway.

## Execution order

```
identity-broker (imported) ── payment-suite (imported)
<channel>-provider-mock (one per channel) ── notify-api ── notify-api-ready ── channels_ready
                                                           ├── notify-smoke-<channel>
                                                           ├── template-render-<locale>
                                                           ├── dlq-drain-audit
                                                           ├── rate-limit-boundary
                                                           └── payment-notify-<suite>
```

## What breaks it

A send a provider refuses is dead-lettered, never dropped: `dead_letter()` in `tasks/notify_api.py`. Make it return without appending and `dlq-drain-audit`, which counts what was sent against what was delivered plus dead-lettered, fails.

A payment event the payment service does not bear out is dropped, not announced; remove that check and `payment-notify-*` fails.

## Profiles

| Profile | What it does |
|---|---|
| `local.yaml` | All channels and locales, rate limit 20 requests a second. |
| `ci.yaml` | Quiet logs, the same rate limit. |

## Structure

| Path | Description |
|---|---|
| `suite.star` | The topology, with the two imported suites. |
| `dependencies.yaml` | Where the imported suites come from. |
| `tasks/notify_api.py` | The notification service. |
| `api/channels.yaml` | The providers' contract. |
| `mock/<channel>/` | A mock per channel; `fail@example.test` is refused. |
| `tests/` | Smoke, template, dead-letter, rate-limit and payment-event tests. |

## Running

```bash
python scripts/push-to-zot.py --suite notification-hub --sync http://localhost:8090
```

Then launch it from the UI, or `POST /api/v1/executions` with `{"suiteId": "notification-hub", "profile": "local.yaml"}`.
