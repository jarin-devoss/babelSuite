---
title: Examples
---

# Examples

[Back to index](index.md)

## Example Suites

The example suites under [`examples/oci-suites/`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites) are ordered from simplest to most complex. Each one introduces new features on top of the previous.

| Level | Suite | What it introduces |
|-------|-------|--------------------|
| 1 | [`notification-hub`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/notification-hub) | `service.run`, `test.run` with `commands=`, profile `env:`, `services.<name>.env:` |
| 2 | [`identity-broker`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/identity-broker) | `task.run` with `commands=`, `service.mock`, `log.info`, `secretRefs`, `continue_on_failure` |
| 3 | [`payment-suite`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/payment-suite) | `file=` in tasks and tests, profile `extendsId` inheritance, `network.mode: execution`, per-currency loops |
| 4 | [`returns-control-plane`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/returns-control-plane) | `services.<name>.devices: ["gpu"]`, `log` template placeholders, `on_failure` rollback, `traffic.baseline` |
| 5 | [`storefront-browser-lab`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/storefront-browser-lab) | Playwright `browser()` nodes, multi-browser loop, `continue_on_failure`, GPU in promo profile |
| 6 | [`soap-claims-hub`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/soap-claims-hub) | `security.*` nodes, hardware profile with `/dev/ttyUSB0`, conditional `LEGACY_MODE` adapter |
| 7 | [`fleet-control-room`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/fleet-control-room) | Multi-region loops, GPU in perf profile, complex conditional topology, `log` with full template |
| 8 | [`security-suite`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/security-suite) | Full eight-mode security scan, hardware profile for USB-connected device |
| 9 | [`composite-readiness`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/composite-readiness) | `suite.run` cross-suite orchestration, health endpoint probes, composite readiness gates |

### Feature quick-reference

| Feature | First seen in |
|---------|---------------|
| `service.run` + `test.run` | notification-hub |
| Profile `env:` / `services.<name>.env:` | notification-hub |
| `task.run` with `commands=` | identity-broker |
| `service.mock` | identity-broker |
| `log.info` / `log.warn` | identity-broker |
| `secretRefs` | identity-broker |
| `continue_on_failure` | identity-broker |
| `file=` in task/test | payment-suite |
| Profile `extendsId` inheritance | payment-suite |
| `network.mode: execution` | payment-suite |
| `services.<name>.devices:` | returns-control-plane |
| `log` template placeholders (`{{ healthy }}`, `{{ env.X }}`) | returns-control-plane |
| `on_failure` rollback | returns-control-plane |
| `traffic.*` | returns-control-plane |
| Playwright `browser()` | storefront-browser-lab |
| `security.*` | soap-claims-hub |
| Hardware device profile (`/dev/ttyUSB0`) | soap-claims-hub |
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
