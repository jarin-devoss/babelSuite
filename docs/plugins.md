---
title: Plugins
---

# Plugins

[Back to index](index.md)

Plugins are Lua functions that run inside the APISIX sidecar — no extra container, no image pull. BabelSuite ships two built-in plugins: `babelsuite-traffic-cannon` (all `traffic.*` nodes) and `babelsuite-attack-scanner` (all `security.*` nodes). User plugins follow exactly the same dispatch pattern and run in the same sidecar process — they are just Lua code registered in `configuration.yaml` instead of being bundled with BabelSuite.

## Plugins vs Modules

There are actually **three kinds of topology nodes** in BabelSuite, not two:

| | Modules (`@babelsuite/kafka`, …) | Built-in plugins (`traffic.*`, `security.*`) | User plugins (`@plugins/spice-sim`, …) |
|---|---|---|---|
| **What it is** | A Starlark package that returns topology nodes | Lua code bundled with BabelSuite, running inside the APISIX sidecar | Lua code you write and register, running in the same sidecar |
| **Where the code runs** | Parsed at suite load time — wires up Docker containers | Inside the APISIX process at execution time — no container | Same as built-in plugins — inside APISIX |
| **How it's delivered** | OCI artifact pulled from a registry | Bundled with BabelSuite — always available | Lua source registered in `configuration.yaml` |
| **What it produces** | `service`, `task`, `test` nodes — full containers | A lightweight step: POST config → read JSON verdict | Same pattern as built-in plugins |
| **Startup cost** | Container pull + boot | Zero — sidecar already running | Zero — same sidecar |
| **Use it when** | Spin up infrastructure: broker, database, cache | Load test or security scan a running service | Custom verification: domain-specific checks, external simulators |

`traffic.*` and `security.*` **are plugins** — they are Lua code (`babelsuite-traffic-cannon` and `babelsuite-attack-scanner`) that runs inside the APISIX sidecar, exactly like a user plugin. The only difference is they ship bundled with BabelSuite instead of being registered manually. User plugins follow the exact same dispatch pattern: the backend POSTs a config payload to the sidecar, and reads `passed` / `findings` / `summary` / `level` back.

**Rule of thumb:** if the answer to "does my service need this running while tests execute?" is yes — it's a module. If the answer is "does my pipeline need to verify or probe something?" — it's a plugin (built-in or user-defined).

Examples:
- `@babelsuite/kafka` is a **module** — starts a Kafka broker your services publish to
- `traffic.baseline(target=...)` is a **built-in plugin** — runs load against a service via `babelsuite-traffic-cannon` in the sidecar
- `security.fuzz(target=...)` is a **built-in plugin** — fuzzes endpoints via `babelsuite-attack-scanner` in the sidecar
- `@plugins/consumer-lag` is a **user plugin** — asserts the consumer group has caught up after the test
- `@plugins/pii-scanner` is a **user plugin** — scans your API's response for PII

## How It Works

1. Register a plugin via the API or UI (`Settings → Plugins`). The plugin's Lua source is embedded into the settings file.
2. When a suite that references the plugin runs, BabelSuite generates an `apisix.yaml` for the sidecar that includes the plugin's Lua code as a serverless function at the registered trigger path.
3. The APISIX sidecar starts once per (suite, profile) pair and stays up across all executions of that combination.
4. For each `plugin.run()` step the backend POSTs the step config to the trigger URL on the sidecar and reads the JSON response back.

## Using a Plugin in a Suite

Plugins are loaded with `load("@plugins/<name>", "<operation>")`. Each exported name is a function that creates a topology node.

```python
load("@plugins/spice-sim",   "transient", "dc_sweep")
load("@plugins/verilog-sim", "simulate")
load("@plugins/control-sim", "monitor")

spice_mock   = service.mock(name="spice-service")
verilog_mock = service.mock(name="verilog-service")
control_mock = service.mock(name="control-service")

rc_filter = transient(
    name        = "rc-filter-step-response",
    netlist     = "RC Filter\n...",
    probe_node  = "2",
    max_rise_ms = 3.0,
    max_voltage = 5.5,
    after       = [spice_mock],
)

counter_check = simulate(
    name       = "4bit-counter-correctness",
    top_module = "tb_counter",
    verilog    = "...",
    testbench  = "...",
    timeout_ns = 200,
    after      = [verilog_mock],
)

damper_watch = monitor(
    name        = "mass-spring-damper-response",
    numerator   = [1],
    denominator = [1, 2, 1],
    time_end    = 10,
    after       = [control_mock],
)
```

Plugin nodes accept `after=`, `continue_on_failure=`, and `on_failure=` just like any other topology node.

## What User Plugins Should NOT Duplicate

Before writing a user plugin, check whether the built-in plugins already cover it:

| If you need to… | Use built-in… | NOT a user plugin |
|---|---|---|
| Load-test an endpoint | `traffic.baseline`, `traffic.stress`, `traffic.spike`, … | — |
| Check for SQLi / XSS / path traversal | `security.fuzz` | — |
| Verify auth bypass / open endpoints | `security.auth`, `security.probe` | — |
| Audit security response headers | `security.headers` | — |
| Find CORS misconfigurations | `security.cors` | — |
| Test rate limiting | `security.flood` | — |
| Detect exposed GraphQL introspection | `security.graphql` | — |
| Test dangerous HTTP verbs | `security.verbs` | — |

User plugins are the right tool when the check is **domain-specific** and not covered by the built-ins: hardware simulation, token/claim auditing, schema registry compatibility, Kafka infrastructure assertions, database state checks, etc.

## Example Plugins

Ten plugins ship under `examples/oci-plugins/`. They cover four domains: hardware simulation, application security, event-streaming health, and deployment validation.

---

### `spice-sim` — Analog Circuit Simulation

POSTs a SPICE netlist to a simulation backend and checks the resulting waveform against threshold rules. Useful for electronics teams that run automated analog circuit checks as part of a CI pipeline.

| Operation | What it does |
|-----------|--------------|
| `transient` | Runs a time-domain simulation. Checks peak voltage, minimum voltage, and rise time (10%→90%) against configured limits. |
| `dc_sweep` | Runs a DC operating-point sweep. Checks peak and minimum voltage at the probe node. |
| `ac_analysis` | Runs a small-signal AC analysis. Checks the gain in dB at the probe node against a minimum threshold. |

Required config: `sim_url`, `netlist`. Optional: `probe_node`, `max_voltage`, `min_voltage`, `max_rise_ms`, `min_gain_db`, `severity`.

---

### `verilog-sim` — Digital Logic Simulation

Sends Verilog source and a testbench to an Icarus-compatible simulation backend and inspects the results for compile errors, runtime assertion failures, and undefined (X-state) signals.

| Operation | What it does |
|-----------|--------------|
| `simulate` | Runs the testbench. Fails if compile errors exceed zero or simulation errors exceed `max_errors`. X-state assertion is configurable via `assert_no_x`. |
| `strict` | Same as `simulate` but zero-tolerance: any X-state signal, any compile error, any simulation error fails the step immediately. |

Required config: `sim_url`, `top_module`. Optional: `verilog`, `testbench`, `timeout_ns`, `max_errors`, `assert_no_x`, `severity`.

---

### `control-sim` — Control System Simulation

Sends a transfer function (numerator/denominator polynomial coefficients) to a scipy-backed simulation service and checks stability and transient response metrics.

| Operation | What it does |
|-----------|--------------|
| `step_response` | Runs a step response simulation. Checks stability (poles in right-half plane), settling time, overshoot percentage, and DC gain. Fails the step on violation. |
| `monitor` | Same checks, but always emits `warn`-level findings — never blocks the suite. Use this for passive observability of control system health. |

Required config: `sim_url`, `numerator`, `denominator`. Optional: `time_end`, `max_settling_time`, `max_overshoot_pct`, `min_dc_gain`, `max_dc_gain`, `severity`.

---

### `jwt-auditor` — JWT Token and Endpoint Security

Application-level JWT security checks. The built-in `security.*` modes check HTTP-layer concerns (headers, CORS, verbs, fuzz payloads). `jwt-auditor` checks what's *inside* the token and whether the endpoint actually enforces signature validation — neither of which the attack scanner covers.

| Operation | What it does |
|-----------|--------------|
| `check_token` | POSTs credentials to `auth_url`, extracts the JWT from the response, decodes the header and payload, and audits: algorithm (`none` is critical, HMAC symmetric is a warning), missing `exp` claim, token lifetime exceeding `max_lifetime_hours`, and missing required claims (`sub`, `iss`, `exp`, `iat` by default). |
| `audit_endpoint` | Gets a valid token first, then probes `endpoint` three ways: no Authorization header (must be 401/403), tampered signature (must be 401/403), and valid token (must be 2xx). Flags any case where the endpoint accepts what it shouldn't. |

Required config: `auth_url`, `username`, `password`. Optional: `token_field` (default `token`), `endpoint` (required for `audit_endpoint`), `max_lifetime_hours` (default `24`), `required_claims`, `severity`.

---

### `canary-validator` — Canary Deployment Traffic Ratio

> **Note:** this plugin makes HTTP requests to check traffic routing. The built-in `traffic.*` nodes handle load generation — this plugin is specifically for validating that the *split ratio* itself is correct, which is a different concern.

Sends N requests to a target URL and counts how many responses carry a canary header. Checks that the observed ratio matches the expected split within a tolerance band. Useful for validating that a canary deployment is receiving exactly the right percentage of traffic.

| Operation | What it does |
|-----------|--------------|
| `validate` | Checks the observed canary ratio against `expected_ratio ± tolerance`. Fails if out of range. |
| `watch_ratio` | Same check but with a wider default tolerance and `warn`-level findings — passive monitoring mode. |

Required config: `target`. Optional: `canary_header` (default `X-Canary`), `expected_ratio` (default `0.1`), `tolerance` (default `0.05`), `sample_size` (default `100`), `severity`.

---

### `consumer-lag` — Kafka Consumer Group Lag

Queries a Kafka REST Proxy for a consumer group's partition offsets and checks whether any partition's lag exceeds a threshold. Useful after a load test or data ingestion task to confirm the consumer has fully caught up.

| Operation | What it does |
|-----------|--------------|
| `check_lag` | Checks every partition in the group. Fails with `critical` if any partition lag exceeds `max_lag`. |
| `watch_lag` | Same check but always `warn`-level — use for continuous observability. |

Required config: `kafka_rest_url`, `group`. Optional: `max_lag` (default `1000`), `severity`.

---

### `dlq-inspector` — Dead Letter Queue Inspector

Checks the latest offset of a Kafka DLQ topic via the REST Proxy. A non-zero (or above-threshold) offset means messages were dead-lettered during the test run.

| Operation | What it does |
|-----------|--------------|
| `inspect` | Fails the step if the DLQ topic has more messages than `max_messages`. |
| `watch_dlq` | Same check but always `warn`-level — never blocks the suite. |

Required config: `kafka_rest_url`, `topic`. Optional: `max_messages` (default `0`), `severity`.

---

### `pii-scanner` — PII Leak Detection

> **Note:** this plugin makes an HTTP GET to a URL and scans the body. The built-in `security.probe` checks for *access control* on sensitive paths — `pii-scanner` checks for *data leakage* in the response body, which is a different concern.

Fetches a URL and scans the response body for PII patterns using regex. Ships with built-in patterns for credit card numbers, SSNs, email addresses, and private keys. Additional patterns can be provided per-step.

| Operation | What it does |
|-----------|--------------|
| `scan` | Fetches `target` and fails if any PII pattern matches. Use this as a hard gate in your pipeline. |
| `probe` | Same scan but always `warn`-level — passive auditing mode. |

Required config: `target`. Optional: `patterns` (list of extra regex strings), `severity`.

---

### `schema-compat` — Avro Schema Compatibility

Posts a new schema to a Confluent-compatible Schema Registry compatibility endpoint and checks whether it is compatible with the current version for a given subject.

| Operation | What it does |
|-----------|--------------|
| `check_compat` | Checks compatibility using the mode in `mode` (`BACKWARD`, `FORWARD`, `FULL`, etc.). |
| `enforce` | Always checks `FULL` compatibility (both backward and forward). Use this as a hard gate before publishing a new schema version. |

Required config: `registry_url`, `subject`, `new_schema`. Optional: `mode` (default `BACKWARD`), `severity`.

---

### `shadow-diff` — Shadow Traffic Response Diffing

> **Note:** this plugin makes HTTP requests to two targets. The built-in `traffic.*` nodes generate load — `shadow-diff` is for *migration validation*, comparing the response payloads of an old and new implementation, which traffic nodes don't do.

Fetches the same endpoint from two targets — a primary and a shadow — and compares the JSON responses field by field. Useful for validating a new service version before switching traffic.

| Operation | What it does |
|-----------|--------------|
| `diff` | Fails the step if the number of field-level differences exceeds `threshold`. Useful as a hard gate during migration. |
| `check` | Same diff but always `warn`-level — passive shadow monitoring. |

Required config: `primary`, `shadow`. Optional: `ignore_fields` (list of field paths to skip), `threshold` (default `0`), `severity`.

---

## Plugin Fields

| Field | Description |
|-------|-------------|
| `name` | Unique identifier — used as the load target in `suite.star` (e.g. `@plugins/spice-sim`) |
| `trigger` | APISIX route path the plugin listens on (e.g. `/_babelsuite/plugins/spice-sim/start`) |
| `version` | Semantic version string stored as-is — e.g. `1.0.0`. Not auto-bumped by the API. |
| `kind` | Must be `plugin` |
| `variants` | List of variant names the plugin registers as APISIX Lua extensions |
| `operations` | Optional list of allowed operation names — the suite call must match one |
| `lua` | Full Lua source embedded verbatim into the generated `apisix.yaml` |
| `schema` | Optional CUE schema; step config is validated against it before dispatch |
| `star` | Starlark wrapper source — defines the typed functions suite authors call via `load("@plugins/<name>", ...)`. See [Plugin Starlark Wrapper](#plugin-starlark-wrapper-pluginstar). |
| `deprecated` | When `true`, the plugin is flagged in health checks but still dispatched |

## Plugin Response Contract

The Lua plugin's HTTP handler must return a JSON body. BabelSuite reads these fields:

| Field | Type | Description |
|-------|------|-------------|
| `passed` | bool | Whether the step should be considered successful |
| `findings` | array | List of finding objects — each should have at least `message` |
| `summary` | string | One-line summary emitted as a log line at the level set by `level` |
| `level` | string | Log level for the summary: `info`, `debug`, `warn`, or `error`. Defaults to `info` if omitted. |
| `stderr` | string | Optional raw stderr from the plugin process, shown in the execution log |

Minimal compliant Lua return:

```lua
local ok = #findings == 0
return {
    passed   = ok,
    findings = findings,
    summary  = ok and "All checks passed." or string.format("%d finding(s).", #findings),
    level    = ok and "info" or "warn",
}, nil
```

## API

All plugin endpoints require an admin session except `check` and `validate`.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/platform-settings/plugins` | List all registered plugins |
| `POST` | `/api/v1/platform-settings/plugins` | Register a new plugin |
| `PUT` | `/api/v1/platform-settings/plugins/{name}` | Update an existing plugin in-place |
| `DELETE` | `/api/v1/platform-settings/plugins/{name}` | Remove a plugin |
| `GET` | `/api/v1/platform-settings/plugins/{name}/check` | Static health check: validates trigger path and CUE schema |
| `POST` | `/api/v1/platform-settings/plugins/{name}/validate` | Validate a step config object against the plugin's CUE schema |

### Register

```bash
curl -s -X POST http://localhost:8090/api/v1/platform-settings/plugins \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name":       "spice-sim",
    "version":    "1.0.0",
    "trigger":    "/_babelsuite/plugins/spice-sim/start",
    "kind":       "plugin",
    "variants":   ["spice-sim"],
    "operations": ["transient", "dc_sweep", "ac_analysis"],
    "lua":        "<lua source>",
    "schema":     "<cue schema>"
  }'
```

### Update in-place

```bash
curl -s -X PUT http://localhost:8090/api/v1/platform-settings/plugins/spice-sim \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{ "trigger": "/_babelsuite/plugins/spice-sim/start", "lua": "<updated lua>", ... }'
```

The plugin name is taken from the URL — the `name` field in the body is ignored.

## APISIX Sidecar Lifecycle

Each unique (suite, profile) pair gets one long-lived APISIX sidecar container. The sidecar starts on the first execution of that pair and remains up across all subsequent executions. Plugin Lua code is embedded into the sidecar's `apisix.yaml` at startup — removing the sidecar container forces it to restart with the latest plugin code on the next execution.

The sidecar runs in **standalone mode**: `deployment.role: data_plane` with `config_provider: yaml`. No etcd is involved.

## Using Plugins Inside Modules

Modules can load and re-export plugins just like they load `@babelsuite/runtime`. The `@plugins/<name>` path is resolved through the same mechanism inside module `.star` files, so a module can bundle both infrastructure setup **and** plugin-backed verification helpers in one package.

```python
# examples/oci-modules/kafka/admin.star
load("@babelsuite/runtime",    "task")
load("@plugins/consumer-lag",  "check_lag")
load("@plugins/dlq-inspector", "inspect")

def check_consumer_health(broker, group, topic, kafka_rest_url,
                          max_lag = 1000, after = []):
    lag = check_lag(
        name             = broker.name + "-lag-" + group,
        kafka_rest_url   = kafka_rest_url,
        group            = group,
        max_lag          = max_lag,
        after            = [broker] + after,
    )
    dlq = inspect(
        name           = broker.name + "-dlq-" + topic,
        kafka_rest_url = kafka_rest_url,
        topic          = topic + ".DLQ",
        max_messages   = 0,
        after          = [lag],
    )
    return lag, dlq
```

A suite then gets both the infrastructure and the verification checks from a single module load:

```python
load("@babelsuite/kafka", "kafka", "create_topic", "check_consumer_health")

broker  = kafka()
topic   = create_topic(broker, "orders", partitions=3)
app     = service.run(after=[topic])
lag, dlq = check_consumer_health(
    broker,
    group          = "orders-consumer",
    topic          = "orders",
    kafka_rest_url = "http://kafka:8082",
    after          = [app],
)
```

The plugins referenced inside a module must still be registered in platform settings before any suite that loads the module can run — the module provides the Starlark API, but the Lua code runs in the APISIX sidecar at execution time.

## Plugin Starlark Wrapper (`plugin.star`)

Every plugin ships a `plugin.star` file alongside its Lua source. This file is the **Starlark API surface** of the plugin — it defines the named functions that suite authors call in `suite.star`, with typed parameters and defaults, and forwards them to the underlying plugin dispatcher.

Without `plugin.star`, a suite would have to call the plugin with raw keyword arguments and remember every field name and default. With it, the suite just calls `transient(name=..., netlist=..., max_rise_ms=3.0)` — the wrapper handles the rest.

### Structure

```python
load("@babelsuite/runtime", "plugin")

_ref = plugin("spice-sim")          # bind to the registered plugin by name

def transient(name, netlist,         # typed, documented parameters
              probe_node = "out",
              max_rise_ms = None,
              max_voltage = None,
              sim_url = "",
              severity = "critical",
              after = []):
    return _ref.transient(           # forward to the dispatcher
        name        = name,
        after       = after,
        netlist     = netlist,
        probe_node  = probe_node,
        max_rise_ms = max_rise_ms,
        max_voltage = max_voltage,
        sim_url     = sim_url,
        severity    = severity,
    )
```

Each exported function corresponds to one operation in the plugin's `operations` list. The function name must match the operation name exactly — that is how the backend knows which operation to pass to the Lua handler.

### How it's used in a suite

```python
load("@plugins/spice-sim", "transient", "dc_sweep")

rc_filter = transient(
    name        = "rc-filter",
    netlist     = "...",
    probe_node  = "2",
    max_rise_ms = 3.0,
    after       = [spice_mock],
)
```

The `load("@plugins/spice-sim", "transient")` resolves the `plugin.star` file for `spice-sim` and imports the `transient` function from it.

### Relationship to `plugin.yaml`

The `plugin.yaml` field `star: plugin.star` tells BabelSuite where the Starlark wrapper lives. When the registration API receives it (via the `star` field on `CustomPlugin`), the content is stored alongside the Lua source and served to suite authors via the `@plugins/<name>` load path.

### Writing a `plugin.star`

1. Load `plugin` from `@babelsuite/runtime` and bind it to your plugin name.
2. Define one function per operation. Parameter names must match the config keys your Lua handler reads from `payload.config`.
3. Always forward `name=` and `after=` — these are topology fields, not config fields.
4. Set sensible defaults so callers only have to specify what they care about.

```python
load("@babelsuite/runtime", "plugin")

_ref = plugin("my-checker")

def check(name, target, threshold = 100, severity = "critical", after = []):
    return _ref.check(
        name      = name,
        after     = after,
        target    = target,
        threshold = threshold,
        severity  = severity,
    )
```

## Plugin Definition Files

The example plugins under [`examples/oci-plugins/`](examples.md#example-plugins) each ship four files:

| File | Purpose |
|------|---------|
| `plugin.yaml` | Manifest — name, version, trigger, operations, and references to the other files |
| `plugin.lua` | Lua source embedded into the APISIX sidecar |
| `plugin.star` | Starlark wrapper — typed functions for `suite.star` authors |
| `schema.cue` | CUE schema for validating step config before dispatch |

`plugin.yaml` example:

```yaml
name: spice-sim
version: 1.0.0
trigger: /_babelsuite/plugins/spice-sim/start
kind: plugin
variants: [spice-sim]
operations: [transient, dc_sweep, ac_analysis]
schema: schema.cue
lua: plugin.lua
star: plugin.star
```

The `schema`, `lua`, and `star` fields are file references — the registration script reads them and sends the content inline to the API. Versions live in the definition file and are not auto-bumped by the backend.

## UI

Plugins are managed at **Settings → Plugins** (`/settings/plugins`). The page lists all registered plugins, shows their version and operations, and lets admins register, edit, or delete them.
