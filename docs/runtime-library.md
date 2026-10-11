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
    return [service.run(name=prefix + str(i)) for i in range(count)]
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

!!! warning "`env.get()` does not read the profile"
    The `env` global is an empty, frozen mapping during evaluation, so
    `env.get("NAME", "fallback")` always returns the fallback — in every
    profile, with no way to populate it.

    The consequence is about **graph shape**, and it is easy to miss. A loop or
    conditional over an `env.get()` value is decided once, at author time:

    ```python
    BROWSERS = env.get("BROWSERS", "chromium").split(",")   # always ["chromium"]

    for b in BROWSERS:              # always one iteration
        test.run(name="checkout-" + b, ...)

    if len(BROWSERS) > 1:           # always false
        log.error("multi-browser run")
    ```

    A profile setting `BROWSERS: chromium,firefox,webkit` changes none of that.
    So a profile can change **what a step does**, never **which steps exist**.

    What does read the active profile:

    - a step's `env=`, profile `env:` and `services.<step>.env:` — injected into
      the container, so your script reads them normally
    - a dependency's `inputs:` — injected into the imported suite's steps
    - `{{ env.NAME }}` inside a `log.*` message — resolved at runtime

    Treat `env.get()` as a named constant with a default. If a profile must
    drive something, have a step read it as an environment variable at runtime
    rather than branching on it here.

## Core Families

### `service`

Long-lived background infrastructure in the suite graph.

| Call | Kind |
|------|------|
| `service.run` | `service` |

```python
db  = service.run()
api = service.run(after=[db])
```

Use `service.run` for first-party background dependencies such as databases, APIs, caches, and workers.

When `image=` is provided, `service.run` starts a real Docker container and runs it **detached** — the node signals healthy as soon as the container is up, then keeps it running in the background while downstream nodes proceed. The container is torn down when the execution ends.

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

### `service.mock`

Native suite-defined mock surfaces.

| Call | Kind |
|------|------|
| `service.mock` | `mock` |

```python
orders = service.mock(after=[api])
```

`service.mock` is the runtime entrypoint for mocks backed by the suite's `api/` and `mock/` folders.

| Argument | Used for |
|----------|---------|
| `harden=True` | opts the APISIX sidecar into gateway-provided defenses — see [Hardening and `security.*` checks](#hardening-and-security-checks) below |

Compatibility aliases still parse:

- `mock.serve`

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

#### Hardening and `security.*` checks

By default the gateway adds nothing on top of your backend: `security.headers` and `security.flood` observe whatever your real API actually returns, so a backend that's genuinely missing security headers or rate-limiting will genuinely fail those checks.

Pass `harden=True` to `service.mock(...)` to opt the sidecar into two gateway-provided defenses instead of relying on the backend for them:

- a baseline set of security response headers (HSTS, CSP, X-Frame-Options, X-Content-Type-Options, Referrer-Policy, Permissions-Policy) applied to every response, including unmatched-route 404s
- a synthetic rate-limited route at `/api/v1/resource` (HTTP 429 after 20 requests/second) for `security.flood` to validate against when the suite has no rate-limited surface of its own

This is opt-in and suite-scoped — a suite that doesn't set `harden=True` gets none of this, so `headers`/`flood` checks stay genuine signals about that suite's own backend.

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

spice_mock = service.mock(name="spice-service")
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
- the local (Docker) runner bind-mounts the shared execution workspace into every container and reads each declared export path from it after the container exits, so a step writes `reports/junit.xml` relative to its working directory
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
| `BABELSUITE_GATEWAY_URL` | the APISIX sidecar address this step can reach, when the suite has a mock surface |
| `BABELSUITE_GATEWAY_URLS` | comma-separated address per mock node, in topology order |
| `BABELSUITE_ARTIFACTS_DIR` | Kubernetes backend only — path to the artifacts sidecar mount |

`BABELSUITE_GATEWAY_URL` is how a `task.run` or `test.run` reaches a mock
surface without hardcoding a host and port. It is set only when the suite has
one, so a step can tell the difference between "no gateway" and "wrong address".

!!! note "`BABELSUITE_ARTIFACTS_DIR` is Kubernetes-only"
    The Docker backend does not set it. There, `.export(...)` paths are read
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

Hardware, memory and networking are declared in the **profile YAML**, not in `suite.star`. This keeps the suite portable — the same topology runs on a laptop (no GPU, default bridge, modest memory) and a GPU cluster (with devices, execution network, a larger ceiling) by switching profiles.

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

### `network.mode`

```yaml
# staging.yaml
network:
  mode: execution   # isolated bridge — containers reach each other by node name
```

Every container in the execution joins a shared Docker bridge network, and the node's `name` becomes its DNS hostname — a `service.run(name="redis")` is reachable at `redis:6379` from any `task.run` or `test.run` in the same execution. No `suite.star` changes required.

!!! warning "`network.mode` is read but not yet acted on"
    The Docker backend creates and joins the per-execution network
    unconditionally, because the shipped modules address other steps by name in
    every profile — `connect()` dials `<db name>:5432` whether or not a profile
    opted in. `host` and `none` are parsed and stored but change nothing today,
    so treat the three values as the intended contract rather than working
    configuration.

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
- backslash continuations are supported for chained `.export(...)` calls
- the left-hand assignment becomes the default node ID
- use `after=[db, api]` to declare ordering; omitting it means the node has no dependencies
- use `on_failure=[primary]` for rollback or contingency branches
- quoted `after=["db"]` still parses, but identifier references are the preferred style
- `task.run(file="...")` resolves from `tasks/`; `task.run(commands=[...])` is the inline alternative — both require `image=`
- `test.run(file="...")` resolves from `tests/`; `test.run(commands=[...])` is the inline alternative — both require `image=`
- `service.run(image="...")` starts a detached container; `commands=` is accepted here too
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
