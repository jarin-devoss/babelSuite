---
title: Examples
---

# Examples

[Back to index](index.md)

## Example Suites

The example suites under [`examples/oci-suites/`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites) each end **Healthy** because they test something: a service the suite ships, a mock the service calls, and tests that fail when the service is wrong. Every README names the one line that, when broken, turns the suite red.

They are ordered from the simplest to the most composed. Two profiles are *meant* to fail: `electronics-lab` under `broken.yaml` and `kafka-consumer-health` under `lagging.yaml`.

| Suite | What it shows |
|-------|---------------|
| [`gateway-check`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/gateway-check) | One mock and one test step: the smallest complete package. It asserts that `BABELSUITE_GATEWAY_URL` reaches a step and that the sidecar answers from inside a step container. |
| [`payment-suite`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/payment-suite) | `service.run(file=)` — a payment service the suite ships and tests; `mock.api` for the card network it calls; a `<name>-ready` wait step; `test.run` that replays the contract; `traffic.baseline/stress`; `reset_mocks=`; `fail_on_logs=`; profile `extendsId` and `secretRefs`; `log.warn`. |
| [`identity-broker`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/identity-broker) | A pre-hook (`pre-hook.star`) that starts and gates on a session store and an upstream; `task.run`; one mock per identity provider; profile `env:` read through `env.get()`; `traffic.baseline`; `fail_on_logs=`. |
| [`storefront-browser-lab`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/storefront-browser-lab) | A web shop tested in real browsers: `browser_test(...)` from `@babelsuite/playwright` runs `@playwright/test` specs once per browser; a Node `api.test.ts`; `exports=` with `format="ctrf"`; `traffic.soak`. |
| [`returns-control-plane`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/returns-control-plane) | A gRPC mock (the pricing service, served from the suite's `.proto` and called over gRPC-Web) next to a REST mock; `traffic.stress/spike`; `on_failure=`; log `{{ }}` templates. |
| [`soap-claims-hub`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/soap-claims-hub) | SOAP mocks (one per claim type) chosen by `SOAPAction`, a REST bridge that builds SOAP 1.1 and 1.2 envelopes, a Fault the bridge must report as a failure, and `traffic.baseline` per SOAP version. |
| [`fleet-control-room`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/fleet-control-room) | A dispatcher that asks a geo-fence mock and a gRPC route-planner mock; `mock.api` with body-dispatched examples and a fallback; all four traffic phases (`baseline`, `spike`, `soak`); one acceptance step per region. |
| [`electronics-lab`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/electronics-lab) | `load("@plugins/...")` steps: SPICE, Verilog and control-system simulators that are mocks answering by the design they are sent. The `broken.yaml` profile sends bad designs and the run must end Failed. |
| [`kafka-consumer-health`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/kafka-consumer-health) | The `@babelsuite/kafka` module (a real broker, a topic) and `@babelsuite/kafka-checks`, which loads the `consumer-lag` plugin; a producer, a consumer group and a REST proxy. The `lagging.yaml` profile leaves a lag the check must catch. |
| [`notification-hub`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/notification-hub) | `suite.run` of two other suites (identity-broker vouches for callers, payment-suite is the source of payment events), one provider mock per channel, a dead-letter rule, and rate limiting. |
| [`composite-readiness`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/composite-readiness) | `suite.run` of three suites and a readiness check of each imported service by name. A name in `SUITES_TO_CHECK` that is not imported stops the suite at evaluation. |
| [`security-suite`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/security-suite) | All eight `security.*` modes scanning a real in-suite service (`target=`), reports read back by a gate task, and a `devices:` entry in a profile. |
| [`plugin-span-demo`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/plugin-span-demo) | Plugin span attributes on the `runner.run` trace, backed entirely by CUE mock exchanges — starts no containers. |
| [`ai-skill-eval`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/ai-skill-eval) | An agent skill graded by a judge: the judge's model is a mock. |
| [`llm-judge-live`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/llm-judge-live) | A real model as a suite step, a gating pull task, and `services.<name>.memory:`. |
| [`ai-skill-eval-live`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/ai-skill-eval-live) | The same skill evaluation against a real model. |

### The pattern the suites share

- The **service under test** is a small program in `tasks/`, started with `service.run(file=...)`. It reaches the things it calls by name (`http://stripe-mock:9080`).
- Each mock is a `mock.api(name=...)` node. Its examples answer by what the request says (`dispatch:`), and its `fallback` answers everything else, so a wrong request gets the wrong answer.
- A `<name>-ready` step waits for the service's `/health` before anything uses it.
- A step's container receives only the file named by `file=`, so each test script carries its own small helper.
- A test writes its JUnit report from what ran, never a fixed one: a static check fails the build otherwise.

### Feature quick-reference

| Feature | Where |
|---------|-------|
| `service.run(file=)`, `test.run`, `mock.api`, a `<name>-ready` wait step | every suite that ships a service |
| `env.get()` reading the launch profile's `env:` | payment-suite, identity-broker, electronics-lab, kafka-consumer-health |
| `task.run`, a pre-hook | identity-broker |
| `suite.run`, `dependencies.yaml` | notification-hub, composite-readiness |
| `fail_on_logs=` | payment-suite, identity-broker, soap-claims-hub, fleet-control-room, storefront-browser-lab |
| `reset_mocks=`, `secretRefs`, profile `extendsId` | payment-suite |
| gRPC mock (`.proto`, gRPC-Web) | returns-control-plane, fleet-control-room |
| `on_failure=` | returns-control-plane |
| `browser_test(...)`, `format="ctrf"` | storefront-browser-lab |
| `traffic.baseline`, `stress`, `spike`, `soak` | payment-suite, returns-control-plane, fleet-control-room, storefront-browser-lab |
| `load("@plugins/...")` plugin steps | electronics-lab, kafka-consumer-health (through a module) |
| `@babelsuite/kafka`, `@babelsuite/kafka-checks` | kafka-consumer-health |
| All 8 `security.*` modes, `target=` | security-suite |

An imported suite's HTTP and SOAP mocks are served on the importing run's gateway, so a step can call them by node name. Its gRPC mocks are not: a suite that calls a gRPC mock (`returns-control-plane`, `fleet-control-room`) is run on its own.

## Example Plugins

The example plugin packages under [`examples/oci-plugins/`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-plugins) show how to author, package, and register Lua plugins.

The built-in `traffic.*` and `security.*` nodes already cover load generation and HTTP-layer security scanning (fuzz, headers, CORS, auth bypass, GraphQL, verbs, flood). Example plugins should cover things those built-ins don't — domain-specific simulation, application-level token security, event-streaming infrastructure, and deployment validation.

| Plugin | Operations | Domain | Description |
|--------|------------|--------|-------------|
| [`spice-sim`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-plugins/spice-sim) | `transient`, `dc_sweep`, `ac_analysis` | Hardware simulation | Analog circuit simulation via a SPICE backend. Checks rise time, voltage bounds, and frequency response. |
| [`verilog-sim`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-plugins/verilog-sim) | `simulate`, `strict`, `lint`, `check_coverage` | Hardware simulation | Digital logic simulation via an Icarus Verilog backend. Checks compile success, assertion counts, lint warnings and toggle coverage. |
| [`control-sim`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-plugins/control-sim) | `step_response`, `monitor`, `impulse_response`, `frequency_response` | Hardware simulation | Control-system simulation via a scipy backend. Checks settling time, overshoot, DC gain, impulse decay, and stability margins. |
| [`jwt-auditor`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-plugins/jwt-auditor) | `check_token`, `audit_endpoint`, `check_claims`, `audit_endpoints` | Application security | JWT token auditing: algorithm strength, claim completeness, lifetime, issuer/audience/scope checks, and endpoint signature enforcement. Not covered by the built-in attack scanner. |
| [`consumer-lag`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-plugins/consumer-lag) | `check_lag`, `watch_lag`, `wait_caught_up`, `check_trend` | Event streaming | Kafka consumer group lag check via REST Proxy, with a drain-wait and a growth-trend mode. |
| [`dlq-inspector`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-plugins/dlq-inspector) | `inspect`, `watch_dlq`, `check_growth`, `inspect_topics` | Event streaming | Kafka DLQ message count check via REST Proxy, over one topic or many, with a growth-between-samples mode. |
| [`schema-compat`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-plugins/schema-compat) | `check_compat`, `enforce`, `check_registered`, `check_mode` | Event streaming | Avro schema compatibility check against a Confluent Schema Registry, plus registration and enforced-mode checks. |
| [`canary-validator`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-plugins/canary-validator) | `validate`, `watch_ratio`, `sticky`, `compare_errors` | Deployment validation | Validates a canary's traffic split ratio, session stickiness, and error-rate parity against stable. |
| [`pii-scanner`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-plugins/pii-scanner) | `scan`, `probe`, `scan_headers`, `scan_many` | Data quality | Scans API response bodies and headers for PII leakage (credit card, SSN, email, private key patterns), over one target or many. |
| [`shadow-diff`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-plugins/shadow-diff) | `diff`, `check`, `compare_paths`, `compare_latency` | Deployment validation | Deep JSON diff of primary vs shadow service responses for migration validation, across many paths, plus a latency-ratio check. |
| [`llm-judge`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-plugins/llm-judge) | `judge`, `monitor`, `consistency`, `compare` | Output grading | Grades text against a list of criteria using any OpenAI-compatible chat-completions endpoint, scoring the fraction satisfied; also measures judgement stability and compares two candidates. |

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

Add `--sync http://localhost:8090` (with `ADMIN_EMAIL` and `ADMIN_PASSWORD` in the
environment) to make the running backend re-read its registries, so it finds the
push without a restart. `GET /api/v1/suites/<id>` lists the package's `sourceFiles`,
which is the quickest way to confirm an edit actually reached the backend. A module
the backend also embeds (`@babelsuite/kafka`, for one) is read from the registry
when a suite loads it, so push the module as well (`--suite kafka`).

A run launched while a sync re-reads the registries resolves against the
catalog as it was before the push; one launched after the sync returns sees the
pushed package. The catalog is never empty in between.

## Why The Examples Matter

The examples act as runnable reference suites, UI inspection data, catalog enrichment sources, and authoring guides for new suites and modules.
