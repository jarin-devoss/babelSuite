---
title: Examples
---

# Examples

[Back to index](index.md)

## Example Suites

The repository includes these workspace suites under [`examples/oci-suites/`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites):

| Suite | Description |
|-------|-------------|
| [`composite-readiness`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/composite-readiness) | Nested suite composition with shared readiness gates |
| [`fleet-control-room`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/fleet-control-room) | Event-driven topology with multiple async workers |
| [`identity-broker`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/identity-broker) | Auth and SSO flows with mock identity provider |
| [`kafka-topic-lifecycle`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/kafka-topic-lifecycle) | Kafka broker, topic creation, and consumer group tests |
| [`payment-suite`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/payment-suite) | Mock-heavy payment API with traffic and security probes |
| [`returns-control-plane`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/returns-control-plane) | Policy and fixture-driven returns and refund flows |
| [`soap-claims-hub`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/soap-claims-hub) | SOAP/XML API mocking with CUE-driven mock exchanges |
| [`storefront-browser-lab`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-suites/storefront-browser-lab) | Browser-driven Playwright tests across device and browser matrix |

## Example Modules

The example module folders under [`examples/oci-modules/`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-modules):

| Module | Description |
|--------|-------------|
| [`kafka`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-modules/kafka) | Kafka broker cluster and topic admin helpers |
| [`postgres`](https://github.com/jarin-devoss/babelSuite/tree/main/examples/oci-modules/postgres) | Postgres cluster and query execution helpers |

These are pure Starlark module examples built on top of the built-in runtime primitives.

## Syncing Example Content

The repository includes:

- `backend/cmd/sync-examples`

This command syncs generated example workspace content into the checked-in examples root.

Run it from `backend/` with:

```powershell
go run ./cmd/sync-examples
```

## Seeding A Local Registry

The repository also includes:

- `backend/cmd/seed-zot`

This command can publish seeded references into a local registry so the catalog has discoverable package content.

## Why The Examples Matter

The examples act as:

- runnable reference suites
- UI inspection data
- catalog enrichment sources
- authoring examples for new suites and modules
