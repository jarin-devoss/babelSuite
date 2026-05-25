---
title: Operations
---

# Operations

[Back to index](index.md)

This page covers health probes, telemetry, and the cache and datastore configuration needed to run BabelSuite in production.

---

## Health Endpoints

The control plane exposes both short and namespaced paths for each probe.

| Endpoint | Description |
|----------|-------------|
| `GET /healthz` | Process liveness — returns `200 OK` if the server is running |
| `GET /readyz` | Full readiness report — fails with `503` if any required subsystem is unhealthy |
| `GET /readyz/{subsystem}` | Check a single subsystem by name |
| `GET /api/v1/system/healthz` | Same as `/healthz`, namespaced under the API prefix |
| `GET /api/v1/system/readyz` | Same as `/readyz`, namespaced under the API prefix |
| `GET /api/v1/system/readyz/{subsystem}` | Single-subsystem check, namespaced |

### Readiness Subsystems

`/readyz` checks each subsystem and returns a JSON report. Required subsystems fail the overall readiness when unhealthy; optional subsystems are reported but do not block.

| Subsystem | Required | Description |
|-----------|----------|-------------|
| `database` | Yes | Primary datastore connectivity |
| `platform` | Yes | Platform settings file readable |
| `profiles` | Yes | Profile store accessible |
| `agents` | No | At least one registered agent (when agent backends are configured) |
| `suites` | No | At least one launchable suite detected |
| `cache` | No | Redis connectivity |
| `telemetry` | No | OTLP exporter reachable |

---

## Telemetry

BabelSuite exports OpenTelemetry traces and metrics from both the control plane and the frontend.

### Example Configuration

```bash
OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4318
OTEL_SERVICE_NAME=babelsuite-control-plane
OTEL_EXPORTER_OTLP_INSECURE=true
OTEL_RESOURCE_ATTRIBUTES=env=production,team=platform
```

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | — | OTLP collector endpoint (HTTP or gRPC) |
| `OTEL_SERVICE_NAME` | `babelsuite` | Service name reported to the collector |
| `OTEL_EXPORTER_OTLP_INSECURE` | `false` | Skip TLS verification (for local collectors) |
| `OTEL_EXPORTER_OTLP_HEADERS` | — | Additional headers sent with each OTLP request |
| `OTEL_RESOURCE_ATTRIBUTES` | — | Comma-separated `key=value` resource attributes |

!!! note
    If telemetry is not configured, the readiness report marks the `telemetry` subsystem as `disabled` — the server continues to run normally.

---

## Cache Layer

Redis is optional. When present, it accelerates:

- Platform settings and profile reads
- Execution runtime state
- Catalog discovery results
- Coordination fast paths

If Redis is not configured, the control plane falls back to the primary store for all reads. The readiness report reflects the cache state as `disabled` rather than failing.

### Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `REDIS_URL` | — | Redis connection string (e.g. `redis://localhost:6379`) |

---

## Primary Datastore

BabelSuite supports two primary store backends. Select with the `DB_DRIVER` environment variable.

| Driver | Variable | Description |
|--------|----------|-------------|
| `mongo` | `MONGODB_URI` | MongoDB (default for local development) |
| `postgres` | `DATABASE_URL` | PostgreSQL |

### MongoDB

```bash
DB_DRIVER=mongo
MONGODB_URI=mongodb://localhost:27017/babelsuite
```

### PostgreSQL

```bash
DB_DRIVER=postgres
DATABASE_URL=postgres://user:password@localhost:5432/babelsuite?sslmode=disable
```

---

## Middleware Stack

Every API request passes through the shared middleware stack in this order:

```
CORS → Request IDs → Auth Session → OTel Trace → HTTP Metrics → Audit
```

| Layer | What it does |
|-------|-------------|
| CORS | Enforces the `FRONTEND_URL` origin allowlist |
| Request IDs | Attaches a unique ID to each request for log correlation |
| Auth Session | Verifies JWT and populates request context |
| OTel Trace | Starts a span and propagates trace context |
| HTTP Metrics | Records request duration and status code |
| Audit | Logs writes and sensitive reads to the audit trail |

---

## Worker Health

The remote agent process exposes its own liveness probe and a small control API for managing in-progress steps.

| Endpoint | Description |
|----------|-------------|
| `GET /healthz` | Worker liveness check |
| `GET /api/v1/agent/info` | Agent identity and runtime capabilities |
| `POST /api/v1/agent/run` | Start a step |
| `POST /api/v1/agent/jobs/{jobId}/cancel` | Cancel an in-progress step |
| `POST /api/v1/agent/jobs/{jobId}/cleanup` | Clean up resources from a completed step |

---

## Kubernetes Deployment (Helm)

A production-grade Helm chart is included at `deploy/helm/babelsuite/`.

### Quick install

```bash
helm install babelsuite ./deploy/helm/babelsuite \
  --namespace babelsuite --create-namespace \
  --set secrets.jwtSecret="$(openssl rand -hex 32)" \
  --set secrets.adminEmail="admin@example.com" \
  --set secrets.adminPassword="changeme" \
  --set config.frontendURL="https://babelsuite.example.com" \
  --set mongodb.uri="mongodb://mongo:27017" \
  --set ingress.enabled=true \
  --set ingress.className=nginx \
  --set "ingress.hosts[0].host=babelsuite.example.com" \
  --set "ingress.hosts[0].paths[0].path=/" \
  --set "ingress.hosts[0].paths[0].pathType=Prefix"
```

### Production secrets

Never pass secrets via `--set` in CI. Use an existing Secret instead:

```bash
kubectl create secret generic babelsuite-secrets \
  --from-literal=JWT_SECRET="$(openssl rand -hex 32)" \
  --from-literal=ADMIN_EMAIL=admin@example.com \
  --from-literal=ADMIN_PASSWORD=changeme \
  -n babelsuite

helm install babelsuite ./deploy/helm/babelsuite \
  --namespace babelsuite \
  --set secrets.existingSecret=babelsuite-secrets \
  --set config.frontendURL=https://babelsuite.example.com \
  --set mongodb.uri=mongodb://mongo:27017
```

### Autoscaling

```yaml
autoscaling:
  enabled: true
  minReplicas: 2
  maxReplicas: 10
  targetCPUUtilizationPercentage: 70
```

### Redis + Telemetry

```yaml
redis:
  enabled: true
  addr: redis:6379

telemetry:
  enabled: true
  endpoint: http://otel-collector:4318
  serviceName: babelsuite
  resourceAttributes: env=production
```

---

## Database Migrations

BabelSuite's MongoDB store creates collections and indexes automatically on startup — no separate migration step is required for MongoDB.

### PostgreSQL

When using the PostgreSQL driver (`DB_DRIVER=postgres`), schema migrations are managed via embedded SQL files applied at startup. To inspect or run them manually:

```bash
# List pending migrations
DATABASE_URL=postgres://... ./babelsuite migrate status

# Apply all pending migrations
DATABASE_URL=postgres://... ./babelsuite migrate up

# Roll back one step
DATABASE_URL=postgres://... ./babelsuite migrate down
```

Keep `DATABASE_URL` in a secret and never commit it to version control.

### Backup and restore

**MongoDB**

```bash
# Backup
mongodump --uri "$MONGODB_URI" --db babelsuite --out /backup/$(date +%Y%m%d)

# Restore
mongorestore --uri "$MONGODB_URI" --db babelsuite /backup/20260526/babelsuite
```

**PostgreSQL**

```bash
# Backup
pg_dump "$DATABASE_URL" -Fc -f /backup/babelsuite-$(date +%Y%m%d).dump

# Restore
pg_restore "$DATABASE_URL" -d babelsuite /backup/babelsuite-20260526.dump
```

Automate backups via a Kubernetes CronJob, a managed-database snapshot schedule, or a dedicated backup tool.

---

## See Also

- [Architecture](architecture.md) — system layers and component relationships
- [Configuration](configuration.md) — all environment variables
- [Agents](agents.md) — worker process lifecycle and endpoints
