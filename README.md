# BabelSuite

**Open-source orchestrator for integration test suites.**
Declare your entire test environment in one file. Run it anywhere.

<p align="center">
  <a href="#quick-start">Quick Start</a>
  &nbsp;•&nbsp;
  <a href="https://jarin-devoss.github.io/babelSuite">Docs</a>
  &nbsp;•&nbsp;
  <a href="examples/">Examples</a>
</p>

<p align="center">
  <a href="https://github.com/jarin-devoss/babelSuite/actions/workflows/backend-ci.yml">
    <img src="https://github.com/jarin-devoss/babelSuite/actions/workflows/backend-ci.yml/badge.svg?branch=main" alt="Backend CI" />
  </a>
  <a href="https://github.com/jarin-devoss/babelSuite/actions/workflows/e2e.yml">
    <img src="https://github.com/jarin-devoss/babelSuite/actions/workflows/e2e.yml/badge.svg?branch=main" alt="E2E CI" />
  </a>
  <a href="https://github.com/jarin-devoss/babelSuite/blob/main/LICENSE">
    <img src="https://img.shields.io/badge/License-Apache%202.0-blue.svg" alt="License: Apache-2.0" />
  </a>
</p>

---

## What is BabelSuite?

Setting up a realistic integration environment usually means juggling Docker Compose files, bash scripts, custom seeders, and CI jobs — each in a different format, with no shared model and no live visibility into what's happening.

BabelSuite replaces that with a single declarative **suite file**. You describe your stack — services, databases, mocks, migrations, tests, and traffic — and BabelSuite resolves the dependency graph, launches everything in the right order, and streams logs and status in real time to the UI and API.

One file. One command. Any environment.

---

## Suite Example

A `suite.star` file for a payment service with Postgres, Kafka, a Stripe mock, fraud worker, traffic, and smoke tests:

```python
load("@babelsuite/runtime", "service", "task", "test", "traffic")

db               = service.run()
kafka            = service.run()
stripe_mock      = service.mock(after=[db])
bootstrap_topics = task.run(file="bootstrap_topics.sh", image="bash:5.2",   after=[kafka])
migrations       = task.run(file="migrate.py",           image="python:3.12", after=[db])
payment_gateway  = service.run(after=[db, stripe_mock, migrations])
fraud_worker     = service.run(after=[kafka, bootstrap_topics, payment_gateway])

checkout_baseline = traffic.baseline(
    plan="checkout_baseline.star",
    target="http://payment_gateway:8080",
    after=[payment_gateway, fraud_worker],
)

checkout_smoke = test.run(
    file="checkout_smoke.py",
    image="python:3.12",
    after=[checkout_baseline],
    exports=[
        {"path": "reports/junit.xml", "name": "checkout-test-report", "on": "always", "format": "junit"},
    ],
)
```

BabelSuite reads the file, builds the dependency graph, and executes each step in topological order — parallelizing independent branches automatically:

```
db ──────┬── migrations ──┐
         └── stripe_mock ─┤
                           payment_gateway ──┐
kafka ───┬── bootstrap ───┐                  │
         └───────────────── fraud_worker ────┤
                                              checkout_baseline
                                                    │
                                              checkout_smoke
```

---

## Key Features

**Starlark Topology**
Write your environment in Starlark — expressive, Python-like, version-controlled. `service`, `task`, `test`, `traffic`, and `suite` are first-class primitives, not YAML keys.

**DAG Execution**
BabelSuite builds a dependency graph from your `after=[]` declarations and executes steps in topological order, parallelizing independent branches.

**Profile System**
Switch between environment configurations at launch time without editing the suite file. Define `local`, `ci`, `staging`, and `production` profiles — each overlaying different env vars, secrets, and module settings.

**Stateful Mocks**
Define mock endpoints with per-operation schemas, request/response examples, and state machine transitions. Supports Wiremock, Prism, and custom adapters.

**OCI Module Packages**
Reuse topology building blocks — Kafka, Postgres, Redis — packaged as OCI artifacts and loaded with a single `load()` statement.

**Multi-Backend**
Run against local Docker, Kubernetes, or distributed remote agent pools. No code changes between environments — switch with a profile.

**Live Streaming**
Every log line and status change is pushed to the UI and REST API clients in real time over Server-Sent Events.

**OpenTelemetry Built-in**
Traces and metrics exported via OTLP from both the control plane and the frontend. Plug into Grafana, Jaeger, or Honeycomb.

---

## Quick Start

**Prerequisites:** Go 1.22+, Node.js 20+, Docker, MongoDB or PostgreSQL

```bash
# Clone the repo
git clone https://github.com/jarin-devoss/babelSuite.git
cd babelSuite

# Start the control plane (port 8090)
cd backend && go run ./cmd/server

# In another terminal, start the UI (port 5173)
cd frontend && npm install && npm run dev
```

Open [http://localhost:5173](http://localhost:5173) and sign in:

| Field    | Value                   |
|----------|-------------------------|
| Email    | `admin@babelsuite.test` |
| Password | `admin`                 |

Populate the catalog with the bundled examples:

```bash
cd backend && go run ./cmd/seed-zot
```

See the [Getting Started guide](https://jarin-devoss.github.io/babelSuite/getting-started) for the full walkthrough, including environment variables and database setup.

---

## Example Suites

Seven runnable examples are included under [`examples/oci-suites/`](examples/oci-suites/):

| Suite | What it exercises |
|---|---|
| [`payment-suite`](examples/oci-suites/payment-suite/) | Postgres, Kafka, Stripe mock, fraud worker, checkout traffic, smoke tests |
| [`identity-broker`](examples/oci-suites/identity-broker/) | OIDC mock, SAML mock, realm seeding, session worker, login flows |
| [`storefront-browser-lab`](examples/oci-suites/storefront-browser-lab/) | Kafka event streams, Playwright browser journeys, promo traffic |
| [`returns-control-plane`](examples/oci-suites/returns-control-plane/) | Refund state machines, pricing service, event-schema validation |
| [`soap-claims-hub`](examples/oci-suites/soap-claims-hub/) | Legacy SOAP/XML service integration |
| [`fleet-control-room`](examples/oci-suites/fleet-control-room/) | Fleet management with traffic phases and telemetry |
| [`composite-readiness`](examples/oci-suites/composite-readiness/) | Nested suite composition across multiple registries |

Each example includes a `suite.star` topology, environment profiles, OpenAPI contracts, mock schemas, seed fixtures, and test scripts.

---

## Architecture

```
Browser UI  (React + TypeScript, :5173)
      │  REST + SSE
Control Plane  (Go, :8090)
  ├── Suite Service        — loads & resolves suite.star topology
  ├── Execution Service    — DAG orchestration, live event streaming
  ├── Catalog Service      — OCI registry discovery & package browsing
  ├── Profile Service      — launch-time config overlays
  ├── Platform Settings    — agents, registries, secrets
  └── Agent Coordinator    — heartbeat, work distribution
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
  internal/         All internal packages
frontend/           React 19 + TypeScript UI
examples/
  oci-suites/       Runnable suite examples
  oci-modules/      Reusable Starlark modules (Kafka, Postgres)
proto/              Protocol Buffer definitions (buf)
docs/               MkDocs documentation site
Dockerfile          Multi-stage build — frontend + backend → Alpine image
```

---

## Documentation

Full documentation: **[jarin-devoss.github.io/babelSuite](https://jarin-devoss.github.io/babelSuite)**

| Section | Description |
|---|---|
| [Getting Started](https://jarin-devoss.github.io/babelSuite/getting-started) | Prerequisites, local dev flow, first run |
| [Suite Authoring](https://jarin-devoss.github.io/babelSuite/suite-authoring) | Package layout, topology primitives, naming |
| [Modules](https://jarin-devoss.github.io/babelSuite/modules) | OCI module packages, Kafka, Postgres |
| [Runtime Library](https://jarin-devoss.github.io/babelSuite/runtime-library) | `service`, `task`, `test`, `traffic` reference |
| [Mocking](https://jarin-devoss.github.io/babelSuite/mocking) | Mock endpoints, state machines, fallback modes |
| [Profiles](https://jarin-devoss.github.io/babelSuite/profiles) | Environment overlays, secrets, launch-time config |
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
