---
title: Runtime Library Reference
---

# Runtime Library Reference

[Back to index](index.md)

## What The Runtime Library Is

The runtime library is BabelSuite's built-in topology surface. It defines the node families that `suite.star` can assemble into a topology graph.

This is separate from the checked-in example OCI modules such as `kafka` or `postgres`.

## How The Parser Works

`suite.star` is evaluated by a real Starlark interpreter, so the file is a
program rather than a list of declarations. Control flow runs:

```python
for region in REGIONS:                    # loops build nodes
    test.run(name="smoke-" + region, ...)

if FRAUD_STRATEGY == "shadow":            # conditionals apply
    log.warn("shadow mode")

def fan_out(prefix, count):               # functions are available
    return [service.run(name=prefix + str(i), image="worker:latest") for i in range(count)]
```

A node is registered when a `service.*`, `task.*`, `test.*`, `traffic.*`,
`security.*`, `log.*`, `suite.*` or plugin function is **called**. Assigning the
result to a variable is the normal style because the variable name becomes the
node's default id and lets other nodes reference it in `after=[...]`, but the
call is what creates the node.

Evaluation errors are reported as Starlark errors naming the file and line, so
a typo fails the suite at resolution time rather than silently dropping a node.

## `load()` Statements

`load()` statements appear at the top of every example `suite.star`:

```python
load("@babelsuite/runtime", "service", "task", "test", "traffic", "security", "suite")
```

These are real imports: the names they bind are what the rest of the file calls,
and loading a module or plugin that cannot be resolved is an error rather than a
comment. The workspace loader additionally reads the module path strings to
populate the suite's contracts/module references.

### `env`: the launch profile's environment

`env` is the environment of the profile the run was launched with: the profile's
`env:` section, with the platform's global overrides applied on top. It is
read-only (assigning to it is an error), so `env.get("NAME", "fallback")` and
`env["NAME"]` read the profile, and a profile can change **which steps exist**:

```python
BROWSERS = env.get("BROWSERS", "chromium").split(",")   # the profile's BROWSERS

for b in BROWSERS:                  # one step per browser in the profile
    test.run(name="checkout-" + b, ...)
```

A profile with `BROWSERS: chromium,firefox,webkit` builds three checkout steps; a
profile without it builds one. Values are strings, so a count is
`int(env.get("COUNT", "1"))`.

- **Secrets are not in `env`.** A secret a profile references must not decide the
  shape of the graph, or turn up in a step's name. Read it in the step, as an
  environment variable.
- **The catalog and the graph view** have no launch, so they show what the suite's
  *default* profile builds. A run shows the graph of the profile it was launched
  with.
- **A nested suite does not see its parent's `env`.** It reads the profile its
  dependency entry names (or its default one), plus the `inputs:` it was given.

What else reads the active profile:

- a step's `env=`, profile `env:` and `services.<step>.env:` — injected into the
  container, so your script reads them normally
- a dependency's `inputs:` — injected into the imported suite's steps
- `{{ env.NAME }}` inside a `log.*` message — resolved at runtime

### A node's `env`

A node returned by `service.run`, `task.run` and the other calls has a read-only
`env`: the environment the node was created with. A module's helpers read it to
learn what the service was started with, instead of guessing a default:

```python
db = pg(port=5433, database="shop")
db.env["PGPORT"]        # "5433"
```

## Core Families

### `service`

Long-lived background infrastructure in the suite graph.

| Call | Kind |
|------|------|
| `service.run` | `service` |

```python
db  = service.run(image="postgres:16")
api = service.run(image="payments-api:latest", after=[db])
```

Use `service.run` for first-party background dependencies such as databases, APIs, caches, and workers.

`image=` is required: `service.run` starts that container and runs it **detached** — the node signals healthy as soon as the container is up, then keeps it running in the background while downstream nodes proceed. The container is torn down when the execution ends. A `service.run` with no image is an error; to fake an API rather than run one, use [`mock.api`](#mockapi).

```python
cache = service.run(image="redis:7-alpine", after=[db])
worker = service.run(
    image="myapp:latest",
    commands=["./worker --queue jobs"],
    after=[cache],
)
```

`commands=` is available as an inline alternative to a separate `file=` script — the commands are encoded into a `BABELSUITE_SCRIPT` environment variable and executed inside the container via `/bin/sh -e`. See the [CI_SCRIPT injection](#ci_script-injection) section below.

`service(...)` is intentionally rejected. Use one of the explicit `service.*` forms.

### `mock.api`

Native suite-defined mock surfaces.

| Call | Kind |
|------|------|
| `mock.api` | `mock` |

```python
orders = mock.api(after=[api])
```

`mock.api` is the runtime entrypoint for mocks backed by the suite's `api/` and `mock/` folders. It starts no container: the suite's APISIX sidecar answers it.

| Argument | Used for |
|----------|---------|
| `harden=True` | opts the APISIX sidecar into gateway-provided defenses — see [Hardening and `security.*` checks](#hardening-and-security-checks) below |


### `task`

Short-lived jobs: setup, seeding, migrations, and one-off operations.

| Call | Kind |
|------|------|
| `task.run` | `task` |

```python
migrate = task.run(file="migrate.py", image="python:3.12", after=[db])
seed    = task.run(file="seed.sh",    image="bash:5.2",    after=[migrate])
```

Use `file=` to reference a script bundled in the suite artifact, or `commands=` for inline shell commands:

```python
build = task.run(
    image="node:22-alpine",
    commands=["npm ci", "npm run build"],
    after=[db],
)
```

`task(...)` is intentionally rejected. Use `task.run(...)`.

`task.run` resolves `file=` relative to `tasks/`, so `file="seed.sh"` means `./tasks/seed.sh`.

Both `file=` and `commands=` execute via the [CI_SCRIPT injection](#ci_script-injection) mechanism — see that section for interpreter inference and environment details.

### `test`

Verification, smoke checks, browser assertions, and pass/fail validation.

| Call | Kind |
|------|------|
| `test.run` | `test` |

```python
smoke = test.run(file="go/smoke_test.go", image="golang:1.24", after=[api])
```

Use `file=` to reference a test script from the suite artifact, or `commands=` for inline assertions:

```python
ping = test.run(
    image="curlimages/curl:8",
    commands=["curl -sf http://api:8080/health"],
    after=[api],
)
```

`test(...)` is intentionally rejected. Use `test.run(...)`.

`test.run` resolves `file=` relative to `tests/`, so `file="go/smoke_test.go"` means `./tests/go/smoke_test.go`.

Both `file=` and `commands=` execute via the [CI_SCRIPT injection](#ci_script-injection) mechanism — see that section for interpreter inference and environment details.

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
- `min_users=<number>` — on `traffic.scalability`, the number of users the target must sustain for the step to pass

Recommended meanings:

- `traffic.smoke`: a tiny preflight run before heavier profiles
- `traffic.baseline`: normal expected concurrent user load
- `traffic.stress`: push beyond normal operating range
- `traffic.spike`: sudden step up in user count
- `traffic.soak`: long-running endurance pressure
- `traffic.scalability`: breaking-point detection — probes successive user-count stages and reports the maximum sustainable load
- `traffic.step`: increase traffic in discrete blocks
- `traffic.wave`: oscillating high/low cycles
- `traffic.staged`: several phases of different user counts in one run
- `traffic.constant_throughput`: cap requests per second independently of user count
- `traffic.constant_pacing`: fixed interval between iterations per user
- `traffic.open_model`: fixed arrival-rate style workload independent of response time

Every variant has a default shape, used when `plan=` is omitted. The default request is `GET /` on the target.

| Variant | Default shape |
|---------|---------------|
| `traffic.smoke` | 1 user for 10 s |
| `traffic.baseline` | 5 users for 30 s |
| `traffic.stress`, `traffic.staged` | 5, 20, then 32 users, 20 s each |
| `traffic.spike` | 5 users, a jump to 32, back to 5, 15 s each |
| `traffic.soak` | 5 users for 60 s (a plan can ask for up to the 300 s cap) |
| `traffic.step` | 4, 8, 12, then 16 users, 15 s each |
| `traffic.wave` | 4, 16, 4, then 16 users, 15 s each |
| `traffic.scalability` | probes of 1, 2, 4, 8, 16, then 32 users, 10 s each |
| `traffic.constant_throughput`, `traffic.open_model` | 30 s at `rps=` / `arrival_rate=` (5 requests per second when neither is given) |
| `traffic.constant_pacing` | 5 users for 30 s, one iteration per user per second |

A closed-model variant given `rps=` runs as an open model at that rate for the same stages' durations, because `rps=` counts arrivals per second, which a number of virtual users cannot honour. Rates are capped at 100 requests per second, users at 32, and a run at 300 seconds.

### Where traffic goes

The target decides where the requests are sent. Nothing is simulated: the requests leave the suite's APISIX sidecar and reach the target, and every number in the log is measured from them.

| Target | Where it goes |
|--------|---------------|
| A service of the suite (`http://api:8080` for a `service.run` named `api`) | The sidecar joins the run's network and dials the service's address on it |
| The suite's mock host (`https://payment-suite.mock.internal`) | The sidecar's own listener, with the mock host as the `Host` header |
| `localhost` or `127.0.0.1` | The machine running the control plane, through `host.docker.internal` |
| Any other fully-qualified host or address | Dialled as written |

A bare name that is none of the suite's services is refused when the suite loads, and again when the step runs (`targets host "ghost", which is not a service in this suite`). A target that is a service of a Kubernetes backend is refused too, since the sidecar cannot reach into a cluster.

### When a traffic step fails

A traffic step fails, with the reason in its log, when:

- the run sent no request, or a stage sent none (`no requests were sent to ...`);
- no request got an answer the step counts as success (`target ... did not answer a single request successfully`), which is reported early, within seconds, instead of after the whole run;
- a request's status is not expected: any status of 400 or above is a failure unless the task says it expects it with `checks=[traffic.threshold("status", "==", 404)]`;
- a threshold is broken. `traffic.dropped` counts the arrivals an open-model run could not start because too many requests were already in flight, which means the target could not keep up. `http.error_rate` and the latency metrics are computed from the exact counts the sidecar reports;
- a `traffic.scalability` run sustained fewer users than `min_users=` (`sustained 4 users, below the min_users=8 the step requires`), or its first probe already broke;
- the suite's sidecar is not running, or the step has no load spec.

The log lists the most common error categories (`connect`, `timeout`, `http_4xx:404`, and so on) beside the failure.

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
- the requests are real: they are sent by the suite's APISIX sidecar to the target, and the figures in the log are measured from them (see the target rules above)
- a run that sent no requests fails; it never passes on zero data
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

HTTP-layer security scanning. The scanner runs inside the suite's APISIX sidecar, so it needs no extra containers.

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
api   = service.run(name="api", image="my-api:latest")
probe = security.probe(name="probe", target="http://api:8000", after=[api],
    exports=[{"path": "/findings/probe.json", "on": "always"}])
fuzz  = security.fuzz(name="fuzz", technique="sqli", target="http://api:8000", after=[probe],
    exports=[{"path": "/findings/fuzz.json", "on": "always"}])
flood = security.flood(name="flood", target="http://api:8000", path="/api/v1/resource",
    rate=50.0, duration=5.0, expect_throttle=True, after=[api],
    exports=[{"path": "/findings/flood.json", "on": "always"}])
```

`security(...)` is intentionally rejected. Use one of the explicit `security.*` forms. A key a mode does not use (for example `technique=` on `security.headers`) is rejected when the suite loads, with the list of keys it accepts.

Mode reference:

- `security.probe`: requests sensitive paths; flags unexpected 2xx/3xx responses
- `security.fuzz`: injects SQLi, XSS, path traversal, and header-injection payloads
- `security.auth`: verifies protected endpoints are unreachable without credentials
- `security.flood`: sends sustained traffic and checks the target throttles it
- `security.headers`: audits security response headers (HSTS, CSP, X-Frame-Options, ...)
- `security.verbs`: probes dangerous HTTP methods (PUT, DELETE, TRACE, TRACK)
- `security.graphql`: detects exposed GraphQL introspection endpoints
- `security.cors`: finds CORS misconfigurations (reflected origin, wildcard credentials)

Arguments specific to `security.*` nodes:

| Argument | Used for |
|----------|---------|
| `target="..."` | what to scan; see [Targets](#targets) below |
| `fail_on="low"` | lowest severity that fails the step: `low` (default, any finding fails), `medium`, `high` or `critical` |
| `technique="sqli"` | payload technique for `security.fuzz`: `sqli`, `xss`, `traversal` or `header-injection` (default: all four) |
| `path="..."` | target path for `security.flood` |
| `rate=<float>` | requests per second for `security.flood` (at most 100) |
| `duration=<float>` | run duration in seconds for `security.flood` (at most 60) |
| `expect_throttle=True` | `security.flood` only: fail unless the target answers HTTP 429 |

#### Targets

A security step scans the address in `target=`, resolved the way a `traffic.*` target is:

| `target=` | Scanned at |
|-----------|-----------|
| a `service.run` node of the suite (`http://api:8000`) | the service's address on the run's network; the sidecar joins that network before the scan |
| a `mock.api` node of the suite | the mock, through the sidecar's gateway port |
| `localhost` / `127.0.0.1` | the Docker host |
| a fully qualified name or an IP | as written |
| any other bare host name | an error when the suite loads, since nothing could resolve it |
| a Kubernetes service | refused: the scanner cannot reach it from the control plane |

With no `target=`, the step scans the suite's `mock.api` through the gateway. That measures the gateway and the mock, not a service of yours, and the step logs a warning saying so. A suite with no mock and no `target=` is rejected when it loads.

#### Results

Every check ends **passed**, **failed** or **unknown**. A check is unknown when the scanner could not send its request or got no answer; unknown is never counted as passed. The step fails when:

- a finding is at or above `fail_on`;
- no check reached the target (no request got an answer), or
- any check is unknown.

The log ends with `Assessment complete: total=N passed=N failed=N unknown=N findings=N requests=N`, and each finding is logged with its label, method, path and severity.

`security.flood` sends `rate` requests per second for `duration` seconds. With `expect_throttle=True` it fails unless some response is HTTP 429. Without it, it fails when more than 1% of the answers are 5xx or errors, since the target did not survive the load. If more than half the requests error, the check is unknown.

#### Reports

A security step runs inside the control plane, not in a container, so it has no files of its own. Its JSON report (`mode`, `target`, `total`, `passed`, `failed`, `unknown`, `findings`, ...) is the artifact `exports=` collects. On a Docker backend it is also written to the execution's workspace at the exported path, so a later task can read it, as `security-suite`'s `verify-findings` does.

#### Hardening and `security.*` checks

By default the gateway adds nothing on top of your backend: `security.headers` and `security.flood` observe whatever the target actually returns, so a backend that is genuinely missing security headers or rate limiting genuinely fails those checks.

Pass `harden=True` to `mock.api(...)` to opt the sidecar into two gateway-provided defenses:

- a baseline set of security response headers (HSTS, CSP, X-Frame-Options, X-Content-Type-Options, Referrer-Policy, Permissions-Policy) applied to every response, including unmatched-route 404s
- a synthetic rate-limited route at `/api/v1/resource` (HTTP 429 after 20 requests/second)

This is opt-in and suite-scoped. When it is on and a security step scans the mock, the header and flood results measure the gateway, not your backend, and the step says so in its log. Scan a `service.run` target to measure the service itself.

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
api = service.run(image="payments-api:latest", after=[checkpoint])
```

The message can be passed as a positional argument or via `message=`:

```python
log.warn(message="feature flag disabled — skipping canary step", after=[db])
```

`log.*` nodes are valid anywhere in the graph, including inside OCI modules:

```python
def pg(name="db", ...):
    cluster = service.run(name=name, image=image, ...)
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

#### Live terminal rendering

`log.*` nodes render inline in the live execution terminal as the graph runs — no container pull, no queue wait. Template placeholders are resolved at the moment the node executes, so `{{ healthy }}` and `{{ total }}` reflect real-time graph state.

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

### `plugin`

User-registered Lua plugins. Plugins are not part of the built-in runtime library — they are loaded with `load("@plugins/<name>", "<operation>")` and must be registered in platform settings before a suite that references them can run.

```python
load("@plugins/spice-sim", "transient")

spice_mock = mock.api(name="spice-service")
rc_filter  = transient(name="rc-filter", netlist="...", probe_node="2", after=[spice_mock])
```

The function call forwards its keyword arguments as the plugin config. The APISIX sidecar dispatches the request to the plugin's trigger path, and the returned `passed` / `findings` / `summary` / `level` fields drive the step outcome.

For the full plugin reference, see [Platform Settings — Plugins](platform.md#plugins).

## Resolver Argument Extraction

The parser extracts these topology fields from recognized statements:

| Argument | Used for |
|----------|---------|
| left-hand assignment (`db = ...`) | default node identity |
| `name="..."` or `id="..."` or `name_or_id="..."` | optional identity override |
| `after=[db, api]` | dependency edges |
| `on_failure=[smoke]` | failure-trigger ordering edge |
| `image="..."` | container image for `task.run`, `test.run`, and `service.run` |
| `file="..."` | task/test asset path (alternative to `commands=`) |
| `commands=["...", "..."]` | inline shell commands (alternative to `file=`) |
| `plan="..."` | traffic plan file (optional — omit to use the variant's default shape) |
| `target="..."` | native traffic or security target |
| `rps=<float>` or `arrival_rate=<float>` | request rate override for `traffic.*` nodes |
| `fail_on="..."` | lowest severity that fails a `security.*` step (`low`, `medium`, `high`, `critical`) |
| `technique="..."` | payload technique for `security.fuzz` (`sqli`, `xss`, `traversal`, `header-injection`) |
| `path="..."` | target path for `security.flood` |
| `rate=<float>` | requests per second for `security.flood` |
| `duration=<float>` | run duration in seconds for `security.flood` |
| `expect_throttle=True` | fail `security.flood` unless the target answers HTTP 429 |
| `ref="..."` | nested suite alias for `suite.run` |
| `expect_exit=0` | expected process exit code |
| `expect_logs="..."` or `expect_logs=["...", "..."]` | required log/output matches |
| `fail_on_logs="..."` or `fail_on_logs=["...", "..."]` | forbidden log/output matches |
| `continue_on_failure=true` | mark the node failed but let normal downstream nodes continue |

The variable name is the default node ID. Use `name=` or `id=` only when you need an explicit override.

## Artifact Exports

A node attaches artifact export rules with `exports=[...]`:

```python
smoke = test.run(
    file="go/smoke_test.go",
    image="golang:1.24",
    exports=[
        {"path": "coverage/*.xml", "name": "go-coverage", "on": "always", "format": "cobertura"},
        {"path": "reports/junit.xml", "name": "go-tests", "format": "junit"},
        {"path": "logs/crash.dump", "name": "crash-debug", "on": "failure"},
    ],
)
```

Each entry is a dict with these keys. Any other key is a suite validation error, so a typo is caught when the suite loads:

- `path` (required): the artifact path or glob, relative to the step's working directory
- `name`: the exported artifact label
- `on`: `"success"` (the default), `"failure"` or `"always"`, to control when the export is kept
- `format`: `"junit"`, `"ctrf"` or `"cobertura"` for structured summaries in the execution UI. Any other value is a validation error
- `enforce`: `False` stops a `junit` or `ctrf` report from failing its step (see below). It is an error on any other format

### What happens to an export

The runner reads every declared export after the step's container exits, and the control plane keeps the ones whose `on=` matches the step's **final** status. That status can differ from the container's exit: a failed `fail_on_logs` check makes the step fail although the container exited 0, and an `on="failure"` export is still kept.

Every export shows one status in the execution view, and only a `collected` one has counts:

| Status | Meaning |
|--------|---------|
| `collected` | A file was read and, for a structured format, parsed |
| `missing` | No file matched the path |
| `empty` | The file has no content |
| `invalid` | The file could not be parsed (not XML, the wrong root element, a cut-off document) |
| `truncated` | A file is over the size limit, so it was not read |
| `unreadable` | The file exists but could not be read, for example a directory where a file belongs |
| `not_collected` | The backend cannot read files from where the step ran (remote agents) |

A report nobody produced is never filled in: a missing, empty or invalid export has no test counts, and the UI says why.

### Test reports decide the step

A `junit` or `ctrf` export is a claim about tests, so when the step's own checks pass (exit code, `expect_logs`, `fail_on_logs`) its reports are checked too. The step **fails** when a report:

- was not produced, is empty, cannot be parsed, or was not collected;
- lists no tests;
- lists failures or errors (`report "go-tests" (reports/junit.xml): 2 of 5 tests failed`).

This applies to exports that are kept on success (`on="success"` or `"always"`). `continue_on_failure` and `on_failure=` treat it like any other failure. `cobertura` exports never fail a step.

Add `"enforce": False` to show a report without letting it fail the step. An unenforced report that is not collected adds one warning line to the step's log.

### Several files

A glob collects every matching file, up to 50 files per export and 50 MB per step, each at most 10 MB. `junit` and `ctrf` summaries add up across the files, and the artifact lists the files it matched. A file over a limit makes the export `truncated`. A `cobertura` export summarises only the first file.

### JUnit counting

When a `<testsuite>` lists `<testcase>` elements, those are counted. The `tests`, `failures`, `errors` and `skipped` attributes are used only for a file that lists no cases. If the two disagree, the cases win and the artifact carries a note saying so.

### Where the files come from

- the local (Docker) runner bind-mounts the shared execution workspace into every container and reads each export from it, so a step writes `reports/junit.xml` relative to its working directory
- the Kubernetes runner reads files from the artifact sidecar volume through the pod exec API
- remote agents run the step on another machine and do not return files yet, so their exports are `not_collected`, and an enforced `junit` or `ctrf` export fails the step there. Use `"enforce": False` on a suite that runs on a remote agent

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
- `traffic.*` steps use threshold metrics for latency and error budgets, and a run with zero requests fails before any threshold is read
- `continue_on_failure=true` keeps the suite running even when the node itself finishes as `failed`
- `on_failure=[primary]` activates rollback or contingency nodes only when one of the referenced nodes fails
- `reset_mocks=[billing_mock]` clears the mock state **of this run** before a `test.run(...)` step starts; another run of the same suite keeps its own (see [Stateful Mocking](mocking.md#stateful-mocking))
- once a hard failure happens, unrelated branches are skipped while activated failure-path branches continue

Current behavior:

- exit-code and log assertions apply to any step that runs in a container (`file=` or `commands=` steps)
- `log.*` node output is included in the step log stream for `expect_logs` / `fail_on_logs` matching

## CI_SCRIPT Injection

`task.run`, `test.run`, and `service.run` steps that specify `file=` or `commands=` execute inside a Docker container via an injected script.

### How it works

1. **Script assembly** — the runner builds a POSIX shell script from `commands=` entries, or writes the `file=` content to the shared execution workspace and prepends the appropriate interpreter call.
2. **Encoding** — the script is base64-encoded and injected as `BABELSUITE_SCRIPT` into the container environment.
3. **Execution** — the container entrypoint is overridden to:
   ```
   /bin/sh -c "echo $BABELSUITE_SCRIPT | base64 -d | /bin/sh -e"
   ```
4. **Exit code** — the shell exits with the script's exit code, which the runner records as the step result.

### `file=` together with `commands=`

A step that sets both ships the file and runs **only its commands**. The file is
written to the workspace, where the commands find it, and is not run by its own
interpreter. Use it to give a command a file to work on: a spec the test runner
is told to run, a config it reads.

```python
test.run(
    name     = "checkout",
    image    = "mcr.microsoft.com/playwright:v1.53.0-noble",
    file     = "checkout.spec.ts",                      # shipped, not run by node
    commands = ["npx playwright test checkout.spec.ts"],
)
```

### Interpreter inference for `file=`

The interpreter is inferred from the file extension when `file=` is used:

| Extension | Interpreter |
|-----------|-------------|
| `.sh` | `sh` |
| `.bash` | `bash` |
| `.py` | `python` |
| `.js` | `node` |
| `.rb` | `ruby` |
| `.ts` | `node --experimental-strip-types`, falling back to `npx ts-node` |
| other | `/bin/sh` (treated as shell script) |

`.sh` runs under `sh`, not `bash`. A step picks its image for the tool it needs
— `curlimages/curl`, `redis`, `node:22-alpine` — and most of those are Alpine
based with no bash at all, so the shell this runner can actually promise is
`sh`. Write POSIX shell in a `.sh` file; name it `.bash` when it genuinely needs
bash, and pick an image that has it.

`.ts` is run by the image's own Node when it can strip type annotations itself,
which Node does from 22.6 on. Only an older runtime falls back to
`npx ts-node`, which downloads ts-node from the registry on first use — so a
`.ts` step on current Node needs no network at all.

!!! warning "The image must carry the inferred interpreter"
    The step runs `<interpreter> <file>` inside the container, so the image has
    to provide that interpreter on `PATH`. If it does not, the shell cannot find
    it and the step exits **127** — the same code for every row of the table, so
    a `.py` file in an image without `python` fails exactly like a `.bash` file
    in one without `bash`.

    Two ways out:

    - match the image to the file type — `python:3.12` for `.py`, `node:22` for
      `.js`, `bash:5.2` for `.bash`
    - use `commands=` instead of `file=`, which always runs through `/bin/sh`
      and so works in any image with a shell

### Environment variables available inside the container

| Variable | Value |
|----------|-------|
| `BABELSUITE_SCRIPT` | base64-encoded script (consumed automatically by the entrypoint) |
| `BABELSUITE_WORKSPACE_DIR` | path to the shared execution workspace bind-mounted into the container |
| `BABELSUITE_GATEWAY_URL` | the APISIX sidecar address this step can reach, when the suite has a mock surface. On Docker it is `http://babelsuite-gateway:9080`: the sidecar joins the run's network under that name and under the suite's mock hosts (`<suite>.mock.internal`) |
| `BABELSUITE_GATEWAY_URLS` | comma-separated address per mock node, in topology order |
| `BABELSUITE_GRPC_GATEWAY` | `host:port` for a gRPC client: the same port as the gateway URL, which speaks HTTP/2 next to HTTP/1.1 |
| `BABELSUITE_TCP_MOCKS` | the suite's TCP mocks as `<surface>/<operation>=<host>:<port>`, comma-separated — every TCP mock has a port of its own |
| `BABELSUITE_UDP_MOCKS` | the same for the suite's UDP mocks |
| `BABELSUITE_EXECUTION_ID` | the id of this run. Send it as `X-Babelsuite-Execution` to name the run to a stateful mock when the request does not go through the gateway |
| `BABELSUITE_ARTIFACTS_DIR` | Kubernetes backend only — path to the artifacts sidecar mount |

A plugin step's config is its call's keyword arguments over the profile's
`services.<step>.env`. A keyword left at an empty default (`""` or `None`) does
not override a value the profile sets, so a `plugin.star` wrapper can forward
every parameter without erasing the profile's.

`BABELSUITE_GATEWAY_URL` is how a `task.run` or `test.run` reaches a mock
surface without hardcoding a host and port. It is set only when the suite has
one, so a step can tell the difference between "no gateway" and "wrong address".
On Docker a mock is also reachable by the name of its node: a `mock.api(name="stripe-mock")`
answers at `http://stripe-mock:9080`, unless a service of the suite already has that name.

!!! note "`BABELSUITE_ARTIFACTS_DIR` is Kubernetes-only"
    The Docker backend does not set it. There, `exports=` paths are read
    from the shared workspace mount after the container exits, so write exports
    to a path relative to the working directory (`reports/junit.xml`) and the
    runner collects them.

### Detached services

`service.run(image="...")` runs the container detached:

- The container is started and the runner waits up to 500 ms for an immediate crash.
- If the container is still running, the node is marked **healthy** and downstream nodes proceed.
- Stdout/stderr are streamed to the execution log for the lifetime of the execution.
- The container is stopped and removed when the execution context is cancelled (execution end or failure).

## Profile-Controlled Step Config

Hardware, memory, CPU and networking are declared in the **profile YAML**, not in `suite.star`. This keeps the suite portable — the same topology runs on a laptop (no GPU, default bridge, modest memory) and a GPU cluster (with devices, execution network, a larger ceiling) by switching profiles.

### `services.<name>.devices`

Attach hardware devices to a step's container:

```yaml
# perf.yaml
services:
  seed-job:
    devices: ["gpu"]        # NVIDIA GPU (all)
  capture:
    devices: ["/dev/video0"]  # camera
  verify-hw:
    devices: ["/dev/ttyUSB0"] # USB serial
```

Device values translate automatically per backend — see [Profiles](profiles.md#servicesnamedevices) for the full mapping table (Docker `DeviceRequest`/`DeviceMapping`, Kubernetes `nvidia.com/gpu` resource limits, remote agent capability matching).

### `services.<name>.memory`

Raise one step's container memory ceiling above the 512 MB default:

```yaml
# local.yaml
services:
  ollama:
    memory: 2Gi
```

Accepts the Kubernetes quantity grammar — binary units (`2Gi`, `512Mi`),
decimal units (`2G`, `512M`), fractions (`1.5Gi`), or a plain byte count.
Suffixes are case-sensitive, and `2GB`/`512MB` are not valid. It is a ceiling
rather than a reservation, so the step still uses only what it touches, and
every other step keeps the default. A step whose process dies with
`signal: killed` during start-up has hit this limit — see
[Profiles](profiles.md#servicesnamememory).

### `services.<name>.cpu`

Cap one step's CPU. Unset is unlimited, the default:

```yaml
# local.yaml
services:
  ollama:
    cpu: "2"        # cores; "500m" for millicores
```

Docker applies it as `NanoCPUs`, Kubernetes as `limits.cpu` — see
[Profiles](profiles.md#servicesnamecpu).

### `network.mode`

```yaml
# staging.yaml
network:
  mode: execution   # the only mode: every run has its own network
```

Every run has its own Docker bridge network. Every container in it joins, and the node's `name` becomes its DNS hostname — a `service.run(name="redis", image="redis:7-alpine")` is reachable at `redis:6379` from any `task.run` or `test.run` in the same execution. No `suite.star` changes required, and the key can be left out.

`host` and `none` are refused (the run does not start, and saving the profile fails), because a profile-wide mode cannot work: the shipped modules and the suite's gateway reach other steps by name on that network, so a suite with a service could not run without it.

```python
# suite.star — works the same regardless of network mode
redis   = service.run(name="redis",  image="redis:7-alpine")
api     = service.run(name="api",    image="myapp:latest", after=[redis])
smoke   = test.run(
    image    = "python:3.12",
    commands = ["python test.py --host api --redis redis:6379"],
    after    = [api],
)
```

The network is created at execution start and removed when the execution finishes, alongside the shared workspace directory.

### Service names when suites are composed

Node ids are namespaced when a suite is imported (`identity-broker/db`), but a
hostname has no namespace — the network is flat. Three dependencies that each
call `pg()` would otherwise all answer to `db`, and Docker lets them share that
alias, so `psql ... @db:5432` reaches whichever one DNS returned.

A contested name therefore has exactly one publisher:

| Node | Answers to |
|------|-----------|
| A service in the suite being run | its bare name — `db` |
| A service in an imported suite, name uncontested | its bare name — `broker-api` |
| A service in an imported suite, name contested | `<suite>-<service>` — `identity-broker-db` |

The suite being run keeps the bare name, so its own steps are unaffected. Point
an imported suite at its own service with `inputs:` in `dependencies.yaml` —
see [Dependency Manifests](dependencies.md#contested-service-names). Left
unwired, the bare name resolves to nothing and the step fails saying so, rather
than silently reading another suite's database.

## Authoring Rules

- one node assignment per logical statement
- the left-hand assignment becomes the default node ID
- use `after=[db, api]` to declare ordering; omitting it means the node has no dependencies
- use `on_failure=[primary]` for rollback or contingency branches
- quoted `after=["db"]` still parses, but identifier references are the preferred style
- `task.run(file="...")` resolves from `tasks/`; `task.run(commands=[...])` is the inline alternative — both require `image=`
- `test.run(file="...")` resolves from `tests/`; `test.run(commands=[...])` is the inline alternative — both require `image=`
- `service.run(image="...")` starts a detached container — `image=` is required; `commands=` is accepted here too
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
