# Fleet Control Room

A route dispatcher. For each assignment it asks a geo-fence service whether the zone may be entered, then asks a route planner (a gRPC mock the gateway serves from `api/proto/fleet_control.proto`) for an assignment, and keeps what came of it. Vehicle telemetry goes to a telemetry mock.

## Execution order

```
telemetry-mock ── geo-fence-mock ── dispatcher ── dispatcher-ready ── fleet_ready
                                                                      └── fleet-ramp ── fleet-steady ── fleet-spike ── fleet-drain
                                                                             └── fleet-smoke
                                                                                  ├── acceptance-<region>
                                                                                  ├── geo-fence-audit
                                                                                  └── route-degradation
```

## What breaks it

A vehicle is never routed into a zone the geo-fence service says is restricted, and the planner is not asked: the `if not allowed:` line in `assign()` (`tasks/dispatcher.py`). Make it `if False:` and `geo-fence-audit`, which asks for a route through the airport apron, fails.

## Profiles

| Profile | What it does |
|---|---|
| `local.yaml` | One region (`eu-west`), a gentle spike. |
| `staging.yaml` | All three regions, a bigger spike. |
| `perf.yaml` | All three regions, the heaviest spike. |

## Structure

| Path | Description |
|---|---|
| `suite.star` | The topology. |
| `tasks/dispatcher.py` | The dispatcher. |
| `api/openapi/fleet-services.yaml` | The telemetry and geo-fence contract. |
| `api/proto/fleet_control.proto` | The route planner. |
| `mock/` | `telemetry/`, `geofence/`, `planner/`: each answers by the request and has a fallback. |
| `tests/` | Smoke, per-region acceptance, geo-fence audit, degradation. |
| `traffic/dispatch.star` | The load plan. |

## Running

```bash
python scripts/push-to-zot.py --suite fleet-control-room --sync http://localhost:8090
```

Then launch it from the UI, or `POST /api/v1/executions` with `{"suiteId": "fleet-control-room", "profile": "local.yaml"}`.

Run this suite on its own: it calls a gRPC mock, which an importing suite's gateway does not serve.
