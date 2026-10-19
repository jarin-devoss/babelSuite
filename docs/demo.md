---
title: Demo
---

# Demo

[Back to index](index.md)

`demo/` brings up a local BabelSuite you can launch suites in, from a clean
checkout: the infrastructure in containers, and the bundled examples loaded into
it — 14 suites, 5 modules, and 11 Lua plugins.

## What runs where

| Component | Where | Why |
|-----------|-------|-----|
| MongoDB, Redis, Zot | containers (`demo/docker-compose.yml`) | pure dependencies, nothing to build |
| Backend | **host** — `go run ./cmd/server` | drives the Docker daemon to run steps |
| Frontend | **host** — `npm run dev` | Vite dev server |

!!! note "Why the backend is not containerised"
    It runs each suite step as a sibling container and bind-mounts a host
    directory into it as the shared workspace. From inside a container it would
    hand the daemon paths that exist only in its own filesystem, so the
    workspace mount would quietly point at nothing.

## Setup

```bash
# 1. infrastructure
cd demo
docker compose --env-file ../.env up -d

# 2. settings + example content  (safe to re-run)
cd ..
cp demo/configuration.demo.yaml configuration.yaml   # optional: created from it if missing
cp .env.example .env    # at the repo root; then set JWT_SECRET
python demo/bootstrap.py

# 3. the app
cd backend  && go run ./cmd/server             # http://localhost:8090
cd frontend && npm install && npm run dev      # http://localhost:5173
```

`JWT_SECRET` must be set or the server refuses to start — `openssl rand -hex 32`.
Sign in with `ADMIN_EMAIL` / `ADMIN_PASSWORD` from `.env`
(`admin@babelsuite.test` / `admin` by default). The admin is seeded on first
startup only, and only when both are set.

### One command instead of two

The same bootstrap runs in a container if you would rather not run Python on the
host:

```bash
cd demo
docker compose --env-file ../.env --profile seed run --rm seed
```

## What bootstrap does

Three idempotent steps. It touches no database and needs no running backend or
session, so it works before the control plane has ever started.

1. Waits for the registry to answer on `/v2/`.
2. [`scripts/push-to-zot.py`](https://github.com/jarin-devoss/babelSuite/tree/main/scripts/push-to-zot.py)
   packs each `examples/oci-suites/<name>` and `examples/oci-modules/<name>`
   into an OCI artifact and uploads it.
3. [`scripts/install-plugins.py`](https://github.com/jarin-devoss/babelSuite/tree/main/scripts/install-plugins.py)
   reads each `examples/oci-plugins/<name>/plugin.yaml`, inlines its Lua, CUE
   schema and Starlark wrapper, and writes them into the `plugins:` list of
   `configuration.yaml`.

!!! warning "The backend reads the catalog at startup"
    Editing anything under `examples/` has no effect on a run until it is pushed
    **and the backend restarted**:

    ```bash
    python demo/bootstrap.py                            # everything
    python scripts/push-to-zot.py --suite payment-suite # just one
    ```

    `GET /api/v1/suites/<id>` lists the package's `sourceFiles` — the quickest
    way to confirm an edit reached the backend. A changed plugin also needs its
    sidecar removed (`docker rm -f babel-sidecar-<suite>-<profile>`) so APISIX
    reloads the Lua.

## Good first runs

| Suite | Why |
|-------|-----|
| `gateway-check` | Smallest complete package — one mock, one test step. Finishes in seconds. |
| `plugin-span-demo` | Plugin span attributes, backed entirely by CUE mocks. Starts no containers. |
| `electronics-lab` | Three Lua plugins against mock surfaces — SPICE, Verilog, control systems. |
| `payment-suite` | A realistic shape: Postgres, migrations, a mock, traffic phases, tests. |
| `composite-readiness` | Composes four other suites — around 70 nodes. |

## Telemetry

The optional collector gives the `runner.run` spans somewhere to land, including
the `runner.plugin.*` attributes a Lua plugin reported:

```bash
cd demo
docker compose --env-file ../.env --profile telemetry up -d
# then in the repo-root .env:
#   OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
docker compose logs -f otel-collector
```

## What will not run

`demo/configuration.demo.yaml` sets `secrets.provider: none`, so a profile whose
inheritance chain declares a Vault `secretRef` fails at boot — Vault resolution
needs `VAULT_TOKEN` and says so. That is `fleet-control-room` (every profile, via
its managed base profile), `identity-broker/canary.yaml`, and `payment-suite`'s
`staging.yaml` and `year.yaml`. The other 22 suite/profile pairs run without a
secret backend.

The demo config also ships `globalOverrides: []` on purpose. A global override
is injected into every step of every suite, so an unreachable value — a proxy on
a hostname that does not resolve, say — breaks every step that touches the
network, and the failure surfaces inside the step rather than in settings.

## Ports

All overridable in the repo-root `.env`, which compose reads via
`--env-file ../.env`:

| Variable | Default |
|----------|---------|
| `DEMO_MONGO_PORT` | `27017` |
| `DEMO_REDIS_PORT` | `6379` |
| `DEMO_ZOT_PORT` | `5000` |

!!! warning "Leave `DEMO_ZOT_PORT` at 5000"
    Every `examples/oci-suites/*/metadata.yaml` pins its repository as
    `localhost:5000/<path>`, and the `dependencies.yaml` refs do too, so another
    port leaves the catalog pointing at a registry that is not there.

## Teardown

```bash
cd demo
docker compose --env-file ../.env down      # keep the registry and database contents
docker compose --env-file ../.env down -v   # discard them too
```

An interrupted suite run can leave its own containers and networks behind. They
are labelled, and the [Environments](environments.md) page reaps them. By hand:

```bash
docker rm -f $(docker ps -aq --filter 'name=babel-run')
docker network ls --format '{{.Name}}' | grep '^babel-net-run-' | xargs -r docker network rm
```

## See Also

- [Getting Started](getting-started.md) — running the pieces without the demo stack
- [Configuration](configuration.md) — every environment variable and `configuration.yaml` field
- [Examples](examples.md) — what each example suite, module and plugin demonstrates
