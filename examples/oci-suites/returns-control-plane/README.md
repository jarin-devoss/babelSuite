# Returns Control Plane

A refund service that takes a return, asks a pricing service for a quote over gRPC, registers the return with a partner, and holds back any refund above a limit for manual review. The pricing service is a gRPC mock the gateway serves from `api/proto/refund_pricing.proto`; the partner is a REST mock.

## Execution order

```
partner-api (mock) ── refund-worker ── refund-worker-ready ── ready
                                                              └── returns-stress ── returns-spike
                                                                     └── returns-smoke ── refund-smoke ── rollback-returns (on failure)
```

## What breaks it

A refund above `REFUND_LIMIT_CENTS` is never issued without manual review, whatever the pricing service says: `decide()` in `tasks/refund_worker.py`. Make the `if amount > LIMIT:` line `if False:` and `returns-smoke`, which asks for a refund above the limit on a reason the pricing mock calls `instant`, fails.

If the pricing service gives no quote, no return is created (502); the same test covers it.

## Profiles

| Profile | What it does |
|---|---|
| `local.yaml` | The default limit of 10000 cents. |
| `canary.yaml` | Canary settings. |
| `peak.yaml` | Peak-season settings. |

## Structure

| Path | Description |
|---|---|
| `suite.star` | The topology. |
| `tasks/refund_worker.py` | The refund service; its gRPC-Web client is written by hand. |
| `api/proto/refund_pricing.proto` | The pricing contract the gateway serves. |
| `mock/pricing/` | The pricing mock, dispatched by the return reason. |
| `mock/returns/` | The partner's REST mocks. |
| `tests/` | `returns_smoke.py`, `refund_smoke.py`. |
| `traffic/returns.star` | The load plan. |

## Running

```bash
python scripts/push-to-zot.py --suite returns-control-plane --sync http://localhost:8090
```

Then launch it from the UI, or `POST /api/v1/executions` with `{"suiteId": "returns-control-plane", "profile": "local.yaml"}`.

A suite that calls a gRPC mock is run on its own: an importing suite's gateway does not serve an imported suite's gRPC mocks.
