# Identity Broker

A broker that signs a user in through an identity provider (OIDC, SAML or LDAP, each a mock), keeps the session in Redis, and answers who a session belongs to. A pre-hook starts the session store and gates the run on the store and the upstream being alive.

## Execution order

```
pre-hook: session-store ─┐
          upstream-idp ──┴── environment_ready
oidc-mock, saml-mock, ldap-mock ── broker-api ── broker-api-ready ── providers_ready
                                                  ├── login-baseline  (traffic)
                                                  ├── login-smoke-<provider>
                                                  ├── expired-session
                                                  ├── mfa-smoke
                                                  └── realm-isolation-<n>
```

## What breaks it

A token is trusted only if its signature verifies: `verified()` in `tasks/broker_api.py`. Make it return the payload without the `hmac.compare_digest` check and `login-smoke-*`, which hands the broker a tampered token, fails.

A session belongs to one realm and cannot read another's claims; remove that check and `realm-isolation-*` fails.

## Profiles

| Profile | What it does |
|---|---|
| `local.yaml` | All three providers, MFA on. |
| `ci.yaml` | Quiet logs for a pipeline. |
| `canary.yaml` | Canary settings. |

## Structure

| Path | Description |
|---|---|
| `suite.star` | The topology. |
| `pre-hook.star` | Starts the session store and the upstream, and gates the run. |
| `tasks/broker_api.py` | The broker (standard library; talks to Redis with the wire protocol). |
| `mock/` | One mock per identity provider. |
| `tests/` | `login_smoke.py`, `expired_session.py`, `mfa_smoke.py`, `realm_isolation.py`. |
| `traffic/login.star` | The load plan. |

## Running

```bash
python scripts/push-to-zot.py --suite identity-broker --sync http://localhost:8090
```

Then launch it from the UI, or `POST /api/v1/executions` with `{"suiteId": "identity-broker", "profile": "local.yaml"}`.

`notification-hub` imports this suite: its `broker-api` vouches for the callers of `notify-api`.
