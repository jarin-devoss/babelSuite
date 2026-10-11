---
title: Examples
---

# Examples

[Back to index](index.md)

## Example Suites

The example suites under [`examples/oci-suites/`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites) are ordered from simplest to most complex. Each one introduces new features on top of the previous.

| Level | Suite | What it introduces |
|-------|-------|--------------------|
| 1 | [`notification-hub`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/notification-hub) | `service.run`, `test.run`, `commands=`, profile `env:`, `services.<name>.env:` |
| 2 | [`identity-broker`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/identity-broker) | `task.run`, `service.mock`, `log.info/debug`, `secretRefs`, `expect_exit=`, `expect_logs=`, `fail_on_logs=`, `continue_on_failure=` |
| 3 | [`payment-suite`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/payment-suite) | `file=`, conditionals, profile `extendsId`, `traffic.baseline/stress`, `reset_mocks=`, `log.warn`, `.export(cobertura)` |
| 4 | [`returns-control-plane`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/returns-control-plane) | `network.mode: execution`, `services.<name>.devices:`, log `{{ }}` templates, `on_failure=`, `traffic.stress/spike` |
| 5 | [`storefront-browser-lab`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/storefront-browser-lab) | Playwright specs, `.export(ctrf)`, `traffic.soak`, `reset_mocks=`, `on_failure=` |
| 6 | [`soap-claims-hub`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/soap-claims-hub) | All 8 `security.*` modes, `traffic.scalability`, hardware profile `/dev/ttyUSB0` |
| 7 | [`fleet-control-room`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/fleet-control-room) | OCI modules (kafka, redis), `service.run(image=, commands=)` detached, all traffic phases, all log levels, multi-region loops, GPU in perf profile |
| 8 | [`security-suite`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/security-suite) | Focused security reference — all 8 modes in sequence, hardware device profile |
| 9 | [`composite-readiness`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/composite-readiness) | `suite.run` cross-suite orchestration, all log levels, dynamic topology from dict |
| 10 | [`electronics-lab`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/electronics-lab) | Lua plugins via `load("@plugins/...")`: analog (SPICE), digital (Verilog), and control-system (scipy) verification via APISIX-hosted Lua handlers |
| 11 | [`plugin-span-demo`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/plugin-span-demo) | Plugin span attributes on the `runner.run` trace, backed entirely by CUE mock exchanges — starts no containers |
| 12 | [`llm-judge-live`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/llm-judge-live) | A real model as a suite step, a gating pull task, `services.<name>.memory:` |

Two more are not on the ladder because they exist to pin one thing each:
[`gateway-check`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/gateway-check)
is the smallest complete package — one mock, one test step — and asserts that
`BABELSUITE_GATEWAY_URL` reaches a step and that the sidecar answers from inside
a step container.
[`kafka-consumer-health`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/kafka-consumer-health)
shows a module that internally composes a plugin.

### Feature quick-reference

| Feature | First introduced |
|---------|-----------------|
| `service.run`, `test.run`, `commands=` | notification-hub |
| Profile `env:`, `services.<name>.env:` | notification-hub |
| `task.run`, `service.mock`, `log.info/debug` | identity-broker |
| `secretRefs`, `expect_exit=`, `expect_logs=`, `fail_on_logs=` | identity-broker |
| `continue_on_failure=` | identity-broker |
| `file=`, conditionals | payment-suite |
| Profile `extendsId`, `reset_mocks=`, `log.warn` | payment-suite |
| `traffic.baseline/stress`, `.export(cobertura)` | payment-suite |
| `network.mode: execution`, `services.<name>.devices:` | returns-control-plane |
| Log `{{ }}` templates, `on_failure=` | returns-control-plane |
| `traffic.spike` | returns-control-plane |
| Playwright specs via `browser_test(...)` | storefront-browser-lab |
| `.export(ctrf)`, `traffic.soak`, `log.error` | storefront-browser-lab |
| All 8 `security.*` modes | soap-claims-hub |
| `traffic.scalability`, hardware device profile | soap-claims-hub |
| OCI modules, `service.run(image=, commands=)` detached | fleet-control-room |
| `traffic.wave`, all log levels | fleet-control-room |
| `suite.run` | composite-readiness |
| `load("@plugins/...")` plugin steps | electronics-lab |

## Example Plugins

The example plugin packages under [`examples/oci-plugins/`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-plugins) show how to author, package, and register Lua plugins.

The built-in `traffic.*` and `security.*` nodes already cover load generation and HTTP-layer security scanning (fuzz, headers, CORS, auth bypass, GraphQL, verbs, flood). Example plugins should cover things those built-ins don't — domain-specific simulation, application-level token security, event-streaming infrastructure, and deployment validation.

| Plugin | Operations | Domain | Description |
|--------|------------|--------|-------------|
| [`spice-sim`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-plugins/spice-sim) | `transient`, `dc_sweep`, `ac_analysis` | Hardware simulation | Analog circuit simulation via a SPICE backend. Checks rise time, voltage bounds, and frequency response. |
| [`verilog-sim`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-plugins/verilog-sim) | `simulate`, `strict` | Hardware simulation | Digital logic simulation via an Icarus Verilog backend. Checks compile success and assertion counts. |
| [`control-sim`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-plugins/control-sim) | `step_response`, `monitor` | Hardware simulation | Control-system simulation via a scipy backend. Checks settling time, overshoot, and DC gain. |
| [`jwt-auditor`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-plugins/jwt-auditor) | `check_token`, `audit_endpoint` | Application security | JWT token auditing: algorithm strength, claim completeness, lifetime, and endpoint signature enforcement. Not covered by the built-in attack scanner. |
| [`consumer-lag`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-plugins/consumer-lag) | `check_lag`, `watch_lag` | Event streaming | Kafka consumer group lag check via REST Proxy. |
| [`dlq-inspector`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-plugins/dlq-inspector) | `inspect`, `watch_dlq` | Event streaming | Kafka DLQ message count check via REST Proxy. |
| [`schema-compat`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-plugins/schema-compat) | `check_compat`, `enforce` | Event streaming | Avro schema compatibility check against a Confluent Schema Registry. |
| [`canary-validator`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-plugins/canary-validator) | `validate`, `watch_ratio` | Deployment validation | Validates that a canary deployment receives the correct traffic split ratio. |
| [`pii-scanner`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-plugins/pii-scanner) | `scan`, `probe` | Data quality | Scans API response bodies for PII leakage (credit card, SSN, email, private key patterns). |
| [`shadow-diff`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-plugins/shadow-diff) | `diff`, `check` | Deployment validation | Deep JSON diff of primary vs shadow service responses for migration validation. |
| [`llm-judge`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-plugins/llm-judge) | `judge`, `monitor` | Output grading | Grades text against a list of criteria using any OpenAI-compatible chat-completions endpoint, scoring the fraction of criteria satisfied. |

Each plugin folder contains:

| File | Purpose |
|------|---------|
| `plugin.yaml` | Plugin manifest — name, version, operations, schema, lua, and star file references |
| `plugin.lua` | Lua source embedded into the APISIX sidecar |
| `plugin.star` | Starlark wrapper — the typed functions `suite.star` authors call |
| `schema.cue` | CUE schema for validating step config before dispatch |

To register a plugin from its manifest:

```bash
# read the lua source into the API payload
lua=$(cat examples/oci-plugins/spice-sim/plugin.lua)
schema=$(cat examples/oci-plugins/spice-sim/schema.cue)

curl -s -X POST http://localhost:8090/api/v1/platform-settings/plugins \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"name\":\"spice-sim\",\"version\":\"1.0.0\",\"kind\":\"plugin\",\"variants\":[\"spice-sim\"],\"operations\":[\"transient\",\"dc_sweep\",\"ac_analysis\"],\"lua\":$(echo "$lua" | jq -Rs .),\"schema\":$(echo "$schema" | jq -Rs .)}"
```

## Example Modules

The example module folders under [`examples/oci-modules/`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-modules):

| Module | Description |
|--------|-------------|
| [`kafka`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-modules/kafka) | Kafka broker cluster and topic admin helpers |
| [`postgres`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-modules/postgres) | Postgres cluster and query execution helpers |
| [`redis`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-modules/redis) | Redis cluster helpers |
| [`mongodb`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-modules/mongodb) | MongoDB cluster and collection helpers |
| [`playwright`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-modules/playwright) | Playwright browser test runner |

These are pure Starlark module examples built on top of the built-in runtime primitives.

## Publishing Example Content

The control plane serves suites from the OCI catalog, not from `examples/` on
disk, so editing a package has no effect on a run until it is pushed and the
catalog re-read:

```bash
python scripts/push-to-zot.py            # all suites, modules, and plugins
python scripts/push-to-zot.py --suite payment-suite
```

Then restart the backend — the catalog is read at startup. `GET /api/v1/suites/<id>`
lists the package's `sourceFiles`, which is the quickest way to confirm an edit
actually reached the backend.

## Why The Examples Matter

The examples act as runnable reference suites, UI inspection data, catalog enrichment sources, and authoring guides for new suites and modules.
