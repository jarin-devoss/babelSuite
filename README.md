# BabelSuite

**A low-code standard for integration tests. One `suite.star` file per service. Every team runs the same environment, anywhere.**

<p align="center">
  <a href="#quick-start">Quick Start</a>
  &nbsp;•&nbsp;
  <a href="https://jarin-devoss.github.io/babelSuite">Documentation</a>
  &nbsp;•&nbsp;
  <a href="examples/">Examples</a>
  &nbsp;•&nbsp;
  <a href="https://jarin-devoss.github.io/babelSuite/api">API Reference</a>
</p>

<p align="center">
  <a href="https://github.com/jarin-devoss/babelSuite/actions/workflows/backend-ci.yml">
    <img src="https://github.com/jarin-devoss/babelSuite/actions/workflows/backend-ci.yml/badge.svg?branch=main" alt="Backend CI" />
  </a>
  <a href="https://github.com/jarin-devoss/babelSuite/actions/workflows/frontend-ci.yml">
    <img src="https://github.com/jarin-devoss/babelSuite/actions/workflows/frontend-ci.yml/badge.svg?branch=main" alt="Frontend CI" />
  </a>
  <a href="https://github.com/jarin-devoss/babelSuite/actions/workflows/e2e.yml">
    <img src="https://github.com/jarin-devoss/babelSuite/actions/workflows/e2e.yml/badge.svg?branch=main" alt="E2E" />
  </a>
  <a href="https://github.com/jarin-devoss/babelSuite/blob/main/LICENSE">
    <img src="https://img.shields.io/badge/License-Apache%202.0-blue.svg" alt="License: Apache-2.0" />
  </a>
</p>

---

## What It Does

Most teams have a staging environment nobody trusts, a README full of setup steps, and one engineer who knows how to run the tests locally. BabelSuite replaces all of that with a `suite.star` file.

You write the full environment your tests need — database, services, mocks, migrations — alongside the tests themselves. No Docker Compose expertise required. No custom shell scripts. Just declare what depends on what:

```python
load("@babelsuite/runtime",  "service", "task", "test")
load("@babelsuite/postgres", "pg")

db          = pg()                                                              # start Postgres
migrate     = task.run(file="migrate.py", image="python:3.12", after=[db])    # run migrations
stripe_mock = service.mock(after=[db])                                         # start a Stripe mock
api         = service.run(after=[db, migrate, stripe_mock])                    # start your service
smoke       = test.run(file="smoke.py",   image="python:3.12", after=[api])   # run tests
```

Push the suite to your OCI registry and any team can pull it, run it against their own backend, and get identical results — on a laptop, in CI, or on Kubernetes. The suite is the shared standard.

BabelSuite starts each step only when its dependencies are ready — parallel where it can, sequential where it must. Every log line and status change streams live to the UI:

```
✓ db            started  (1.2s)
✓ stripe_mock   started  (0.8s)
✓ migrate       passed   (3.1s)
✓ api           started  (2.4s)
✓ smoke         passed   (4.7s)  — 14 tests passed
```

Switch from Docker to Kubernetes or a remote agent by selecting a different backend at launch. Nothing in the file changes.

## Why BabelSuite

Teams running integration tests without BabelSuite usually have a shell script and a Docker Compose file that works on one machine, breaks in CI, and takes 20 minutes to debug when it does.

| | Shell scripts + Compose | BabelSuite |
|---|---|---|
| Start order | Manual, fragile | Automatic from `after=[]` |
| Parallel execution | Not built in | Independent branches run concurrently |
| Live visibility | Tail logs, guess state | Real-time step status and logs in the UI |
| Same setup for local + CI + K8s | Different scripts per target | One file, swap backends with a profile |
| Service mocks | Separate mock servers, manual wiring | Built-in, with state machines and request routing |
| Reusable infrastructure | Copy-paste Compose snippets | OCI modules — load Kafka or Postgres with one line |
| Scheduled regression runs | Cron shell scripts | Built-in cron jobs with email and Slack reports |

**BabelSuite is the right fit if you:**

- Need a reproducible full-stack environment — database, dependencies, mocks, migrations, and tests — that runs the same way everywhere
- Want to see exactly what is happening while the environment starts up, not after the fact
- Run the same suite against multiple targets (local Docker, a shared Kubernetes cluster, or a remote agent)
- Want to schedule nightly or pre-release regression runs and get a results report without writing CI jobs for it
- Are tired of debugging flaky CI environments that nobody can reproduce locally

---

## What It Looks Like

A `suite.star` file for a payment service with Postgres, Kafka, a Stripe mock, fraud detection, traffic baselines, and smoke tests:

```python
load("@babelsuite/runtime", "service", "task", "test", "traffic")
load("@babelsuite/postgres",  "pg")
load("@babelsuite/kafka",     "kafka", "create_topic")

db               = pg()
broker           = kafka()
orders_topic     = create_topic("orders", partitions=3, after=[broker])
migrate          = task.run(file="migrate.py",      image="python:3.12", after=[db])
stripe_mock      = service.mock(after=[db])
payment_gateway  = service.run(after=[db, migrate, stripe_mock])
fraud_worker     = service.run(after=[broker, orders_topic, payment_gateway])

checkout_traffic = traffic.baseline(
    plan="checkout_baseline.star",
    target="http://payment_gateway:8080",
    after=[payment_gateway, fraud_worker],
)
checkout_smoke   = test.run(
    file="checkout_smoke.py",
    image="python:3.12",
    after=[checkout_traffic],
    exports=[{"path": "reports/junit.xml", "name": "test-report", "format": "junit"}],
)
```

BabelSuite reads the file, builds the dependency graph, and executes each step in topological order — running independent branches in parallel:

```
db ────────┬── migrate ────┐
           └── stripe_mock ┤
                            payment_gateway ──┐
broker ────┬── orders_topic ┐                 │
           └────────────────┴── fraud_worker ─┤
                                               checkout_traffic
                                                     │
                                               checkout_smoke
```

---

## Why BabelSuite

| | Shell scripts + Compose | BabelSuite |
|---|---|---|
| **Dependency ordering** | Manual, error-prone | Automatic from `after=[]` declarations |
| **Parallel execution** | Not built in | Independent branches run concurrently |
| **Live visibility** | Tail logs, guess state | Real-time event and log streams in the UI |
| **Environment parity** | Different scripts per target | One suite, swap backends with a profile |
| **Mocking** | Separate mock servers, manual wiring | First-class service mocks with state machines |
| **OCI reuse** | Copy-paste | Load Kafka, Postgres, Redis with one line |
| **Scheduling** | Cron shell scripts | Built-in cron jobs with suite execution and report delivery |
| **Traceability** | None | Full OpenTelemetry traces and audit logs |

---

## Features

### Starlark Topology
Write your environment in a Python-like syntax that is readable, version-controlled, and executable. `service`, `task`, `test`, `traffic`, `security`, and `suite` are first-class primitives — not YAML keys wrapped in string templates.

### Automatic Dependency Resolution
Declare what depends on what with `after=[]`. BabelSuite builds a DAG, executes in topological order, and parallelizes independent branches without any additional configuration.

### Multi-Backend Execution
The same suite runs on local Docker, Kubernetes, or distributed remote agent pools. Switch environments by selecting a different backend at launch time — no changes to the suite file.

### Profile System
Define `local`, `ci`, `staging`, and `production` profiles that overlay different env vars, secrets, module configurations, and resource limits. Launch the right configuration for the right environment without branching your topology.

### OCI Module Packages
Reuse infrastructure building blocks — Kafka, Postgres, Redis, MongoDB, Playwright — packaged as OCI artifacts and loaded with a single `load()` statement. Publish your own modules to any compatible OCI registry.

### Stateful Mocks
Define service mocks with per-operation request/response examples, routing rules, and state machine transitions. Reset mock state between test steps, inject latency, and specify fallback behavior — all in metadata, not code.

### Live Streaming
Every log line and status change is pushed over Server-Sent Events to the UI and any API client in real time. No polling, no log tailing — open the execution page and watch it happen.

### Scheduled Suite Runs
Create cron jobs that run one or more suites on a schedule and automatically send a results report via email and Slack. Each suite target can be pointed at a different backend and profile. Configuration is live — update SMTP settings in the UI and they apply on the next run without restarting the server.

### Security Testing
Run OWASP probes, fuzzing, header inspection, CORS validation, and authentication boundary tests as first-class suite steps alongside your functional tests. Fail builds on threshold violations.

### OpenTelemetry Built-In
Traces and metrics export from both the control plane and the frontend via OTLP. Plug into Grafana, Honeycomb, Jaeger, or any compatible collector.

---

## Quick Start

**Prerequisites:** Go 1.22+, Node.js 20+, Docker, MongoDB

```bash
# 1. Clone
git clone https://github.com/jarin-devoss/babelSuite.git
cd babelSuite

# 2. Start the control plane (port 8090)
cd backend && go run ./cmd/server

# 3. Start the UI (port 5173) — open a new terminal
cd frontend && npm install && npm run dev
```

Open [http://localhost:5173](http://localhost:5173) and sign in:

| Field    | Value                    |
|----------|--------------------------|
| Email    | `admin@babelsuite.test`  |
| Password | `admin`                  |

Populate the catalog with the bundled example suites:

```bash
cd backend && go run ./cmd/seed-zot
```

Then open `/catalog`, pick a suite, select a profile, and launch your first run.

See the [Getting Started guide](https://jarin-devoss.github.io/babelSuite/getting-started) for full environment variable setup, PostgreSQL, Redis, remote agents, and SSO.

---

## Example Suites

Seven production-realistic examples are included under [`examples/oci-suites/`](examples/oci-suites/). Each ships with a `suite.star` topology, environment profiles, OpenAPI contracts, mock schemas, seed fixtures, and test scripts.

| Suite | What it exercises |
|-------|-------------------|
| [`payment-suite`](examples/oci-suites/payment-suite/) | Postgres, Kafka, Stripe mock, fraud worker, checkout traffic, smoke tests |
| [`identity-broker`](examples/oci-suites/identity-broker/) | OIDC mock, SAML mock, realm seeding, session worker, login flows |
| [`storefront-browser-lab`](examples/oci-suites/storefront-browser-lab/) | Kafka event streams, Playwright browser journeys, promo traffic |
| [`returns-control-plane`](examples/oci-suites/returns-control-plane/) | Refund state machines, pricing service, event-schema validation |
| [`soap-claims-hub`](examples/oci-suites/soap-claims-hub/) | Legacy SOAP/XML service integration |
| [`fleet-control-room`](examples/oci-suites/fleet-control-room/) | Fleet management with traffic phases and telemetry |
| [`composite-readiness`](examples/oci-suites/composite-readiness/) | Nested suite composition across multiple registries |

---

## Architecture

```
Browser UI  (React + TypeScript, :5173)
      │  REST + SSE
      ▼
Control Plane  (Go, :8090)
  ├── Auth          — local password + OIDC SSO, JWT sessions
  ├── Suite Service — loads suite.star, resolves topology DAG
  ├── Execution     — orchestrates steps, streams events and logs
  ├── Catalog       — OCI registry discovery and package browsing
  ├── Profiles      — launch-time environment overlays
  ├── Cron Jobs     — scheduled execution with email and Slack reports
  ├── Platform      — agents, registries, secrets, notifications
  └── Agents        — heartbeat, work distribution, log collection
            │
     Execution Backends
     ├── Local Docker
     ├── Kubernetes
     └── Remote Agents  (:8091)
            │
       Storage
       ├── MongoDB / PostgreSQL  (primary)
       └── Redis                 (optional cache)
```

---

## Repository Layout

```
backend/
  cmd/server/       Control plane API (:8090)
  cmd/agent/        Remote worker process (:8091)
  cmd/ctl/          babelctl CLI
  internal/         All packages — execution, auth, catalog, profiles, cronjobs…
frontend/           React 19 + TypeScript UI (Vite)
examples/
  oci-suites/       Seven runnable example suite packages
  oci-modules/      Reusable Starlark modules (Kafka, Postgres, Redis, MongoDB)
proto/              Protocol Buffer definitions
docs/               Full documentation (MkDocs Material)
Dockerfile          Multi-stage build — frontend + backend → Alpine
```

---

## Documentation

Full documentation: **[jarin-devoss.github.io/babelSuite](https://jarin-devoss.github.io/babelSuite)**

| Guide | What it covers |
|-------|----------------|
| [Getting Started](https://jarin-devoss.github.io/babelSuite/getting-started) | Local setup, environment variables, first run |
| [Suite Authoring](https://jarin-devoss.github.io/babelSuite/suite-authoring) | Package layout, topology primitives, naming |
| [Runtime Library](https://jarin-devoss.github.io/babelSuite/runtime-library) | `service`, `task`, `test`, `traffic`, `security` reference |
| [Modules](https://jarin-devoss.github.io/babelSuite/modules) | Built-in OCI module packages |
| [Profiles](https://jarin-devoss.github.io/babelSuite/profiles) | Environment overlays, secrets, launch-time config |
| [Mocking](https://jarin-devoss.github.io/babelSuite/mocking) | Mock endpoints, state machines, fallback modes |
| [Cron Jobs](https://jarin-devoss.github.io/babelSuite/cron-jobs) | Scheduled runs, email and Slack reports |
| [Configuration](https://jarin-devoss.github.io/babelSuite/configuration) | All environment variables and `configuration.yaml` fields |
| [API Reference](https://jarin-devoss.github.io/babelSuite/api) | Full HTTP API route reference |
| [Operations](https://jarin-devoss.github.io/babelSuite/operations) | Health probes, telemetry, datastores |

Run the docs locally:

```bash
pip install -r docs/requirements.txt
mkdocs serve
```

---

## License

BabelSuite is licensed under the [Apache License 2.0](LICENSE).
