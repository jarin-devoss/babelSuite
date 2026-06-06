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
| 3 | [`payment-suite`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/payment-suite) | `file=`, `env.get()`, conditionals, profile `extendsId`, `traffic.baseline/stress`, `reset_mocks=`, `log.warn`, `.export(cobertura)` |
| 4 | [`returns-control-plane`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/returns-control-plane) | `network.mode: execution`, `services.<name>.devices:`, log `{{ }}` templates, `on_failure=`, `traffic.stress/spike` |
| 5 | [`storefront-browser-lab`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/storefront-browser-lab) | Playwright `browser()`, loops over browser matrix, `.export(ctrf)`, `traffic.soak`, `log.error` |
| 6 | [`soap-claims-hub`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/soap-claims-hub) | All 8 `security.*` modes, `traffic.scalability`, hardware profile `/dev/ttyUSB0` |
| 7 | [`fleet-control-room`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/fleet-control-room) | OCI modules (kafka, redis), `service.run(image=, commands=)` detached, all traffic phases, all log levels, multi-region loops, GPU in perf profile |
| 8 | [`security-suite`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/security-suite) | Focused security reference — all 8 modes in sequence, hardware device profile |
| 9 | [`composite-readiness`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/composite-readiness) | `suite.run` cross-suite orchestration, all log levels, dynamic topology from dict |

### Feature quick-reference

| Feature | First introduced |
|---------|-----------------|
| `service.run`, `test.run`, `commands=` | notification-hub |
| Profile `env:`, `services.<name>.env:` | notification-hub |
| `task.run`, `service.mock`, `log.info/debug` | identity-broker |
| `secretRefs`, `expect_exit=`, `expect_logs=`, `fail_on_logs=` | identity-broker |
| `continue_on_failure=` | identity-broker |
| `file=`, `env.get()`, conditionals | payment-suite |
| Profile `extendsId`, `reset_mocks=`, `log.warn` | payment-suite |
| `traffic.baseline/stress`, `.export(cobertura)` | payment-suite |
| `network.mode: execution`, `services.<name>.devices:` | returns-control-plane |
| Log `{{ }}` templates, `on_failure=` | returns-control-plane |
| `traffic.spike` | returns-control-plane |
| Playwright `browser()`, loops | storefront-browser-lab |
| `.export(ctrf)`, `traffic.soak`, `log.error` | storefront-browser-lab |
| All 8 `security.*` modes | soap-claims-hub |
| `traffic.scalability`, hardware device profile | soap-claims-hub |
| OCI modules, `service.run(image=, commands=)` detached | fleet-control-room |
| `traffic.wave`, all log levels | fleet-control-room |
| `suite.run` | composite-readiness |

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

## Syncing Example Content

```powershell
cd backend
go run ./cmd/sync-examples
```

## Why The Examples Matter

The examples act as runnable reference suites, UI inspection data, catalog enrichment sources, and authoring guides for new suites and modules.
