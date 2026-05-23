---
title: BabelSuite
---

# BabelSuite

Your tests should run the same way on every machine, every team, every environment — without a README full of setup steps, without a shared staging environment nobody trusts, and without one engineer who "knows how to run the tests locally."

BabelSuite gives every team a `suite.star` file: a short, readable description of the full environment your tests need — databases, services, mocks, migrations — and the tests themselves. No Docker Compose knowledge required. No custom shell scripts. Just declare what depends on what, and BabelSuite handles the rest.

```python
load("@babelsuite/runtime",  "service", "task", "test")
load("@babelsuite/postgres", "pg")

REGIONS = env.get("REGIONS", "us,eu,ap").split(",")

db          = pg()
migrate     = task.run(file="migrate.py", image="python:3.12", after=[db])
stripe_mock = service.mock(after=[db])
api         = service.run(after=[db, migrate, stripe_mock])

# run smoke tests for every region in parallel
for region in REGIONS:
    test.run(
        name="smoke-" + region,
        file="smoke.py",
        image="python:3.12",
        after=[api],
        env={"REGION": region},
    )
```

The suite is the contract. Push it to your OCI registry and any team can pull it, run it against their own backend, and get identical results — on a laptop, in CI, or on Kubernetes. No environment-specific scripts. No "works on my machine."

When a step fails, logs are live in the UI immediately — not buried in CI output after a ten-minute wait.

## Core Concepts

| Concept | Description |
|---------|-------------|
| **Suite** | A runnable package containing `suite.star`, launch profiles, OCI dependencies, service configs, mocks, tests, and traffic plans. |
| **Profile** | A launch-time overlay that injects env vars, secrets, and module settings without editing the suite itself. |
| **Execution** | A single run of a suite under a chosen profile and backend. Every execution streams live events and logs. |
| **Backend** | Where steps run — `local` Docker, `kubernetes`, or a `remote-agent` worker pool. |
| **Catalog** | The control plane's view of OCI registry contents — suite packages and reusable modules. |
| **Environment** | The live inventory of containers, networks, and volumes produced by running executions. |
| **Agent** | A remote worker process that registers with the control plane, claims step work, streams logs, and completes jobs. |
| **Cron Job** | A scheduled rule that runs one or more suites on a cron expression and delivers a results report via email and Slack. |
| **Dependency Manifest** | `dependencies.yaml` — maps nested suite aliases to pinned OCI refs, profiles, and input overrides. |

## Documentation Map

### Start Here

- [Getting Started](getting-started.md) — prerequisites, local dev flow, first things to try

### System

- [Architecture](architecture.md) — system layers, control plane composition, data flows, storage model
- [Control Plane](control-plane.md) — middleware, request IDs, tracing, audit, health internals
- [Configuration](configuration.md) — all environment variables, `configuration.yaml` fields, local defaults
- [Platform Settings](platform.md) — agents, registries, secrets, notifications

### Suites and Authoring

- [Suites](suites.md) — suite structure, topology families, nested suites, dependency rules
- [Suite Authoring](suite-authoring.md) — package layout, recognized folders, naming conventions
- [Dependency Manifests](dependencies.md) — `dependencies.yaml` and `dependencies.lock.yaml` format
- [Runtime Library](runtime-library.md) — built-in Starlark surface: `service`, `task`, `test`, `traffic`, `security`, `suite`
- [Modules](modules.md) — OCI module packages (Kafka, Postgres, Redis, MongoDB, Playwright)

### Profiles and Mocking

- [Profiles](profiles.md) — profile sources, shape, API records, default selection
- [Profile Runtime](profile-runtime.md) — workspace vs managed profiles, runtime overlays, dependency profile flow
- [Mocking](mocking.md) — mock endpoints, operation metadata, fallback modes, stateful mocking
- [Mocking Reference](mocking-reference.md) — complete field reference for surfaces, operations, state, and exchanges

### Execution and Infrastructure

- [Execution](execution.md) — launch model, backends, step spec, live streams, remote agents
- [Agents](agents.md) — worker lifecycle, control plane endpoints, worker process endpoints, payloads
- [Environments](environments.md) — runtime inventory model, SSE updates, cleanup operations
- [Catalog](catalog.md) — OCI discovery, package fields, favorites
- [Cron Jobs](cron-jobs.md) — scheduled suite execution, multi-suite targets, email and Slack notifications

### Interfaces, Examples, and Operations

- [Authentication](auth.md) — local auth, OIDC SSO, JWT session model
- [API](api.md) — full HTTP API route reference
- [CLI](cli.md) — `babelctl` commands and usage
- [Examples](examples.md) — example suite packages and local registry setup
- [Development](development.md) — local dev commands, tests, seed, sync
- [Operations](operations.md) — health probes, telemetry, cache, datastores

## Product Surface

| Route | Purpose |
|-------|---------|
| `/` | Home dashboard — execution overview and active runs |
| `/catalog` | Registry-backed package discovery |
| `/suites` | Runnable suite explorer |
| `/profiles` | Suite profile management |
| `/executions/:executionId` | Live execution detail with event and log streams |
| `/environments` | Runtime inventory — containers, networks, volumes |
| `/cron-jobs` | Scheduled suite execution and notification rules |
| `/settings/*` | Platform configuration — agents, registries, secrets, notifications (admin only) |
| `/sign-in`, `/sign-up`, `/auth/callback` | Authentication |

## Repository Layout

```
backend/           Go control plane, remote worker, CLI, and all internal services
frontend/          React 19 + TypeScript UI (Vite)
examples/
  oci-suites/      Seven runnable example suite packages
  oci-modules/     Reusable Starlark modules (Kafka, Postgres, Redis, MongoDB)
proto/             API service definitions (protobuf)
demo/              Demo-mode data files
tools/             Local helper scripts and configuration
docs/              This documentation (MkDocs Material)
```

## Running the Docs Locally

```bash
pip install -r docs/requirements.txt
mkdocs serve
```

The local site is available at `http://127.0.0.1:8000/`.

## Publishing

The repository includes a documentation deployment workflow at `.github/workflows/docs.yml`.

To enable GitHub Pages:

1. Open repository **Settings → Pages**.
2. Set the source to the `gh-pages` branch, root folder.
3. Save.

Pushes to `main` that change `docs/**` or `mkdocs.yml` will then auto-deploy the site.
