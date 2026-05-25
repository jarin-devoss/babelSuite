---
title: Troubleshooting
---

# Troubleshooting

[Back to index](index.md)

---

## Control Plane Won't Start

### `JWT_SECRET must be set to a secure random value`

The server requires a non-empty, non-default `JWT_SECRET`. Generate one and set it:

```bash
export JWT_SECRET=$(openssl rand -hex 32)
```

### `platform settings: ...` on startup

The control plane cannot read `configuration.yaml` at the configured path. Check:

1. The file exists at the path set by `PLATFORM_SETTINGS_FILE` (default: `configuration.yaml` in the working directory).
2. The process has read permission.
3. The YAML is valid — run `cat configuration.yaml | python3 -c "import sys,yaml; yaml.safe_load(sys.stdin)"` to validate.

### MongoDB connection refused

```
store: server selection error: server 127.0.0.1:27017 ... connection refused
```

MongoDB is not running or `MONGO_URI` is wrong. Check:

```bash
mongosh "$MONGO_URI" --eval "db.adminCommand({ping:1})"
```

---

## Readiness Probe Failing (`/readyz` returns 503)

Check which subsystem is unhealthy:

```bash
curl -s http://localhost:8090/readyz | jq .
```

| Subsystem | Common cause |
|-----------|-------------|
| `database` | MongoDB/Postgres unreachable |
| `platform` | `configuration.yaml` missing or unreadable |
| `profiles` | `babelsuite-profiles.yaml` unreadable |
| `agents` | No agents registered (optional — only required when agent backends are configured) |

---

## Execution Stuck in `pending`

1. **No available backend** — check that at least one backend is healthy: `curl /readyz/agents`.
2. **Agent not heartbeating** — the agent process may have crashed. Check agent logs and restart it.
3. **Suite not found in catalog** — the OCI registry may be unreachable. Check `curl /readyz/suites`.

---

## Step Fails Immediately With `context canceled`

The execution was cancelled (e.g. by a `reap` call or a timeout). Check:

1. Whether a suite-level timeout (`timeout:` field) is set too aggressively.
2. Whether the step has a dependency that failed, causing the execution engine to cancel downstream steps.

---

## Mock Service Not Receiving Traffic

1. Confirm the mock node started: check the execution log for `[mock-name] Mock server listening on ...`.
2. Confirm the step's `target:` field uses the symbolic service name, not `localhost`.
3. Check that the APISIX sidecar started successfully if the suite uses an API gateway.

---

## CLI: `error: no saved session`

Run `babelctl login` first. If you've set `--server` but not logged in, the token won't exist.

## CLI: `error: connection refused`

The `--server` URL is wrong or the control plane isn't running. Verify:

```bash
curl http://localhost:8090/healthz
```

## CLI: `error: unauthorized`

The session token has expired. Run `babelctl login` again to refresh it.

---

## OTel / Traces Missing

If traces aren't appearing in your collector:

1. Check `OTEL_EXPORTER_OTLP_ENDPOINT` is set and the collector is reachable.
2. Verify `/readyz/telemetry` — if it returns `disabled`, no endpoint is configured.
3. For self-signed TLS: set `OTEL_EXPORTER_OTLP_INSECURE=true` or mount the CA bundle.

---

## See Also

- [Operations](operations.md) — health endpoints, middleware, TLS setup
- [Configuration](configuration.md) — all environment variables
- [Agents](agents.md) — worker process lifecycle
