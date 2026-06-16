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

**Modules vs Plugins:** both are loaded with `load()` but solve different problems. Modules spin up infrastructure your services need while they run (a Kafka broker, a Postgres cluster). Plugins run Lua code inside the APISIX sidecar — this includes the built-in `traffic.*` and `security.*` nodes as well as user-registered plugins for custom checks. Modules can load and re-export plugins internally, bundling infrastructure setup and verification helpers in one package. See [Plugins](plugins.md) for details.

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
| `@babelsuite/kafka` | [`examples/oci-modules/kafka`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-modules/kafka) | Available |
| `@babelsuite/postgres` | [`examples/oci-modules/postgres`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-modules/postgres) | Available |
| `@babelsuite/redis` | [`examples/oci-modules/redis`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-modules/redis) | Available |
| `@babelsuite/mongodb` | [`examples/oci-modules/mongodb`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-modules/mongodb) | Available |
| `@babelsuite/playwright` | [`examples/oci-modules/playwright`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-modules/playwright) | Available |

---

## Kafka Module

**Import path:** `@babelsuite/kafka`  
**Source:** [`examples/oci-modules/kafka`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-modules/kafka)

The Kafka module starts a broker cluster as a topology node and exposes admin helpers for managing topics and consumer group offsets. It wraps the infrastructure concerns so your suite file stays focused on what topics it needs, not how to spin up Kafka.

### Package layout

```
kafka/
  module.star     # public entrypoint — re-exports all symbols
  cluster.star    # broker lifecycle node
  admin.star      # topic and group admin helpers
  usage.star      # runnable examples
  module.yaml     # OCI metadata
  README.md
```

### Exported symbols

| Symbol | Returns | Description |
|--------|---------|-------------|
| `kafka(name, image, port, after, env)` | node | Start a Kafka broker. KRaft mode — no ZooKeeper required. |
| `create_topic(broker, topic, partitions, replication_factor, configs, after)` | node | Create a topic if it doesn't exist. Waits for the broker to be ready before running. |
| `delete_topic(broker, topic, after)` | node | Delete a topic. |
| `set_group_offset(broker, group, topic, offset, partition, after)` | node | Reset a consumer group's offset on a specific partition. |
| `disconnect(broker, after)` | node | Gracefully stop the broker. |

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
**Source:** [`examples/oci-modules/postgres`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-modules/postgres)

The Postgres module starts a cluster node and exposes query helpers that run against it. You use it when a suite needs a real database rather than a mock — migrations, seed data, and tests can all call the same helpers without duplicating connection logic.

### Package layout

```
postgres/
  module.star     # public entrypoint
  cluster.star    # cluster lifecycle node
  query.star      # query execution helpers
  usage.star      # runnable examples
  module.yaml     # OCI metadata
  README.md
```

### Exported symbols

| Symbol | Returns | Description |
|--------|---------|-------------|
| `pg(name, image, database, username, password, port, after, env)` | node | Start a Postgres cluster. |
| `connect(db, after)` | node | Probe the cluster with `SELECT 1` until it responds (up to 30 retries). |
| `query(db, sql, name, after)` | node | Execute a raw SQL statement with `psql`. Fails on error. |
| `insert(db, table, values, after)` | node | Insert a single row from a dict of column→value pairs. |
| `select(db, table, columns, where, after)` | node | Run a `SELECT` with an optional `WHERE` clause. |
| `delete(db, table, where, after)` | node | Run a `DELETE` with a `WHERE` clause. |
| `upsert(db, table, values, conflict_columns, after)` | node | `INSERT … ON CONFLICT … DO UPDATE SET`. |

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

## Redis Module

**Import path:** `@babelsuite/redis`  
**Source:** [`examples/oci-modules/redis`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-modules/redis)

The Redis module starts a cache node and exposes key management helpers. Use it for feature flags, session tokens, rate-limit counters, or any workload that needs a seeded in-memory store before the application starts.

### Package layout

```
redis/
  module.star     # public entrypoint — re-exports all symbols
  cluster.star    # cache lifecycle node
  commands.star   # key/db management helpers
  usage.star      # runnable examples
  module.yaml     # OCI metadata
```

### Exported symbols

| Symbol | Returns | Description |
|--------|---------|-------------|
| `redis(name, image, port, password, max_memory, max_memory_policy, persistence, databases, after, env)` | node | Start a Redis cache. Set `persistence=True` for AOF + RDB. |
| `wait_ready(cache, after)` | node | Poll `PING` until the cache responds with `PONG`. |
| `set_key(cache, key, value, ttl_seconds, after)` | node | `SET key value [EX ttl]`. |
| `set_keys(cache, mapping, ttl_seconds, after)` | node | Set multiple keys from a dict in one task. |
| `delete_key(cache, key, after)` | node | `DEL key`. |
| `flush_db(cache, db, after)` | node | `FLUSHDB` on a specific logical database number. |
| `flush_all(cache, after)` | node | `FLUSHALL` — wipes every database. |

### Example

```python
load("@babelsuite/redis",   "redis", "set_keys", "flush_db", "wait_ready")
load("@babelsuite/runtime", "service")

cache = redis(name="cache", password="s3cr3t", max_memory="512mb")
ready = wait_ready(cache)
flush = flush_db(cache, db=0, after=[ready])

seed = set_keys(
    cache,
    mapping={"checkout_v2": "true", "promo_engine": "true"},
    after=[flush],
)

app = service.run(env={"REDIS_URL": cache["url"]}, after=[seed])
```

---

## MongoDB Module

**Import path:** `@babelsuite/mongodb`  
**Source:** [`examples/oci-modules/mongodb`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-modules/mongodb)

The MongoDB module starts a cluster node and exposes collection and document helpers. Use it for suites that need a real document store — schema creation, index building, seed data, and migration scripts all run as topology nodes wired with `after=[]`.

### Package layout

```
mongodb/
  module.star       # public entrypoint — re-exports all symbols
  cluster.star      # cluster lifecycle node
  collections.star  # collection, index, and document helpers
  usage.star        # runnable examples
  module.yaml       # OCI metadata
```

### Exported symbols

| Symbol | Returns | Description |
|--------|---------|-------------|
| `mongodb(name, image, port, username, password, replica_set, wired_tiger_cache_gb, after, env)` | node | Start a MongoDB cluster. Pass `replica_set` to enable replica set mode. |
| `create_collection(cluster, db, collection, after)` | node | Create a collection if it doesn't exist. |
| `create_index(cluster, db, collection, keys, unique, after)` | node | Create an index. `keys` is a dict of field→direction (e.g. `{"sku": 1}`). |
| `insert_documents(cluster, db, collection, documents, after)` | node | Insert a list of documents. |
| `drop_collection(cluster, db, collection, after)` | node | Drop a collection and all its documents. |
| `run_script(cluster, db, script_path, after)` | node | Execute a `mongosh` JS script for complex migrations. |

### Example

```python
load("@babelsuite/mongodb", "mongodb", "create_collection", "create_index", "insert_documents")
load("@babelsuite/runtime", "service")

db   = mongodb(name="mongo", username="root", password="secret")
col  = create_collection(cluster=db, db="catalog", collection="products")
idx  = create_index(cluster=db, db="catalog", collection="products", keys={"sku": 1}, unique=True, after=[col])
seed = insert_documents(
    cluster=db, db="catalog", collection="products",
    documents=[{"sku": "SKU-001", "name": "Widget A", "price": 9.99}],
    after=[idx],
)

app = service.run(env={"MONGO_URI": db["uri"]}, after=[seed])
```

---

## Playwright Module

**Import path:** `@babelsuite/playwright`  
**Source:** [`examples/oci-modules/playwright`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-modules/playwright)

The Playwright module runs browser tests, accessibility audits, and visual regression checks as topology nodes. Each helper spawns test runs across a configurable matrix of browsers and devices and returns a list of nodes — one per combination — that you wire into the rest of the graph with `after=[]`.

### Package layout

```
playwright/
  module.star   # public entrypoint — re-exports all symbols
  runner.star   # browser test, a11y, and visual diff helpers
  usage.star    # runnable examples
  module.yaml   # OCI metadata
```

### Exported symbols

| Symbol | Returns | Description |
|--------|---------|-------------|
| `browser_test(spec, base_url, browsers, devices, after, env, collect_traces, collect_video, extra_exports)` | list of nodes | Run a Playwright spec across a browser × device matrix. Returns one node per combination. Supported browsers: `chromium`, `firefox`, `webkit`. Supported devices: `desktop`, `mobile`, `tablet`. |
| `a11y_audit(url, after, wcag_level, fail_on_critical)` | node | WCAG accessibility audit using axe-core. `wcag_level` can be `A`, `AA`, or `AAA`. Exports JUnit and JSON reports. |
| `visual_diff(spec, base_url, browsers, threshold, after)` | list of nodes | Screenshot snapshot comparison. Fails if pixel diff exceeds `threshold` (default `0.01` = 1%). Exports snapshots on every run, diffs on failure only. |

### Example

```python
load("@babelsuite/playwright", "browser_test", "a11y_audit")
load("@babelsuite/runtime",   "service")

ui = service.run(name="storefront-ui")

checkout_nodes = browser_test(
    spec     = "checkout.spec.ts",
    base_url = "http://storefront-ui:3000",
    browsers = ["chromium", "firefox"],
    devices  = ["desktop", "mobile"],
    after    = [ui],
)

a11y_audit(
    url            = "http://storefront-ui:3000/checkout",
    after          = checkout_nodes,
    wcag_level     = "AA",
    fail_on_critical = True,
)
```

`browser_test` returns one node per browser/device combination, so downstream `after=checkout_nodes` waits for the full matrix to finish.

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
| Runtime library | Built-in topology primitives: `service`, `task`, `test`, `log`, `suite` |
| Built-in plugins | `traffic.*` and `security.*` — Lua code (`babelsuite-traffic-cannon`, `babelsuite-attack-scanner`) running inside the APISIX sidecar; no container |
| OCI module | Reusable Starlark helpers for specific infrastructure (Kafka, Postgres, Redis, MongoDB, Playwright, …) |
| User plugin | Custom Lua verification steps registered in `configuration.yaml` — same sidecar dispatch as built-in plugins |
| Suite | Runnable topology — assembles all of the above into a full environment |
| Suite dependency | Larger compositions of multiple suites via `suite.run(ref="...")` |

Modules sit between the runtime library and individual suites. They keep infrastructure setup out of your suite file and make it reusable across many suites without copy-paste.
