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
| `VITE_API_URL` | `http://localhost:8090` | API base URL baked into the frontend bundle at build time |
| `PUBLIC_API_URL` | *(same as VITE_API_URL)* | Override for the public-facing API URL when behind a proxy |
| `JWT_SECRET` | **required** | Cryptographic secret for JWT signing — must be a secure random value |
| `HTTP_READ_HEADER_TIMEOUT` | `5s` | Maximum time to read request headers |
| `HTTP_READ_TIMEOUT` | `30s` | Maximum time to read the full request body |
| `HTTP_WRITE_TIMEOUT` | `2m` | Maximum time to write a response |
| `HTTP_IDLE_TIMEOUT` | `2m` | Keep-alive idle connection timeout |
| `TRUSTED_PROXIES` | *(empty)* | Comma-separated CIDR ranges whose `X-Forwarded-*` headers are trusted |

### Authentication

| Variable | Default | Description |
|----------|---------|-------------|
| `ADMIN_EMAIL` | *(empty)* | Email address seeded as the initial admin account on first startup |
| `ADMIN_PASSWORD` | *(empty)* | Password for the seeded admin account |
| `AUTH_PASSWORD_LOGIN_ENABLED` | `true` | Allow sign-in with email and password |
| `AUTH_SIGNUP_ENABLED` | `true` | Allow new accounts to be created via the sign-up page |
| `MOCK_SHARED_SECRET` | *(empty)* | Shared secret used to authenticate the mock service |
| `AGENT_SHARED_SECRET` | *(empty)* | Shared secret used to authenticate remote agent workers |

### SSO / OIDC

| Variable | Default | Description |
|----------|---------|-------------|
| `OIDC_ENABLED` | `false` | Enable OpenID Connect login |
| `OIDC_PROVIDER_ID` | *(empty)* | Internal identifier for the OIDC provider |
| `OIDC_PROVIDER_NAME` | *(empty)* | Display name shown on the SSO button |
| `OIDC_ISSUER_URL` | *(empty)* | OIDC issuer URL (e.g. `https://accounts.google.com`) |
| `OIDC_CLIENT_ID` | *(empty)* | OAuth2 client ID |
| `OIDC_CLIENT_SECRET` | *(empty)* | OAuth2 client secret |
| `OIDC_REDIRECT_URL` | *(empty)* | Backend callback URL (e.g. `http://localhost:8090/api/v1/auth/oidc/callback`) |
| `OIDC_FRONTEND_CALLBACK_URL` | *(empty)* | Frontend callback URL (e.g. `http://localhost:5173/auth/callback`) |
| `OIDC_SCOPES` | *(empty)* | Comma-separated scopes (e.g. `openid,profile,email,groups`) |
| `OIDC_PKCE_ENABLED` | `true` | Enable PKCE code challenge flow |
| `OIDC_EMAIL_CLAIM` | `email` | JWT claim to read the user's email from |
| `OIDC_NAME_CLAIM` | `name` | JWT claim to read the user's display name from |
| `OIDC_GROUPS_CLAIM` | `groups` | JWT claim to read the user's group memberships from |
| `OIDC_ADMIN_GROUPS` | *(empty)* | Comma-separated group names that grant admin privileges |
| `AUTH_STATE_SECRET` | *(empty)* | Secret for signing the OIDC state cookie — should be a secure random value |

### Datastore

| Variable | Default | Description |
|----------|---------|-------------|
| `DB_DRIVER` | `mongo` | Primary datastore: `mongo` or `postgres` |
| `MONGO_URI` | `mongodb://localhost:27017` | MongoDB connection string |
| `MONGO_DB` | `babelsuite` | MongoDB database name |
| `POSTGRES_DSN` | *(empty)* | PostgreSQL connection string (required when `DB_DRIVER=postgres`) |

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

### Demo Mode

| Variable | Default | Description |
|----------|---------|-------------|
| `BABELSUITE_ENABLE_DEMO` | `false` | Use bundled demo data instead of a live workspace |

When demo mode is on, suites and platform settings come from the `demo/` directory rather than from `configuration.yaml` and `examples/`.

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

| Value | Meaning |
|-------|---------|
| `local` | All execution targets are on the same machine |
| `remote` | Primary targets are remote agents or Kubernetes clusters |
| `hybrid` | Mix of local and remote targets |

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
| `routingTags` | List of tags used to route suites to specific agents |
| `dockerSocket` | Docker socket path for `local` type (e.g. `/var/run/docker.sock`) |
| `hostUrl` | Base URL for `remote-agent` and `remote-docker` types |
| `tlsCert` | PEM-encoded TLS certificate for remote connections |
| `tlsKey` | PEM-encoded TLS private key for remote connections |
| `kubeconfigPath` | Path to kubeconfig for `kubernetes` type |
| `targetNamespace` | Kubernetes namespace to dispatch steps into |
| `serviceAccountToken` | Kubernetes service account token |
| `apisixSidecar.image` | APISIX container image used for the mock gateway sidecar |
| `apisixSidecar.configMountPath` | Path where APISIX reads its configuration |
| `apisixSidecar.listenPort` | Port APISIX listens on for proxied traffic |
| `apisixSidecar.adminPort` | Port APISIX exposes for configuration updates |
| `apisixSidecar.capabilities` | Protocols the sidecar handles — e.g. `rest`, `grpc`, `kafka`, `async` |

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
VITE_API_URL=http://localhost:8090
JWT_SECRET=change-me-to-a-secure-random-value
ADMIN_EMAIL=admin@babelsuite.test
ADMIN_PASSWORD=admin

DB_DRIVER=mongo
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
      configMountPath: /usr/local/apisix/conf
      listenPort: 9080
      adminPort: 9180
      capabilities: [rest, grpc, async]
registries:
  - registryId: local-zot
    name: Local Registry
    provider: zot
    registryUrl: http://localhost:5000
    repositoryScope: '*'
```
