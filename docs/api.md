---
title: API
---

# API

[Back to index](index.md)

## Route Groups

The control plane API is grouped by responsibility.

## System

- `GET /healthz`
- `GET /readyz`
- `GET /readyz/{subsystem}`
- `GET /api/v1/system/healthz`
- `GET /api/v1/system/readyz`
- `GET /api/v1/system/readyz/{subsystem}`

## Authentication

- `GET /api/v1/auth/config`
- `POST /api/v1/auth/sign-up`
- `POST /api/v1/auth/sign-in`
- `POST /api/v1/auth/sign-out`
- `POST /api/v1/auth/refresh`
- `POST /api/v1/auth/forgot-password`
- `POST /api/v1/auth/reset-password`
- `GET /api/v1/auth/me`
- `GET /api/v1/auth/sso/providers`
- `GET /api/v1/auth/oidc/login`
- `GET /api/v1/auth/oidc/callback`
- `GET /api/v1/auth/oidc/exchange`

The `oidc/*` routes are registered but SSO cannot currently be enabled — see
[Authentication](auth.md#oidc-single-sign-on).

## Catalog

- `GET /api/v1/catalog/packages`
- `GET /api/v1/catalog/packages/{packageId}`
- `GET /api/v1/catalog/favorites`
- `POST /api/v1/catalog/favorites/{packageId}`
- `DELETE /api/v1/catalog/favorites/{packageId}`

## Suites

- `GET /api/v1/suites`
- `GET /api/v1/suites/{suiteId}`
- `POST /api/v1/suites`

!!! note
    `POST /api/v1/suites` validates `suiteStar` with no module resolver, so a
    suite that `load()`s an OCI module or a `@plugins/*` entry is rejected at
    registration. Suites using those must come from a registry instead.

## Profiles

- `GET /api/v1/profiles/suites`
- `GET /api/v1/profiles/suites/{suiteId}`
- `POST /api/v1/profiles/suites/{suiteId}`
- `PUT /api/v1/profiles/suites/{suiteId}/{profileId}`
- `DELETE /api/v1/profiles/suites/{suiteId}/{profileId}`
- `POST /api/v1/profiles/suites/{suiteId}/{profileId}/default`

## Executions

- `GET /api/v1/executions/launch-suites`
- `GET /api/v1/executions/resolve-ref`
- `GET /api/v1/executions/overview`
- `GET /api/v1/executions`
- `POST /api/v1/executions`
- `GET /api/v1/executions/{executionId}`
- `GET /api/v1/executions/{executionId}/events`
- `GET /api/v1/executions/{executionId}/logs`
- `GET /api/v1/executions/{executionId}/logs/snapshot`

## Engine

- `GET /api/v1/engine/overview`
- `GET /api/v1/engine/overview/stream`

## Platform Settings

- `GET /api/v1/platform-settings`
- `PUT /api/v1/platform-settings`
- `POST /api/v1/platform-settings/registries/{registryId}/sync`

## Plugins

- `GET /api/v1/platform-settings/plugins`
- `POST /api/v1/platform-settings/plugins`
- `PUT /api/v1/platform-settings/plugins/{name}`
- `DELETE /api/v1/platform-settings/plugins/{name}`
- `GET /api/v1/platform-settings/plugins/{name}/check`
- `POST /api/v1/platform-settings/plugins/{name}/validate`

## Environments

The frontend route is `/environments`, but the backing API currently uses `/api/v1/sandboxes`:

- `GET /api/v1/sandboxes`
- `GET /api/v1/sandboxes/events`
- `POST /api/v1/sandboxes/reap-all`
- `POST /api/v1/sandboxes/{sandboxId}/reap`

## Agent Registry And Control Plane

- `GET /api/v1/agents`
- `POST /api/v1/agents/register`
- `POST /api/v1/agents/{agentId}/heartbeat`
- `DELETE /api/v1/agents/{agentId}`
- `POST /api/v1/agent-control/claims/next`
- `POST /api/v1/agent-control/jobs/{jobId}/lease`
- `POST /api/v1/agent-control/jobs/{jobId}/state`
- `POST /api/v1/agent-control/jobs/{jobId}/logs`
- `POST /api/v1/agent-control/jobs/{jobId}/complete`

## Worker Runtime

The standalone worker process exposes:

- `GET /healthz`
- `GET /api/v1/agent/info`
- `POST /api/v1/agent/run`
- `POST /api/v1/agent/jobs/{jobId}/cancel`
- `POST /api/v1/agent/jobs/{jobId}/cleanup`

## Cron Jobs

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/cron-jobs` | List all cron jobs |
| `POST` | `/api/v1/cron-jobs` | Create a cron job |
| `GET` | `/api/v1/cron-jobs/{id}` | Get a single cron job |
| `PUT` | `/api/v1/cron-jobs/{id}` | Update a cron job |
| `DELETE` | `/api/v1/cron-jobs/{id}` | Delete a cron job |

## Mocks

- `/internal/mock-data/`
- `/mocks/rest/`
- `POST /mocks/grpc/{suiteId}/{surfaceId}/{operationId}`
- `POST /mocks/async/{suiteId}/{surfaceId}/{operationId}`

## Authentication Requirement

Most application routes are protected. Streaming routes allow session lookup from the query token so the browser can connect to SSE endpoints.
