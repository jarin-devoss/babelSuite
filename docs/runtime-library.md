---
title: Runtime Library Reference
---

# Runtime Library Reference

[Back to index](index.md)

## What The Runtime Library Is

The runtime library is BabelSuite's built-in topology surface. It defines the node families that `suite.star` can assemble into a topology graph.

This is separate from the checked-in example OCI modules such as `kafka` or `postgres`.

## How The Parser Works

`suite.star` is processed by an assignment-based topology scanner, not a full Starlark interpreter. The parser scans each logical statement for this shape:

```text
<variable> = <family>(<arguments>)
```

Only statements that match this assignment shape are recognized as topology nodes. `load()` statements, comments, and blank lines are ignored by the topology resolver.

Backslash continuations are supported for chained `.export(...)` calls, but the parser still follows the same assignment-driven model.

## `load()` Statements

`load()` statements appear at the top of every example `suite.star`:

```python
load("@babelsuite/runtime", "service", "task", "test", "traffic", "security", "suite")
```

The topology parser ignores these lines. Their job is different:

- the workspace loader reads `load("...")` lines to extract module path strings such as `@babelsuite/runtime` and `@babelsuite/kafka`
- those paths are stored as the suite's contracts/module references
- the imported names are a documentation and authoring convention, not parse-time bindings

In short: write `load()` lines to declare intent and populate the contracts surface. The topology resolver does not need them to recognize node calls.

## Core Families

### `service`

Long-lived background infrastructure in the suite graph.

| Call | Kind |
|------|------|
| `service.run` | `service` |
| `service.wiremock` | `service` |
| `service.prism` | `service` |
| `service.custom` | `service` |

```python
db  = service.run()
api = service.run(after=[db])
```

Use `service.run` for first-party background dependencies such as databases, APIs, caches, and workers.

`service(...)` is intentionally rejected. Use one of the explicit `service.*` forms.

### `service.mock`

Native suite-defined mock surfaces.

| Call | Kind |
|------|------|
| `service.mock` | `mock` |

```python
orders = service.mock(after=[api])
```

`service.mock` is the runtime entrypoint for mocks backed by the suite's `api/` and `mock/` folders.

Compatibility aliases still parse:

- `mock.serve`

### `task`

Short-lived jobs: setup, seeding, migrations, and one-off operations.

| Call | Kind |
|------|------|
| `task.run` | `task` |

```python
migrate = task.run(file="migrate.py", image="python:3.12", after=[db])
seed    = task.run(file="seed.sh", image="bash:5.2", after=[migrate])
```

`task(...)` is intentionally rejected. Use `task.run(...)`.

`task.run` resolves `file=` relative to `tasks/`, so `file="seed.sh"` means `./tasks/seed.sh`.

### `test`

Verification, smoke checks, browser assertions, and pass/fail validation.

| Call | Kind |
|------|------|
| `test.run` | `test` |

```python
smoke = test.run(file="go/smoke_test.go", image="golang:1.24", after=[api])
```

`test(...)` is intentionally rejected. Use `test.run(...)`.

`test.run` resolves `file=` relative to `tests/`, so `file="go/smoke_test.go"` means `./tests/go/smoke_test.go`.

### `traffic`

Throughput, concurrency, and latency testing.

| Call | Kind |
|------|------|
| `traffic.smoke` | `traffic` |
| `traffic.baseline` | `traffic` |
| `traffic.stress` | `traffic` |
| `traffic.spike` | `traffic` |
| `traffic.soak` | `traffic` |
| `traffic.scalability` | `traffic` |
| `traffic.step` | `traffic` |
| `traffic.wave` | `traffic` |
| `traffic.staged` | `traffic` |
| `traffic.constant_throughput` | `traffic` |
| `traffic.constant_pacing` | `traffic` |
| `traffic.open_model` | `traffic` |

```python
perf = traffic.smoke(
    target="http://api:8080",
    after=[api],
)
```

`traffic(...)` is intentionally rejected. Use one of the explicit `traffic.*` forms.

Each `traffic.*` node must declare:

- `target="..."` as an absolute base URL for the native HTTP executor

Optionally:

- `plan="..."` pointing at a file under `traffic/` — enables advanced workload definitions with custom users, stages, tasks, and thresholds
- `rps=<number>` or `arrival_rate=<number>` — override the default request rate

Recommended meanings:

- `traffic.smoke`: a tiny preflight run before heavier profiles
- `traffic.baseline`: normal expected concurrent user load
- `traffic.stress`: push beyond normal operating range
- `traffic.spike`: sudden step up in user count
- `traffic.soak`: long-running endurance pressure
- `traffic.scalability`: breaking-point detection — probes successive user-count stages and reports the maximum sustainable load
- `traffic.step`: increase traffic in discrete blocks
- `traffic.wave`: oscillating high/low cycles
- `traffic.staged`: several named phases with different targets
- `traffic.constant_throughput`: cap requests per second independently of user count
- `traffic.constant_pacing`: fixed interval between iterations per user
- `traffic.open_model`: fixed arrival-rate style workload independent of response time

When `plan=` is omitted the native executor runs a synthetic baseline against `target=` using the APISIX sidecar — no separate plan file required.

For advanced workload control, create a `traffic/*.star` file and reference it via `plan=`. The native plan builders available inside those files include:

- `traffic.plan`
- `traffic.user`
- `traffic.task`
- `traffic.get`
- `traffic.post`
- `traffic.stage`
- `traffic.stages`
- `traffic.constant`
- `traffic.between`
- `traffic.pacing`
- `traffic.threshold`

Current behavior:

- the selected traffic profile is preserved in topology metadata and execution step payloads
- `target=` is the only required argument; `plan=` is optional and enables advanced workload definitions
- the native plan builders define concrete users, tasks, waits, stages, and thresholds
- the native runner executes real guarded HTTP traffic for supported profiles instead of only emitting synthetic traffic logs
- the native runner now reports richer latency and throughput summaries, including `min`, `avg`, `max`, `p50`, `p90`, `p95`, `p99`, per-stage summaries, compact throughput timelines, and latency histograms

Native threshold metrics currently supported:

- `http.error_rate`
- `http.min_ms`
- `http.avg_ms`
- `http.max_ms`
- `http.p50_ms`
- `http.p90_ms`
- `http.p95_ms`
- `http.p99_ms`
- `latency.min_ms`
- `latency.avg_ms`
- `latency.max_ms`
- `latency.p50_ms`
- `latency.p90_ms`
- `latency.p95_ms`
- `latency.p99_ms`
- `throughput.avg_rps`
- `throughput.peak_rps`

Current limit:

- the native executor currently supports HTTP `traffic.get(...)` and `traffic.post(...)` tasks only
- `traffic.scalability` runs each user-count stage as an isolated probe; the loop stops on the first threshold violation and reports the last passing user count as the maximum sustainable load — if all probes pass, the highest count is reported
- strict safety caps keep the control plane from self-harming with oversized traffic plans

### `security`

HTTP-layer security scanning via the APISIX sidecar — no extra containers required.

| Call | Kind |
|------|------|
| `security.probe` | `security` |
| `security.fuzz` | `security` |
| `security.auth` | `security` |
| `security.flood` | `security` |
| `security.headers` | `security` |
| `security.verbs` | `security` |
| `security.graphql` | `security` |
| `security.cors` | `security` |

```python
api   = service.mock(name="api")
probe = security.probe(name="probe", after=[api],
    exports=[{"path": "/findings/probe.json", "on": "always"}])
fuzz  = security.fuzz(name="fuzz", technique="sqli", after=[probe],
    exports=[{"path": "/findings/fuzz.json", "on": "always"}])
flood = security.flood(name="flood", path="/api/v1/resource",
    rate=50.0, duration=5.0, expect_throttle=True, after=[api],
    exports=[{"path": "/findings/flood.json", "on": "always"}])
```

`security(...)` is intentionally rejected. Use one of the explicit `security.*` forms.

Mode reference:

- `security.probe`: requests sensitive paths; flags unexpected 2xx/3xx responses
- `security.fuzz`: injects SQLi, XSS, path traversal, and header-injection payloads
- `security.auth`: verifies protected endpoints are unreachable without credentials
- `security.flood`: sends sustained traffic and expects HTTP 429 rate-limit responses
- `security.headers`: audits security response headers (HSTS, CSP, X-Frame-Options, …)
- `security.verbs`: probes dangerous HTTP methods (PUT, DELETE, TRACE, TRACK)
- `security.graphql`: detects exposed GraphQL introspection endpoints
- `security.cors`: finds CORS misconfigurations (reflected origin, wildcard credentials)

Arguments specific to `security.*` nodes:

| Argument | Used for |
|----------|---------|
| `technique="sqli"` | payload technique for `security.fuzz` (`sqli`, `xss`, `traversal`) |
| `path="..."` | target path for `security.flood` |
| `rate=<float>` | requests per second for `security.flood` |
| `duration=<float>` | run duration in seconds for `security.flood` |
| `expect_throttle=True` | assert HTTP 429 is returned under flood conditions |

The APISIX sidecar is provisioned automatically alongside every `service.mock` node, so `security.*` nodes derive their gateway URL from the mock's sidecar without a separate `target=` argument.

Each security step POSTs to `/_babelsuite/attack/start` and returns a synchronous JSON findings report: `{"total":N,"passed":N,"failed":N,"findings":[...]}`.

Security threshold metrics (analogous to traffic thresholds):

| Metric | Description |
|--------|-------------|
| `findings.total` | total number of findings emitted |
| `findings.failed` | number of failed checks |

Supported operators: `<`, `<=`, `>`, `>=`, `==`.


### `log`

Emits a structured log line at a specific point in the execution graph — no container, no image pull. The node completes instantly after emitting.

| Call | Level |
|------|-------|
| `log.info` | info |
| `log.warn` | warn |
| `log.error` | error |
| `log.debug` | debug |

```python
checkpoint = log.info("schema migrated — starting API warmup", after=[migrate])
api = service.run(after=[checkpoint])
```

The message can be passed as a positional argument or via `message=`:

```python
log.warn(message="feature flag disabled — skipping canary step", after=[db])
```

`log.*` nodes are valid anywhere in the graph, including inside OCI modules:

```python
def pg(name="db", ...):
    cluster = service.run(name=name, ...)
    ready   = log.info("postgres cluster ready at " + name + ":5432", after=[cluster])
    return {"service": cluster, "ready": ready, "name": name}
```

#### Template placeholders

Messages support `{{ ... }}` placeholders that are resolved at runtime from live execution state:

| Placeholder | Resolves to |
|---|---|
| `{{ suite }}` | Suite title (e.g. `Payment Suite`) |
| `{{ profile }}` | Active profile file name (e.g. `staging.yaml`) |
| `{{ total }}` | Total number of nodes in the topology |
| `{{ healthy }}` | Number of healthy nodes at the moment the log node runs |
| `{{ env.NAME }}` | Value of environment variable `NAME` from the active profile |

```python
infra_ready = log.info(
    "{{ suite }} on {{ profile }} — {{ healthy }}/{{ total }} nodes healthy, FRAUD_STRATEGY={{ env.FRAUD_STRATEGY }}",
    after=[db, broker, stripe_mock],
)
```

`log(...)` bare is rejected. Use one of the explicit `log.*` forms.

### `suite`

Imports another suite via `dependencies.yaml`.

| Call | Kind |
|------|------|
| `suite.run` | `suite` |

```python
payments = suite.run(ref="payments-module", after=[db])
```

`suite(...)` is intentionally rejected. Use `suite.run(...)`.

For nested suite manifests, see [Dependency Manifests](dependencies.md).

## Resolver Argument Extraction

The parser extracts these topology fields from recognized statements:

| Argument | Used for |
|----------|---------|
| left-hand assignment (`db = ...`) | default node identity |
| `name="..."` or `id="..."` or `name_or_id="..."` | optional identity override |
| `after=[db, api]` | dependency edges |
| `on_failure=[smoke]` | failure-trigger ordering edge |
| `file="..."` | task/test asset path |
| `plan="..."` | traffic plan file (optional — omit to use the native synthetic baseline) |
| `target="..."` | native traffic target |
| `rps=<float>` or `arrival_rate=<float>` | request rate override for `traffic.*` nodes |
| `technique="..."` | payload technique for `security.fuzz` (`sqli`, `xss`, `traversal`) |
| `path="..."` | target path for `security.flood` |
| `rate=<float>` | requests per second for `security.flood` |
| `duration=<float>` | run duration in seconds for `security.flood` |
| `expect_throttle=True` | assert HTTP 429 is returned under flood for `security.flood` |
| `ref="..."` | nested suite alias for `suite.run` |
| `expect_exit=0` | expected process exit code |
| `expect_logs="..."` or `expect_logs=["...", "..."]` | required log/output matches |
| `fail_on_logs="..."` or `fail_on_logs=["...", "..."]` | forbidden log/output matches |
| `continue_on_failure=true` | mark the node failed but let normal downstream nodes continue |

The variable name is the default node ID. Use `name=` or `id=` only when you need an explicit override.

## Artifact Exports

Nodes can attach artifact export rules with chained `.export(...)` calls:

```python
smoke = test.run(file="go/smoke_test.go", image="golang:1.24") \
  .export("coverage/*.xml", name="go-coverage", on="always", format="cobertura") \
  .export("reports/junit.xml", name="go-tests", format="junit") \
  .export("logs/crash.dump", name="crash-debug", on="failure")
```

Supported export arguments:

- first positional string or `path="..."` for the artifact path or glob
- `name="..."` for the exported artifact label
- `on="success" | "failure" | "always"` to control when the export should run
- `format="junit" | "cobertura"` for structured test and coverage summaries in the execution UI

Current behavior:

- export rules are parsed into topology metadata
- export rules flow into execution step payloads
- the local (Docker) runner bind-mounts a host directory into every container at `$BABELSUITE_ARTIFACTS_DIR`; files written there are harvested after the container exits
- glob patterns in `path=` (e.g. `"coverage/*.xml"`) are expanded on the host mount — the first lexically-ordered match is collected
- the Kubernetes runner copies files from the artifact sidecar volume using the busybox helper after the step container exits
- `format="junit"` exports are summarized into pass/fail counts in the live execution view; real XML from the container is parsed when present, synthetic otherwise
- `format="cobertura"` exports are parsed and line/branch coverage summaries are shown when real content is collected
- `format="ctrf"` (Common Test Results Format) exports are parsed and test summary counts are shown

Current limit:

- when a glob matches multiple files only the first (lexical order) is collected; structured formats such as JUnit cannot yet merge across multiple XML files in one pass

## Evaluation Controls

`task.run(...)`, `test.run(...)`, and other runnable nodes can declare explicit success/failure expectations:

```python
seed = task.run(
    file="seed.sh",
    image="bash:5.2",
    expect_exit=0,
    expect_logs="Task completed successfully",
    fail_on_logs=["FATAL ERROR", "panic:"],
)
```

Supported controls:

- `expect_exit=<int>`
- `expect_logs="..."`
- `expect_logs=["...", "..."]`
- `fail_on_logs="..."`
- `fail_on_logs=["...", "..."]`
- `continue_on_failure=true`
- `on_failure=[primary]`
- `reset_mocks=[billing_mock]`

Current behavior:

- exit-code expectations are checked after the runner finishes the step
- log assertions are matched against the emitted step log stream
- `traffic.*` steps still use threshold metrics for latency and error budgets
- `continue_on_failure=true` keeps the suite running even when the node itself finishes as `failed`
- `on_failure=[primary]` activates rollback or contingency nodes only when one of the referenced nodes fails
- `reset_mocks=[billing_mock]` clears persisted mock state before a `test.run(...)` step starts
- once a hard failure happens, unrelated branches are skipped while activated failure-path branches continue

Current limit:

- the local and orchestrated runners still use BabelSuite's current simulated task/test execution path, so log assertions match the step log stream rather than a full streamed process stdout/stderr capture

## Authoring Rules

- one node assignment per logical statement
- backslash continuations are supported for chained `.export(...)` calls
- the left-hand assignment becomes the default node ID
- use `after=[db, api]` to declare ordering; omitting it means the node has no dependencies
- use `on_failure=[primary]` for rollback or contingency branches
- quoted `after=["db"]` still parses, but identifier references are the preferred style
- `task.run(file="...")` resolves from `tasks/`
- `test.run(file="...")` resolves from `tests/`
- `traffic.*(plan="...")` resolves from `traffic/` — only applies when `plan=` is specified
- `ref=` is required for `suite.run`; the parser errors if it is missing
- duplicate `after` entries are deduplicated automatically
- dependency targets that do not exist in the graph produce a resolver error
- cycles are rejected

## Example Modules vs Runtime

The runtime library is compiled into BabelSuite. The checked-in example modules under `examples/oci-modules/` are separate pure Starlark packages built on top of this surface:

- `examples/oci-modules/kafka` -> `@babelsuite/kafka`
- `examples/oci-modules/postgres` -> `@babelsuite/postgres`
- `examples/oci-modules/redis` -> `@babelsuite/redis`
- `examples/oci-modules/mongodb` -> `@babelsuite/mongodb`
- `examples/oci-modules/playwright` -> `@babelsuite/playwright`

See [Modules](modules.md) for those package details.
