---
title: Configuration
---

# Configuration

[Back to index](index.md)

BabelSuite is configured through three layers:

| Layer | File | What it controls |
|-------|------|-----------------|
| Process environment | `.env` (repo root) | Ports, auth, datastores, telemetry |
| Platform settings | `configuration.yaml` | Agents, registries, secrets, notifications |
| Profile store | `babelsuite-profiles.yaml` | Managed launch-time profile records |

The backend loads `.env` automatically on startup. Platform settings are read from disk at request time — changes take effect without restarting the server.

---

## Environment Variables

### Application

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8090` | Control plane HTTP listen port |
| `FRONTEND_URL` | `http://localhost:5173` | CORS allowed origin — must match the browser-facing URL |
| `VITE_API_URL` | `http://localhost:8090` | API base URL baked into the frontend bundle at build time. Set it to `/` for local development — see the note below. |
| `PUBLIC_API_URL` | *(same as VITE_API_URL)* | Override for the public-facing API URL when behind a proxy |
| `JWT_SECRET` | **required** | Cryptographic secret for JWT signing — must be a secure random value |
| `HTTP_READ_HEADER_TIMEOUT` | `5s` | Maximum time to read request headers |
| `HTTP_READ_TIMEOUT` | `30s` | Maximum time to read the full request body |
| `HTTP_WRITE_TIMEOUT` | `2m` | Maximum time to write a response |
| `HTTP_IDLE_TIMEOUT` | `2m` | Keep-alive idle connection timeout |
| `TRUSTED_PROXIES` | *(empty)* | Comma-separated CIDR ranges whose `X-Forwarded-*` headers are trusted |
| `BABELSUITE_LOCAL_MAX_STEPS` | number of CPUs | Steps the **local Docker** backend runs at once. Each backend has its own limit — Kubernetes leaves it to the cluster scheduler, remote agents declare theirs with `AGENT_MAX_JOBS` — so a slow step on one backend never holds back another. See [Agents](agents.md#how-steps-are-scheduled) |

!!! warning "Use a relative `VITE_API_URL` locally"
    Setting it to an absolute URL such as `http://localhost:8090` makes the
    browser's API calls cross-origin from the Vite dev server on port 5173. The
    `csrf_token` cookie is then discarded, and sign-in fails with
    `403 CSRF token missing.` Set `VITE_API_URL=/` so requests go through the
    Vite proxy and stay same-origin.

### Authentication

| Variable | Default | Description |
|----------|---------|-------------|
| `ADMIN_EMAIL` | *(empty)* | Email address seeded as the initial admin account on first startup |
| `ADMIN_PASSWORD` | *(empty)* | Password for the seeded admin account |
| `AUTH_PASSWORD_LOGIN_ENABLED` | `true` | Allow sign-in with email and password |
| `AUTH_SIGNUP_ENABLED` | `true` | Allow new accounts to be created via the sign-up page |
| `MOCK_SHARED_SECRET` | *(empty)* | The token a suite's gateway presents to the mock engine, which requires it on every mock call. When empty it is derived from `JWT_SECRET`, so it differs for every deployment and survives a restart. Set it to choose the value, or to keep gateways valid across a `JWT_SECRET` rotation |
| `AGENT_SHARED_SECRET` | *(empty)* | Shared secret used to authenticate remote agent workers |
| `BABELSUITE_ENGINE_ADDR` | `host.docker.internal:<PORT>` | Where a suite's gateway reaches this control plane's mock engine from inside its container. Set from `PORT` when not given, so only a control plane behind a different address needs it |
| `BABELSUITE_GRPC_MOCK_PORT` | `8095` | The port of the gRPC mock front end, which a suite's gateway forwards every gRPC call to. It is a port of its own because gRPC is HTTP/2 and the API's listener does not speak it without TLS |
| `BABELSUITE_ENGINE_GRPC_ADDR` | `host.docker.internal:<BABELSUITE_GRPC_MOCK_PORT>` | Where a suite's gateway reaches that front end, for a control plane behind a different address |
| `BABELSUITE_REAP_ON_START` | `true` | On start, remove what runs this host's control plane no longer drives left behind (containers, networks, volumes, workspace directories). `false` turns it off. See [Execution Status And Restarts](execution.md#execution-status-and-restarts) |

### SSO / OIDC

Single sign-on is on when `OIDC_ENABLED=true`. `OIDC_ISSUER_URL`, `OIDC_CLIENT_ID` and `OIDC_REDIRECT_URL` are then required: the server refuses to start with one missing and names it, rather than starting with a half-configured SSO button. The issuer is discovered when the first user signs in, so an identity provider that is down at startup does not stop the server.

| Variable | Default | Description |
|----------|---------|-------------|
| `OIDC_ENABLED` | `false` | Enable OpenID Connect login |
| `OIDC_PROVIDER_ID` | *(empty)* | Internal identifier for the OIDC provider |
| `OIDC_PROVIDER_NAME` | *(empty)* | Display name shown on the SSO button |
| `OIDC_ISSUER_URL` | *(empty)* | OIDC issuer URL (e.g. `https://accounts.google.com`) |
| `OIDC_CLIENT_ID` | *(empty)* | OAuth2 client ID |
| `OIDC_CLIENT_SECRET` | *(empty)* | OAuth2 client secret |
| `OIDC_REDIRECT_URL` | *(empty)* | Backend callback URL (e.g. `http://localhost:8090/api/v1/auth/oidc/callback`) |
| `OIDC_FRONTEND_CALLBACK_URL` | `<FRONTEND_URL>/auth/callback` | Frontend callback URL (e.g. `http://localhost:5173/auth/callback`) |
| `OIDC_SCOPES` | *(empty)* | Comma-separated scopes (e.g. `openid,profile,email,groups`) |
| `OIDC_PKCE_ENABLED` | `true` | Enable PKCE code challenge flow |
| `OIDC_EMAIL_CLAIM` | `email` | JWT claim to read the user's email from |
| `OIDC_NAME_CLAIM` | `name` | JWT claim to read the user's display name from |
| `OIDC_GROUPS_CLAIM` | `groups` | JWT claim to read the user's group memberships from |
| `OIDC_ADMIN_GROUPS` | *(empty)* | Comma-separated group names that grant admin privileges |
| `AUTH_STATE_SECRET` | derived from `JWT_SECRET` | Secret for signing the OIDC state cookie. When not set it is derived from `JWT_SECRET`, so it differs for every deployment and is the same after a restart |

### Datastore

| Variable | Default | Description |
|----------|---------|-------------|
| `DB_DRIVER` | `mongo` | The datastore: `mongo` or `postgres`. Anything else stops the server at startup. `GET /healthz` reports the driver in use as `dbDriver` |
| `MONGO_URI` | `mongodb://localhost:27017` | MongoDB connection string (`DB_DRIVER=mongo`) |
| `MONGO_DB` | `babelsuite` | MongoDB database name |
| `POSTGRES_URL` | *(empty)* | PostgreSQL connection string, e.g. `postgres://user:password@host:5432/babelsuite` (`DB_DRIVER=postgres`, required then) |

Both datastores hold the same data and are held to the same contract by one test that runs against each (`internal/store/storetest`). With `DB_DRIVER=postgres` the schema is created and migrated when the server starts (a lock keeps several replicas from migrating at once). Data is not moved between datastores: switching starts with an empty one. The Helm chart deploys MongoDB only; to run on PostgreSQL there, point `POSTGRES_URL` at a database you provide.

### Cache (Redis)

Redis is optional. When `REDIS_ADDR` is unset, BabelSuite operates without a cache.

| Variable | Default | Description |
|----------|---------|-------------|
| `REDIS_ADDR` | *(empty)* | Redis host and port (e.g. `localhost:6379`) — leave blank to disable |
| `REDIS_PASSWORD` | *(empty)* | Redis authentication password |
| `REDIS_DB` | `0` | Redis logical database index |
| `REDIS_PREFIX` | `babelsuite` | Key prefix for all cache entries |
| `CACHE_TTL_WORKSPACE` | *(server default)* | Workspace record cache lifetime |
| `CACHE_TTL_FAVORITES` | *(server default)* | Catalog favorites cache lifetime |
| `CACHE_TTL_PROFILES` | *(server default)* | Profile records cache lifetime |
| `CACHE_TTL_PLATFORM` | *(server default)* | Platform settings cache lifetime |
| `CACHE_TTL_CATALOG` | `45s` | Catalog package cache lifetime |
| `CACHE_TTL_EXECUTION_RUNTIME` | `24h` | Execution runtime metadata cache lifetime |

Durations use Go notation: `30s`, `5m`, `1h`.

### Platform Files

| Variable | Default | Description |
|----------|---------|-------------|
| `PLATFORM_SETTINGS_FILE` | `configuration.yaml` | Path to agents, registries, secrets, and notifications config |
| `PROFILES_FILE` | `babelsuite-profiles.yaml` | Path to managed profile records |
| `AGENT_RUNTIME_FILE` | `babelsuite-agents.yaml` | Path to live agent heartbeat and runtime metadata |

Relative paths are resolved first from the working directory, then from the parent directory.

### Telemetry (OpenTelemetry)

| Variable | Default | Description |
|----------|---------|-------------|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | *(empty)* | OTLP collector endpoint (e.g. `http://localhost:4317`) — leave blank to disable |
| `OTEL_SERVICE_NAME` | `babelsuite-backend` | Service name attached to all traces and metrics |
| `OTEL_EXPORTER_OTLP_INSECURE` | `false` | Skip TLS verification for the OTLP connection |
| `OTEL_EXPORTER_OTLP_HEADERS` | *(empty)* | Additional headers for the OTLP exporter (e.g. `authorization=Bearer <token>`) |
| `OTEL_RESOURCE_ATTRIBUTES` | *(empty)* | Comma-separated `key=value` pairs attached to every span |

---

## `configuration.yaml`

The platform settings file defines the physical execution environment. It is read and written by the control plane at runtime — changes take effect without restarting the server. Edit it directly or use the **Settings** UI at `/settings`.

### Top-level Structure

```yaml
mode: local
description: ''
agents: []
registries: []
secrets:
  provider: none
  vaultAddress: ''
  vaultNamespace: ''
  vaultRole: ''
  awsRegion: ''
  secretPrefix: ''
  globalOverrides: []
notifications:
  smtp:
    host: ''
    port: 587
    username: ''
    password: ''
    from: ''
updatedAt: 2026-01-01T00:00:00Z
```

### `mode`

The deployment mode decides which of the enabled `agents` runs may use. It applies to every launch — those that pick a backend, those routed by a profile's `agent.tags`, and those left to the default — and to the backends the launch screen offers.

| Value | Backends runs use |
|-------|-------------------|
| `local` (default) | Only `local` (host Docker) agents. Remote agents and Kubernetes backends stay idle even when enabled. |
| `remote` | Only `remote-agent`, `remote-docker` and `kubernetes` agents. The host's Docker is never used, so the control plane only schedules. |
| `hybrid` | Every enabled agent. A profile's `agent.tags` route it to a backend carrying them; the rest go to the default agent. |

A launch the mode rules out is refused with `422`, naming the backend and the mode: asking for `kubernetes-burst` in `local` mode, or running a profile whose `agent.tags` only an excluded backend carries. Settings whose mode leaves no enabled agent are rejected when saved.

### Agents

Each entry in `agents` defines one execution target.

| Field | Description |
|-------|-------------|
| `agentId` | Unique identifier |
| `name` | Display name |
| `type` | `local`, `remote-agent`, `remote-docker`, or `kubernetes` |
| `enabled` | Whether this backend is available for suite runs |
| `default` | Whether this backend is selected when no backend is specified — at most one enabled agent may be the default |
| `description` | Free-text description |
| `routingTags` | Tags this agent carries, matched against a profile's [`agent.tags`](profiles.md#agenttags) when a launch names no backend |
| `dockerSocket` | Docker socket path for `local` type (e.g. `/var/run/docker.sock`) |
| `hostUrl` | Base URL for `remote-agent` and `remote-docker` types |
| `tlsCert` | PEM-encoded TLS certificate for remote connections |
| `tlsKey` | PEM-encoded TLS private key for remote connections |
| `kubeconfigPath` | Path to kubeconfig for `kubernetes` type |
| `targetNamespace` | Kubernetes namespace to dispatch steps into |
| `serviceAccountToken` | Kubernetes service account token |
| `apisixSidecar.image` | APISIX container image used for the mock gateway sidecar |
| `apisixSidecar.configMountPath` | Path of the **file** APISIX reads its routes from. The suite's generated `apisix.yaml` is bind-mounted onto exactly this path, so it must name a file, not a directory. Default `/usr/local/apisix/conf/apisix.yaml`. |
| `apisixSidecar.listenPort` | Recorded with the sidecar settings; the sidecar listens on 9080 inside its container |

### Registries

Each entry in `registries` is an OCI registry that the catalog scans for suite and module packages.

| Field | Description |
|-------|-------------|
| `registryId` | Unique identifier |
| `name` | Display name |
| `provider` | `zot`, `ghcr`, `ecr`, `harbor`, `jfrog`, or `generic` |
| `registryUrl` | Base URL of the registry (e.g. `http://localhost:5000`) |
| `username` | Registry username |
| `secret` | Registry password or access token |
| `repositoryScope` | Repository path prefix to discover (e.g. `core-platform/*`) |
| `region` | Cloud region for ECR registries |
| `allowLocalNetwork` | Allow the registry URL to resolve to a private IP address |
| `syncStatus` | Current sync state — `Indexed` or `Pending` |
| `lastSyncedAt` | Timestamp of the most recent successful sync |

### Secrets

| Field | Description |
|-------|-------------|
| `provider` | `none`, `vault`, or `aws-secrets-manager` |
| `vaultAddress` | Vault server URL (e.g. `https://vault.internal.company.com`) |
| `vaultNamespace` | Vault namespace |
| `vaultRole` | Vault AppRole or Kubernetes role used by the control plane |
| `awsRegion` | AWS region for Secrets Manager (e.g. `us-east-1`) |
| `secretPrefix` | Path prefix for secrets (e.g. `kv/platform`) |
| `globalOverrides` | List of key-value pairs injected into every suite run |

Each global override has the following fields:

| Field | Description |
|-------|-------------|
| `key` | Environment variable name (e.g. `HTTPS_PROXY`) |
| `value` | Value to inject |
| `description` | Human-readable note |
| `sensitive` | When `true`, the value is masked in logs and the UI |

### Notifications

The notifications section configures outbound channels used by cron job reports. Changes take effect on the next scheduled run without restarting the server.

#### SMTP

| Field | Description |
|-------|-------------|
| `smtp.host` | SMTP server hostname (e.g. `smtp.sendgrid.net`) |
| `smtp.port` | SMTP port — typically `587` (STARTTLS) or `465` (TLS) |
| `smtp.username` | SMTP authentication username |
| `smtp.password` | SMTP authentication password — stored on disk, never returned by the API |
| `smtp.from` | Sender address shown in the `From:` header (e.g. `BabelSuite <no-reply@example.com>`) |

Configure SMTP from the UI at **Settings → Notifications** or edit `configuration.yaml` directly.

---

## Recommended Local Setup

For a local workstation:

```bash
# .env
PORT=8090
FRONTEND_URL=http://localhost:5173

# Relative on purpose — see the VITE_API_URL note above.
VITE_API_URL=/

JWT_SECRET=change-me-to-a-secure-random-value
ADMIN_EMAIL=admin@babelsuite.test
ADMIN_PASSWORD=admin

MONGO_URI=mongodb://localhost:27017
MONGO_DB=babelsuite

PLATFORM_SETTINGS_FILE=configuration.yaml
PROFILES_FILE=babelsuite-profiles.yaml

AUTH_PASSWORD_LOGIN_ENABLED=true
AUTH_SIGNUP_ENABLED=true
```

```yaml
# configuration.yaml
mode: local
agents:
  - agentId: local-docker
    name: Local Docker
    type: local
    enabled: true
    default: true
    dockerSocket: /var/run/docker.sock
    apisixSidecar:
      image: apache/apisix:3.9.0-debian
      configMountPath: /usr/local/apisix/conf/apisix.yaml
      listenPort: 9080
registries:
  - registryId: local-zot
    name: Local Registry
    provider: zot
    registryUrl: http://localhost:5000
    repositoryScope: '*'
```
