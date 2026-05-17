# SOAP Claims Hub

Legacy claims sandbox with a schema-driven SOAP mock, XML envelope rendering, and APISIX-fronted dispatch into the BabelSuite mock engine.

## Overview

This suite demonstrates how BabelSuite handles legacy protocol integration. A single SOAP mock provides the intake surface; reference data is seeded before a claims bridge service starts; and smoke tests validate the full SOAP request-response cycle including fault envelopes.

Use it as a reference when you need to test a service that consumes or produces SOAP/XML, or when you are migrating a legacy SOAP endpoint to REST and need both surfaces running in parallel.

## Execution Order

```
claims_mock
└── seed_reference_data ──┐
                          claims_bridge
                               │
                          claims_smoke
```

## Structure

| Path | Description |
|---|---|
| `suite.star` | Declarative topology — SOAP mock, seeder, bridge service, and tests |
| `profiles/` | SOAP endpoint runtime settings and partner header defaults by environment |
| `api/` | WSDL contract published to claim submitters |
| `mock/` | Schema-driven SOAP mock definitions that render XML envelopes at runtime |
| `tasks/` | `seed_reference_data.sh` — claim code tables and partner fixture bootstrap |
| `tests/` | `claims_smoke.py` — submit and lookup SOAP exchange validation, including fault paths |
| `fixtures/` | Seeded partner claims and policy data |
| `policies/` | SOAP fault envelope validation and schema compliance rules |

## Running

```bash
# Run locally with the SOAP mock and dev partner config
babelctl run localhost:5000/babelsuite/soap-claims-hub:latest --profile local

# Run with staging partner headers and production WSDL endpoint
babelctl run localhost:5000/babelsuite/soap-claims-hub:latest --profile staging
```

## Key Concepts Demonstrated

- **Mock-first topology** — `claims_mock` is the root node; everything else depends on it, reflecting how the SOAP surface is the system's primary intake point
- **Schema-driven XML rendering** — mock definitions describe the SOAP envelope structure; BabelSuite renders the XML at request time rather than storing static response files
- **Fault path coverage** — smoke tests explicitly exercise fault envelope responses (malformed requests, unknown claim codes) alongside happy-path flows
- **Minimal topology** — four steps total make this the simplest suite in the collection, useful for understanding BabelSuite fundamentals before exploring more complex examples
