---
title: Modules
---

# Modules

Modules are reusable building blocks that your suite imports and calls to set up infrastructure — a Kafka broker, a Postgres cluster, a Redis cache. Think of them like npm packages or Go libraries, but for topology: each module exposes a small set of Starlark helpers that return nodes you can wire into your dependency graph.

There are two distinct things called "modules" in BabelSuite:

| Kind | What it is | Where it lives |
|------|-----------|----------------|
| **Runtime primitives** | Built-in topology nodes (`service`, `task`, `test`, `traffic`, `suite`) | Bundled — always available |
| **OCI module packages** | Reusable Starlark packages published to an OCI registry | `examples/oci-modules/` or any registry |

This page covers OCI module packages. For the built-in runtime primitives, see [Runtime Library](runtime-library.md).

---

## How Modules Work

A module is a Starlark package with a public entrypoint (`module.star`) that exports helper functions. You load it with a `load()` statement at the top of your `suite.star`:

```python
load("@babelsuite/kafka",    "kafka", "create_topic")
load("@babelsuite/postgres", "pg",    "connect")
```

The workspace loader resolves the `@babelsuite/kafka` path against your configured registries, pulls the OCI artifact, and makes the exported symbols available in your suite file. The helpers return topology nodes — the same kind that `service.run()` or `task.run()` return — so you wire them into your graph with `after=[]` like anything else.

---

## Available Modules

| Module path | Package | Status |
|-------------|---------|--------|
| `@babelsuite/runtime` | built-in | Always available — core topology primitives |
| `@babelsuite/kafka` | `examples/oci-modules/kafka` | Available |
| `@babelsuite/postgres` | `examples/oci-modules/postgres` | Available |
| `@babelsuite/redis` | *(not yet published)* | Referenced in examples, no package yet |
| `@babelsuite/playwright` | *(not yet published)* | Referenced in examples, no package yet |

!!! note
    `@babelsuite/redis` and `@babelsuite/playwright` appear in some example suites but have no corresponding OCI package in `examples/oci-modules/` yet. The suite still loads — the `load()` line is recorded as a contract — but no helpers are available.

---

## Kafka Module

**Import path:** `@babelsuite/kafka`  
**Source:** `examples/oci-modules/kafka`

The Kafka module starts a broker cluster as a topology node and exposes admin helpers for managing topics and consumer group offsets. It wraps the infrastructure concerns so your suite file stays focused on what topics it needs, not how to spin up Kafka.

### Package layout

```
kafka/
  module.star     # public entrypoint — re-exports all symbols
  cluster.star    # broker lifecycle node
  admin.star      # topic and group admin helpers
  _shared.star    # internal utilities (not exported)
  usage.star      # runnable examples
  module.yaml     # OCI metadata
  README.md
```

### Exported symbols

| Symbol | Returns | Description |
|--------|---------|-------------|
| `kafka()` | node | Start a Kafka broker cluster |
| `create_topic(name, partitions, after)` | node | Create a topic; waits on `after` |
| `delete_topic(name, after)` | node | Delete a topic |
| `set_group_offset(group, topic, offset, after)` | node | Reposition a consumer group |
| `disconnect(after)` | node | Gracefully shut down the connection |

### Example

```python
load("@babelsuite/runtime", "service", "task")
load("@babelsuite/kafka",   "kafka", "create_topic")

broker         = kafka()
orders_topic   = create_topic("orders",   partitions=3, after=[broker])
payments_topic = create_topic("payments", partitions=1, after=[broker])

worker = service.run(after=[orders_topic, payments_topic])
```

The worker only starts once both topics exist. BabelSuite runs `kafka()`, then the two `create_topic` calls in parallel (they don't depend on each other), then the worker.

---

## Postgres Module

**Import path:** `@babelsuite/postgres`  
**Source:** `examples/oci-modules/postgres`

The Postgres module starts a cluster node and exposes query helpers that run against it. You use it when a suite needs a real database rather than a mock — migrations, seed data, and tests can all call the same helpers without duplicating connection logic.

### Package layout

```
postgres/
  module.star     # public entrypoint
  cluster.star    # cluster lifecycle node
  query.star      # query execution helpers
  _shared.star    # internal utilities (not exported)
  usage.star      # runnable examples
  module.yaml     # OCI metadata
  README.md
```

### Exported symbols

| Symbol | Returns | Description |
|--------|---------|-------------|
| `pg()` | node | Start a Postgres cluster |
| `connect(after)` | node | Open a connection to the cluster |
| `query(sql, after)` | node | Execute a raw SQL statement |
| `insert(table, rows, after)` | node | Insert one or more rows |
| `select(table, where, after)` | node | Select rows matching a condition |
| `delete(table, where, after)` | node | Delete matching rows |
| `upsert(table, rows, conflict, after)` | node | Insert or update on conflict |

### Example

```python
load("@babelsuite/runtime",  "task", "service")
load("@babelsuite/postgres", "pg", "connect", "insert")

db   = pg()
conn = connect(after=[db])

seed = insert(
    table="merchants",
    rows=[{"id": "m1", "name": "Acme"}, {"id": "m2", "name": "Globex"}],
    after=[conn],
)

app = service.run(after=[seed])
```

The app only starts after the seed data is in place. The `pg()` → `connect()` → `insert()` chain runs in strict order; `service.run()` waits on the final insert node.

---

## Module Metadata

Every OCI module package carries a `module.yaml` file that the catalog uses for discovery and display:

```yaml
kind: Module
metadata:
  id: stdlib-kafka
  title: "@babelsuite/kafka"
  description: Kafka broker cluster and admin helpers for BabelSuite suite topologies.
  provider: BabelSuite
  version: 1.2.3
spec:
  repository: localhost:5000/babelsuite/kafka
  entrypoint: module.star
  pullCommand: "babelctl module pull @babelsuite/kafka"
  forkCommand: "babelctl module fork @babelsuite/kafka"
```

The catalog reads these fields to populate the module browser in the UI — title, version, pull command, and description are all surfaced there.

---

## Writing Your Own Module

A minimal module has two files:

**`module.star`** — re-exports your public helpers:

```python
load("cluster.star", _cluster = "cluster")
load("admin.star",   _create  = "create_topic")

kafka        = _cluster
create_topic = _create
```

**`module.yaml`** — OCI metadata the catalog reads:

```yaml
kind: Module
metadata:
  id: my-module
  title: "@myorg/my-module"
  version: 0.1.0
spec:
  repository: ghcr.io/myorg/my-module
  entrypoint: module.star
```

Push the directory as an OCI artifact to any compatible registry (Zot, GHCR, ECR), add the registry to your platform settings, and any suite in the workspace can load it with `load("@myorg/my-module", ...)`.

---

## Layer Summary

| Layer | Purpose |
|-------|---------|
| Runtime library | Built-in topology primitives (`service`, `task`, `test`, `traffic`) |
| OCI module | Reusable Starlark helpers for specific infrastructure (Kafka, Postgres, ...) |
| Suite | Runnable topology — assembles primitives and modules into a full environment |
| Suite dependency | Larger compositions of multiple suites via `suite.run(ref="...")` |

Modules sit between the runtime library and individual suites. They keep infrastructure setup out of your suite file and make it reusable across many suites without copy-paste.
