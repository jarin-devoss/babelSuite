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
| `@babelsuite/redis` | `examples/oci-modules/redis` | Available |
| `@babelsuite/mongodb` | `examples/oci-modules/mongodb` | Available |
| `@babelsuite/playwright` | `examples/oci-modules/playwright` | Available |

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

## Redis Module

**Import path:** `@babelsuite/redis`  
**Source:** `examples/oci-modules/redis`

The Redis module starts a cache node and exposes key management helpers. Use it for feature flags, session tokens, rate-limit counters, or any workload that needs a seeded in-memory store before the application starts.

### Package layout

```
redis/
  module.star     # public entrypoint — re-exports all symbols
  cluster.star    # cache lifecycle node
  commands.star   # key/db management helpers
  _shared.star    # internal utilities (not exported)
  usage.star      # runnable examples
  module.yaml     # OCI metadata
```

### Exported symbols

| Symbol | Returns | Description |
|--------|---------|-------------|
| `redis(name, port, password, max_memory, max_memory_policy)` | node | Start a Redis cache |
| `wait_ready(cluster, after)` | node | Wait until the cache accepts connections |
| `set_key(cluster, key, value, ttl_seconds, after)` | node | Set a single key |
| `set_keys(cluster, mapping, ttl_seconds, after)` | node | Set multiple keys from a dict |
| `delete_key(cluster, key, after)` | node | Delete a key |
| `flush_db(cluster, db, after)` | node | Flush one logical database |
| `flush_all(cluster, after)` | node | Flush all databases |

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
**Source:** `examples/oci-modules/mongodb`

The MongoDB module starts a cluster node and exposes collection and document helpers. Use it for suites that need a real document store — schema creation, index building, seed data, and migration scripts all run as topology nodes wired with `after=[]`.

### Package layout

```
mongodb/
  module.star       # public entrypoint — re-exports all symbols
  cluster.star      # cluster lifecycle node
  collections.star  # collection, index, and document helpers
  _shared.star      # internal utilities (not exported)
  usage.star        # runnable examples
  module.yaml       # OCI metadata
```

### Exported symbols

| Symbol | Returns | Description |
|--------|---------|-------------|
| `mongodb(name, port, username, password, wired_tiger_cache_gb)` | node | Start a MongoDB cluster |
| `create_collection(cluster, db, collection, after)` | node | Create a collection |
| `create_index(cluster, db, collection, keys, unique, after)` | node | Create an index |
| `insert_documents(cluster, db, collection, documents, after)` | node | Insert documents |
| `drop_collection(cluster, db, collection, after)` | node | Drop a collection |
| `run_script(cluster, db, script_path, after)` | node | Execute a JS migration script |

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
**Source:** `examples/oci-modules/playwright`

The Playwright module runs browser tests, accessibility audits, and visual regression checks as topology nodes. Each helper spawns test runs across a configurable matrix of browsers and devices and returns a list of nodes — one per combination — that you wire into the rest of the graph with `after=[]`.

### Package layout

```
playwright/
  module.star   # public entrypoint — re-exports all symbols
  runner.star   # browser test, a11y, and visual diff helpers
  _shared.star  # internal utilities (not exported)
  usage.star    # runnable examples
  module.yaml   # OCI metadata
```

### Exported symbols

| Symbol | Returns | Description |
|--------|---------|-------------|
| `browser_test(spec, base_url, browsers, devices, after, env)` | list of nodes | Run a Playwright spec across a browser/device matrix |
| `a11y_audit(url, after, wcag_level, fail_on_critical)` | node | WCAG accessibility audit against a live URL |
| `visual_diff(spec, base_url, browsers, threshold, after)` | node | Visual regression snapshot comparison |

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
| Runtime library | Built-in topology primitives (`service`, `task`, `test`, `traffic`) |
| OCI module | Reusable Starlark helpers for specific infrastructure (Kafka, Postgres, Redis, MongoDB, Playwright, …) |
| Suite | Runnable topology — assembles primitives and modules into a full environment |
| Suite dependency | Larger compositions of multiple suites via `suite.run(ref="...")` |

Modules sit between the runtime library and individual suites. They keep infrastructure setup out of your suite file and make it reusable across many suites without copy-paste.
