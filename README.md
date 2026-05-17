<p align="center">
  <img src="docs/assets/logo.png" alt="BabelSuite" width="220" />
</p>

<p align="center">
  <b>Open-source orchestrator for integration test suites</b><br/>
  Declare your entire test environment in one file. Run it anywhere.
</p>

<p align="center">
  <a href="#quick-start">Quick Start</a>
  &nbsp;•&nbsp;
  <a href="https://babelsuite.github.io/babelsuite">Docs</a>
  &nbsp;•&nbsp;
  <a href="examples/">Examples</a>
  &nbsp;•&nbsp;
  <a href="https://github.com/babelsuite/babelsuite/discussions">Discussions</a>
</p>

<p align="center">
  <a href="https://github.com/babelsuite/babelsuite/actions/workflows/backend-ci.yml">
    <img src="https://github.com/babelsuite/babelsuite/actions/workflows/backend-ci.yml/badge.svg?branch=main" alt="Backend CI" />
  </a>
  <a href="https://github.com/babelsuite/babelsuite/actions/workflows/e2e.yml">
    <img src="https://github.com/babelsuite/babelsuite/actions/workflows/e2e.yml/badge.svg?branch=main" alt="E2E CI" />
  </a>
  <a href="https://goreportcard.com/report/github.com/babelsuite/babelsuite">
    <img src="https://goreportcard.com/badge/github.com/babelsuite/babelsuite" alt="Go Report Card" />
  </a>
  <a href="https://github.com/babelsuite/babelsuite/blob/main/LICENSE">
    <img src="https://img.shields.io/badge/License-Apache%202.0-blue.svg" alt="License: Apache-2.0" />
  </a>
  <a href="https://github.com/babelsuite/babelsuite/issues?q=is%3Aopen+label%3A%22good+first+issue%22">
    <img src="https://img.shields.io/github/issues-search/babelsuite/babelsuite?query=is%3Aopen+label%3A%22good+first+issue%22&label=good+first+issues&color=7057ff" alt="Good First Issues" />
  </a>
</p>

---

## What is BabelSuite?

BabelSuite is an open-source control plane for running complex integration test environments. You define your entire stack — services, databases, mocks, migrations, tests, and traffic — in a single **Starlark** file (`suite.star`). BabelSuite resolves the dependency graph and executes everything in the right order, with live log streaming and full observability built in.

It is the missing link between your microservices and your CI pipeline: one file to describe what your integration environment looks like, one command to run it.

## Why BabelSuite?

Setting up a realistic integration environment typically means maintaining a tangle of Docker Compose files, bash scripts, custom seeders, and CI jobs — each described in a different format, with no shared model and no live visibility.

BabelSuite replaces that with a single declarative topology that:

1. **Resolves dependencies automatically** — steps launch in the correct order and parallelize where they can
2. **Runs anywhere** — local Docker, Kubernetes, or distributed remote agent pools
3. **Streams everything live** — every log line and status change is pushed to the UI in real time
4. **Mocks with state** — built-in stateful mock servers with operation schemas and state machine transitions
5. **Composes across registries** — pull suite packages from any OCI-compatible registry and nest them as dependencies

## Key Features

**Starlark Suite Definitions**
Write your environment topology in Starlark — expressive, Python-like, version-controlled. `service`, `task`, `test`, `traffic`, and `suite` are first-class primitives, not YAML keys.

**DAG-Based Execution**
BabelSuite builds a dependency graph from your `after=[]` declarations and executes steps in topological order, parallelizing independent branches automatically.

**Multi-Backend Support**
- Local Docker — zero-config default for development
- Kubernetes — production-grade cluster execution
- Remote Agents — distributed worker pools for large-scale or isolated runs

**Live Event Streaming**
Step status, log lines, and execution state changes are pushed to the browser UI and REST API clients in real time over Server-Sent Events.

**Stateful Service Mocking**
Define mock endpoints with per-operation schemas, request/response examples, fallback modes, and state machine transitions. Supports Wiremock, Prism, and service.mock adapters.

**OCI Registry Integration**
Browse, pull, and compose suite packages from any OCI-compliant registry — Zot, GHCR, ECR, Docker Hub. Pin by tag or content digest.

**Profile System**
Switch between environment configurations at launch time without editing the suite file. Define profiles for `local`, `ci`, `staging`, `canary`, and `production` — each overlaying different env vars, module settings, and observability targets.

**OpenTelemetry Built-in**
Traces and metrics are exported via OTLP from both the control plane and frontend out of the box. Plug into your existing Grafana, Jaeger, or Honeycomb stack.

**Authentication & Access Control**
Local email/password auth for development. OIDC SSO for production. JWT-based sessions throughout.

---

## Quick Start

**Prerequisites:** Go 1.22+, Node.js 20+, Docker, MongoDB or PostgreSQL

```bash
# Clone the repo
git clone https://github.com/babelsuite/babelsuite.git
cd babelsuite

# Start the control plane (port 8090)
cd backend && go run ./cmd/server

# In another terminal, start the UI (port 5173)
cd frontend && npm install && npm run dev
```

Open [http://localhost:5173](http://localhost:5173) and sign in with:

| Field    | Value                    |
|----------|--------------------------|
| Email    | `admin@babelsuite.test`  |
| Password | `admin`                  |

Populate the catalog with the bundled example suites:

```bash
cd backend && go run ./cmd/seed-zot
```

See the [Getting Started guide](https://babelsuite.github.io/babelsuite/getting-started) for the full walkthrough, including environment variables and database setup.

---

## How It Works

A suite is a `suite.star` file that declares a dependency graph of steps:

```python
load("@babelsuite/runtime", "service", "task", "test", "traffic")

db      = service.run()
kafka   = service.run()
stripe  = service.mock(after=[db])
migrate = task.run(file="migrate.py", image="python:3.12", after=[db])
gateway = service.run(after=[db, stripe, migrate])
worker  = service.run(after=[kafka, gateway])
traffic.baseline(plan="checkout.star", target="http://gateway:8080", after=[gateway, worker])
test.run(file="smoke.py", image="python:3.12", after=[worker])
```

BabelSuite reads the file, resolves the graph, launches each step against the configured backend, and streams logs and status changes to the UI and API in real time.

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

Each example includes a `suite.star` topology, environment profiles (`local`, `ci`, `staging`), OpenAPI contracts, mock schemas, seed fixtures, and runnable test scripts.

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
tools/              Local helper scripts (Zot registry, seeding)
Dockerfile          Multi-stage build — frontend + backend → Alpine image
```

---

## Documentation

Full documentation: **[babelsuite.github.io/babelsuite](https://babelsuite.github.io/babelsuite)**

| Section | Description |
|---|---|
| [Getting Started](https://babelsuite.github.io/babelsuite/getting-started) | Prerequisites, local dev flow, first run |
| [Suite Authoring](https://babelsuite.github.io/babelsuite/suite-authoring) | Package layout, topology primitives, naming |
| [Runtime Library](https://babelsuite.github.io/babelsuite/runtime-library) | `service`, `task`, `test`, `traffic` reference |
| [Mocking](https://babelsuite.github.io/babelsuite/mocking) | Mock endpoints, state machines, fallback modes |
| [Profiles](https://babelsuite.github.io/babelsuite/profiles) | Environment overlays, secrets, launch-time config |
| [Agents](https://babelsuite.github.io/babelsuite/agents) | Remote worker lifecycle and coordination |
| [API Reference](https://babelsuite.github.io/babelsuite/api) | Full HTTP API route reference |
| [Operations](https://babelsuite.github.io/babelsuite/operations) | Health probes, telemetry, datastores |

Run the docs locally:

```bash
pip install -r docs/requirements.txt
mkdocs serve
```

---

## License

BabelSuite is licensed under the [Apache License 2.0](LICENSE).
