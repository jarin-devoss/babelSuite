---
title: Platform Settings
---

# Platform Settings

[Back to index](index.md)

Platform settings define the physical execution environment for the control plane — where suites run, where packages come from, how secrets are sourced, and how notifications are delivered. All settings are stored in `configuration.yaml` and can be managed from the **Settings** section of the UI without restarting the server.

## API

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/platform-settings` | Read current settings — passwords and secrets are redacted |
| `PUT` | `/api/v1/platform-settings` | Replace settings — requires admin session |
| `POST` | `/api/v1/platform-settings/registries/{registryId}/sync` | Trigger a registry catalog sync |
| `GET` | `/api/v1/platform-settings/plugins` | List all registered Lua plugins |
| `POST` | `/api/v1/platform-settings/plugins` | Register a new plugin — admin only |
| `PUT` | `/api/v1/platform-settings/plugins/{name}` | Update an existing plugin in-place — admin only |
| `DELETE` | `/api/v1/platform-settings/plugins/{name}` | Remove a plugin — admin only |
| `GET` | `/api/v1/platform-settings/plugins/{name}/check` | Static health check: validates trigger path and CUE schema |
| `POST` | `/api/v1/platform-settings/plugins/{name}/validate` | Validate a step config object against the plugin's CUE schema |

## Settings Sections

### Agents

Execution agents are the backends where suite steps run. The control plane routes work to agents based on the selected backend and routing tags.

| Field | Description |
|-------|-------------|
| `agentId` | Unique identifier for this agent |
| `name` | Display name |
| `type` | `local`, `remote-agent`, `remote-docker`, or `kubernetes` |
| `enabled` | Whether this agent accepts work |
| `default` | Whether this agent is used when no backend is specified — at most one enabled agent may be the default |
| `routingTags` | Tags used to route specific suite runs to this agent |
| `dockerSocket` | Docker socket path for `local` type |
| `hostUrl` | Base URL for remote agent connections |
| `tlsCert` / `tlsKey` | TLS certificate and key for secure remote connections |
| `kubeconfigPath` | Path to kubeconfig for `kubernetes` type |
| `targetNamespace` | Kubernetes namespace to dispatch step pods into |
| `serviceAccountToken` | Kubernetes service account token |
| `apisixSidecar` | APISIX gateway sidecar configuration — required for all agents |

The `apisixSidecar` object:

| Field | Description |
|-------|-------------|
| `image` | APISIX container image (e.g. `apache/apisix:3.9.0-debian`) |
| `configMountPath` | Path where APISIX reads its config inside the container |
| `listenPort` | Port APISIX listens on for proxied requests |
| `adminPort` | Port APISIX exposes for runtime configuration |
| `capabilities` | Protocols this sidecar handles — e.g. `rest`, `grpc`, `kafka`, `async`, `websocket` |

Runtime-populated fields (read-only):

| Field | Description |
|-------|-------------|
| `status` | Current agent status |
| `registeredAt` | When the agent last registered with the control plane |
| `lastHeartbeatAt` | Most recent heartbeat timestamp |
| `runtimeCapabilities` | Capabilities reported by the agent at registration |

### Registries

OCI registry entries drive the catalog. The control plane scans registered registries for suite packages and reusable modules.

| Field | Description |
|-------|-------------|
| `registryId` | Unique identifier |
| `name` | Display name |
| `provider` | `zot`, `ghcr`, `ecr`, `harbor`, `jfrog`, or `generic` |
| `registryUrl` | Base URL (e.g. `http://localhost:5000`) |
| `username` | Registry username |
| `secret` | Password or access token — stored on disk, never returned by the API |
| `repositoryScope` | Repository path prefix to include (e.g. `core-platform/*`, `*` for all) |
| `region` | Cloud region, required for ECR |
| `allowLocalNetwork` | Allow the URL to resolve to a private IP — needed for local development registries |
| `syncStatus` | `Indexed` or `Pending` |
| `lastSyncedAt` | Timestamp of the most recent successful sync |

### Secrets

The secrets section configures how the control plane sources runtime secrets and what global values are injected into every suite run.

| Field | Description |
|-------|-------------|
| `provider` | `none`, `vault`, or `aws-secrets-manager` |
| `vaultAddress` | Vault server URL |
| `vaultNamespace` | Vault namespace |
| `vaultRole` | Vault AppRole or Kubernetes role |
| `awsRegion` | AWS region for Secrets Manager |
| `secretPrefix` | Path prefix for secret lookups (e.g. `kv/platform`) |
| `globalOverrides` | List of key-value pairs injected into every execution as environment variables |

Global override fields:

| Field | Description |
|-------|-------------|
| `key` | Environment variable name |
| `value` | Value to inject |
| `description` | Human-readable note |
| `sensitive` | Mask in logs and the UI when `true` |

### Notifications

The notifications section configures outbound channels used by cron job reports. Changes take effect on the next scheduled run — no server restart required.

#### SMTP

| Field | Description |
|-------|-------------|
| `smtp.host` | SMTP server hostname (e.g. `smtp.sendgrid.net`) |
| `smtp.port` | SMTP port — `587` for STARTTLS, `465` for TLS |
| `smtp.username` | SMTP authentication username |
| `smtp.password` | SMTP password — stored on disk, never returned by the API. Sending an empty password on update preserves the existing value. |
| `smtp.from` | Sender address in the `From:` header (e.g. `BabelSuite <no-reply@example.com>`) |

Configure SMTP from the UI at **Settings → Notifications**, or edit `configuration.yaml` directly. See [Cron Jobs](cron-jobs.md) for how SMTP is used.

### Plugins

Lua plugins extend the suite runtime with custom verification steps that run inside the APISIX sidecar. Plugins are stored in `configuration.yaml` alongside the rest of the platform settings and are available to all suites without any package or image pull.

| Field | Description |
|-------|-------------|
| `name` | Unique identifier — used as the load target in `suite.star` (e.g. `@plugins/spice-sim`) |
| `trigger` | APISIX route path the plugin listens on (e.g. `/_babelsuite/plugins/spice-sim/start`) |
| `version` | Semantic version string stored as-is — e.g. `1.0.0`. Not auto-bumped by the API. |
| `kind` | Must be `plugin` |
| `variants` | List of variant names the plugin registers as APISIX Lua extensions |
| `operations` | Optional list of allowed operation names — the suite call must match one |
| `lua` | Full Lua source embedded verbatim into the generated `apisix.yaml` |
| `schema` | Optional CUE schema; step config is validated against it before dispatch |
| `star` | Optional Starlark snippet that re-exports the plugin's call signatures to `suite.star` |
| `deprecated` | When `true`, the plugin is flagged in health checks but still dispatched |

#### Plugin response contract

The Lua plugin's HTTP handler must return a JSON body. BabelSuite reads the following fields:

| Field | Type | Description |
|-------|------|-------------|
| `passed` | bool | Whether the step should be considered successful |
| `findings` | array | List of finding objects — each should have at least `message` |
| `summary` | string | One-line summary emitted as a log line at the level specified by `level` |
| `level` | string | Log level for the summary line: `info`, `debug`, `warn`, or `error`. Defaults to `info` if omitted. |
| `stderr` | string | Optional raw stderr from the plugin process, shown in the execution log |

#### APISIX sidecar lifecycle

Each unique (suite, profile) pair gets one long-lived APISIX sidecar container. The sidecar starts the first time the pair is executed and remains up across all subsequent executions of the same combination. Plugin Lua code is embedded into the sidecar's `apisix.yaml` at startup — the sidecar must be restarted (by removing its Docker container) for plugin changes to take effect on an already-running sidecar.

The sidecar runs in standalone mode: `deployment.role: data_plane` with `config_provider: yaml`. No etcd is involved.

## UI Pages

| Route | Description |
|-------|-------------|
| `/settings` | Settings overview |
| `/settings/general` | Platform mode and description |
| `/settings/agents` | Add, edit, and remove execution agents |
| `/settings/registries` | Add, edit, remove, and sync OCI registries |
| `/settings/secrets` | Configure secrets provider and global overrides |
| `/settings/notifications` | Configure SMTP for cron job email reports |
| `/settings/plugins` | Register, edit, and remove Lua plugins |

All settings pages are admin-only.

## File Storage

Platform settings are stored in the file referenced by `PLATFORM_SETTINGS_FILE` (default: `configuration.yaml`). The file is read at request time — writes from the UI take effect immediately without a server restart. File operations are mutex-protected for concurrent safety.
