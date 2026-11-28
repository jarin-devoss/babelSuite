# SOAP Claims Hub

A REST bridge over a legacy SOAP claims system. Clients speak JSON; the bridge builds a SOAP 1.1 or 1.2 envelope, calls the claims system (one mock per claim type, chosen by `SOAPAction`) and turns the answer back into JSON.

## Execution order

```
<claim-type>-claims-mock (one per type) ── claims-bridge ── claims-bridge-ready ── mocks_ready
                                                             ├── claims-baseline-soap-1_1, claims-baseline-soap-1_2
                                                             └── claims-smoke-<claim type>
                                                                  ├── fault-injection-<scenario>
                                                                  ├── legacy-compat-check
                                                                  └── adapter-contract
```

## What breaks it

A SOAP Fault from the claims system is never reported as a success: the `if "Fault" in parsed:` line in `from_soap()` (`tasks/claims_bridge.py`). Make it `if False:` and `fault-injection-missing_header`, `legacy-compat-check` and `adapter-contract`, which send an action the claims system does not serve, fail.

A policy number must carry its claim type's code (`MED-1024` for a medical claim): `validate()` checks it before an envelope is built, and `claims-smoke-*` submits a dental policy as a medical claim.

## Profiles

| Profile | What it does |
|---|---|
| `local.yaml` | Four claim types, SOAP 1.1 and 1.2, legacy envelopes replayed. |
| `canary.yaml` | Canary settings. |

## Structure

| Path | Description |
|---|---|
| `suite.star` | The topology. |
| `tasks/claims_bridge.py` | The bridge. |
| `api/wsdl/claims.wsdl` | The claims system's operations. |
| `mock/claims/` | The SOAP mock: answers by `SOAPAction`, and a Fault for any other. |
| `tests/` | Smoke, fault, legacy and contract tests. |
| `traffic/` | One plan per SOAP version. |

## Running

```bash
python scripts/push-to-zot.py --suite soap-claims-hub --sync http://localhost:8090
```

Then launch it from the UI, or `POST /api/v1/executions` with `{"suiteId": "soap-claims-hub", "profile": "local.yaml"}`.
