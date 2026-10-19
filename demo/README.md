# Demo

A local BabelSuite you can launch suites in, from a clean checkout.

This folder brings up the infrastructure — datastore, cache, OCI registry — and
loads the bundled examples into it: 14 suites, 5 modules, and 11 Lua plugins.

## What runs where

| Component | Where | Why |
|---|---|---|
MongoDB, Redis, Zot | containers, from `docker-compose.yml` | pure dependencies, nothing to build |
Backend | **host** (`go run ./cmd/server`) | drives the Docker daemon to run steps |
Frontend | **host** (`npm run dev`) | Vite dev server |

The backend is not containerised on purpose. It runs suite steps as sibling
containers and bind-mounts a host directory into each one as the shared
workspace. From inside a container it would hand the daemon paths that only
exist in its own filesystem, and the workspace mount would quietly point at
nothing.

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
cd backend  && go run ./cmd/server      # http://localhost:8090
cd frontend && npm install && npm run dev   # http://localhost:5173
```

`JWT_SECRET` must be set or the server refuses to start:

```bash
openssl rand -hex 32
```

Sign in with `ADMIN_EMAIL` / `ADMIN_PASSWORD` from `.env` — `admin@babelsuite.test`
/ `admin` by default. The admin is seeded on first startup only, and only when
both are set.

### One command instead of two

If you would rather not run Python on the host, the same bootstrap runs in a
container:

```bash
cd demo
docker compose --env-file ../.env --profile seed run --rm seed
```

It mounts the repo, installs `pyyaml`, and runs `bootstrap.py` against the
registry over the compose network.

## What bootstrap does

Three idempotent steps, no database and no running backend required:

1. waits for the registry to answer on `/v2/`
2. `scripts/push-to-zot.py` — packs each `examples/oci-suites/<name>` and
   `examples/oci-modules/<name>` into an OCI artifact and uploads it
3. `scripts/install-plugins.py` — reads each `examples/oci-plugins/<name>/plugin.yaml`,
   inlines its Lua, CUE schema and Starlark wrapper, and writes them into the
   `plugins:` list of `configuration.yaml`

**The backend reads the catalog at startup.** Editing anything under
`examples/` has no effect on a run until you push it and restart the backend:

```bash
python demo/bootstrap.py                      # or: --skip-plugins
python scripts/push-to-zot.py --suite payment-suite    # just one
```

`GET /api/v1/suites/<id>` lists the package's `sourceFiles`, which is the
quickest way to confirm an edit actually reached the backend. A changed plugin
also needs its sidecar removed (`docker rm -f babel-sidecar-<suite>-<profile>`)
so APISIX reloads the Lua.

## Try it

| Suite | Why it is a good first run |
|---|---|
`gateway-check` | Smallest complete package — one mock, one test step. Finishes in seconds. |
`plugin-span-demo` | Plugin span attributes, backed entirely by CUE mocks. Starts no containers. |
`electronics-lab` | Three Lua plugins against mock surfaces — SPICE, Verilog, control systems. |
`payment-suite` | A realistic shape: Postgres, migrations, a mock, traffic phases, tests. |
`composite-readiness` | Composes four other suites. ~70 nodes. |

With the telemetry profile on, `docker compose logs -f otel-collector` shows the
`runner.run` span for every step, including the `runner.plugin.*` attributes a
plugin reported:

```bash
docker compose --env-file ../.env --profile telemetry up -d
# then in ../.env:  OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
```

## What will not run

`secrets.provider` is `none`, so a profile whose inheritance chain declares a
Vault `secretRef` fails at boot — Vault needs `VAULT_TOKEN` and says so. That is
`fleet-control-room` (all profiles, via its managed base profile),
`identity-broker/canary.yaml`, and `payment-suite`'s `staging.yaml` and
`year.yaml`. The other 22 suite/profile pairs run without a secret backend.

## Ports

Everything is overridable in the repo-root `.env` — one env file for the whole
project. Compose has to be pointed at it (`--env-file ../.env`), which the
commands above do. Useful when you already have a database or registry running:

| Variable | Default |
|---|---|
`DEMO_MONGO_PORT` | `27017` |
`DEMO_REDIS_PORT` | `6379` |
`DEMO_ZOT_PORT` | `5000` |

Leave `DEMO_ZOT_PORT` at `5000`. Every `examples/oci-suites/*/metadata.yaml`
pins its repository as `localhost:5000/<path>` and the `dependencies.yaml` refs
do too, so another port leaves the catalog pointing nowhere.

## Teardown

```bash
cd demo
docker compose --env-file ../.env down      # keep the registry and database contents
docker compose --env-file ../.env down -v   # discard them too
```

Suite runs leave their own containers and networks behind if a run is
interrupted. Those are labelled, and the Environments page (`/environments`)
reaps them. By hand:

```bash
docker rm -f $(docker ps -aq --filter 'name=babel-run')
docker network ls --format '{{.Name}}' | grep '^babel-net-run-' | xargs -r docker network rm
```
