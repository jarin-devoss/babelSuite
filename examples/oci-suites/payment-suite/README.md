# Payment Suite

A payment service the suite ships and tests. It takes a payment, asks a card processor (the Stripe mock) to authorize it, and keeps the payment, its refunds and a ledger. The tests call the service and replay its contract; the reconcile steps compare the ledger with the payments.

## Execution order

```
stripe-mock ── payment-api ── payment-api-ready ── payment-api is answering
                                                   ├── checkout-baseline / checkout-stress  (traffic)
                                                   ├── checkout-smoke
                                                   ├── refund-regression
                                                   └── reconcile-<currency>   (one per currency)
```

## What breaks it

A payment is posted to the ledger only when the processor authorized it: the `if code == AUTHORIZED:` line in `tasks/payment_api.py`. Take that line out (`if True:`) and the `reconcile-*` steps fail, because the ledger no longer matches the payments, and `checkout-smoke` fails, because a payment parked for review must leave the ledger untouched.

A refund larger than what is left of a payment is refused (422). Remove that check and `refund-regression` fails.

## Profiles

| Profile | What it does |
|---|---|
| `local.yaml` | `FRAUD_STRATEGY: permissive`, verbose logs. |
| `staging.yaml` | `FRAUD_STRATEGY: strict`: a payment the processor parks for review is declined. |
| `year.yaml` | Year-end settings. |
| `base.yaml` | What the others extend (`extendsId`). |

## Structure

| Path | Description |
|---|---|
| `suite.star` | The topology: mock, service, wait step, traffic, tests. |
| `tasks/payment_api.py` | The payment service (standard library only). |
| `mock/wiremock/stripe/` | The card processor's answers, by the request's `status`. |
| `tests/` | `checkout_smoke.py`, `refund_regression.py`, `reconcile_smoke.py`. |
| `traffic/checkout.star` | The load plan, aimed at `payment-api`. |

## Running

```bash
python scripts/push-to-zot.py --suite payment-suite --sync http://localhost:8090
```

Then launch it from the UI, or `POST /api/v1/executions` with `{"suiteId": "payment-suite", "profile": "local.yaml"}`.
