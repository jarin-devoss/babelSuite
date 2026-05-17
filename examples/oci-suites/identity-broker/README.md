# Identity Broker

OIDC and SAML integration sandbox with deterministic login failures, certificate rotation paths, and secret injection for session storage.

## Overview

This suite models a full identity broker topology: a backing database, two protocol mocks (OIDC and SAML), realm seeding, a broker API, a session worker, and login smoke tests. It is designed to exercise BabelSuite's mock state machine and secret injection features in a realistic authentication context.

Use it as a starting point for any system that federates identity across OIDC and SAML providers, or to test how your application behaves under expired tokens, misconfigured issuers, and cert rotation scenarios.

## Execution Order

```
broker_db
├── oidc_mock
├── saml_mock
└── seed_realms ──┐
                  broker_api
                  └── session_worker
                                 │
                             login_smoke
```

## Structure

| Path | Description |
|---|---|
| `suite.star` | Declarative topology — database, protocol mocks, realm seeder, API, worker, and tests |
| `profiles/` | Realm, issuer, and session-storage overrides by environment |
| `api/` | OIDC bridge OpenAPI spec and SAML assertion mapping definitions |
| `mock/` | OIDC JWKS payloads, SAML assertion fixtures, and fault injection scenarios |
| `tasks/` | `seed_realms.ts` — realm definitions and certificate bootstrap |
| `tests/` | `login_smoke.py` — successful login flows and expired-session validation |
| `fixtures/` | Realm definitions, claim payloads, and test user accounts |
| `policies/` | Cookie scope and token issuance validation rules |

## Running

```bash
# Run with the local profile (uses dev issuer URLs and in-memory session store)
babelctl run localhost:5000/babelsuite/identity-broker:latest --profile local

# Run with staging settings (external issuer, Redis session store)
babelctl run localhost:5000/babelsuite/identity-broker:latest --profile staging
```

## Key Concepts Demonstrated

- **Multiple protocol mocks** — OIDC and SAML mocks run side-by-side, both depending on the same backing database
- **Seeding before the API** — `seed_realms` runs after the database is healthy and before `broker_api` starts, ensuring realm config is in place at boot
- **Secret injection** — session storage credentials are injected at launch time through the profile system rather than hardcoded in the topology
- **Deterministic failure scenarios** — the OIDC mock includes state machine definitions for expired JWKS and misconfigured issuer responses
