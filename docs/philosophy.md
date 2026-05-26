---
title: Philosophy
---

# Philosophy

[Back to index](index.md)

## One File, No Setup Scripts

Every team that has ever shared a test environment has experienced the same failure mode: a README that is six months out of date, a setup script that works on one machine, and the one engineer who "knows how to run it locally."

BabelSuite replaces all of that with a single `suite.star` file. It is a short, readable declaration of the full environment your tests need — databases, mocks, migrations, traffic, security probes, and the tests themselves. The file is the contract. Anyone who can read it knows exactly what the suite does and in what order.

## Declare What, Not How

A `suite.star` file describes a topology graph, not a script. You name nodes and their dependencies:

```python
db      = pg()
migrate = task.run(file="migrate.py", image="python:3.12", after=[db])
api     = service.run(after=[db, migrate])
smoke   = test.run(file="smoke_test.go", image="golang:1.24", after=[api])
```

BabelSuite resolves the graph, determines the execution order, and runs each node when its dependencies are satisfied. There are no shell conditionals, no `sleep` loops, no manual port assignments. The runtime handles the rest.

## Zero Extra Containers for Built-In Checks

The single biggest cost of "test environment as code" tools is the number of extra processes they require. A k6 container for load testing. A ZAP container for security scanning. A WireMock container for mocking. Each one is a new thing to pull, configure, resource-limit, and clean up.

BabelSuite avoids this through the APISIX sidecar model:

- **Mocking** is handled by BabelSuite's internal engine, backed by CUE schemas in `api/` and `mock/`. No WireMock, no Prism container required.
- **Traffic testing** — all `traffic.*` profiles — is executed by a Lua plugin (`babelsuite-traffic-cannon`) running inside the APISIX sidecar that is already provisioned alongside every mock node. No k6, no Locust, no Gatling.
- **Security scanning** — all `security.*` modes (probe, fuzz, headers, verbs, CORS, auth, GraphQL, flood) — is executed by a second Lua plugin (`babelsuite-attack-scanner`) inside the same APISIX sidecar. No ZAP, no Nuclei.

The sidecar is already there. The checks are Lua running inside OpenResty. The backend just POSTs a config and reads a JSON findings report back. No extra image pull. No extra container lifecycle.

The only steps that need a container are `task.run` and `test.run` — because those run user-supplied scripts that BabelSuite cannot anticipate. Everything else runs in the sidecar or in the control plane itself.

## APISIX as the Execution Surface

APISIX serves as the gateway and execution surface for all built-in runtime checks:

```
suite.star topology
      │
      ├── service.mock ──► BabelSuite mocking engine  (internal, no container)
      │        │
      │        └── APISIX sidecar ──► traffic.* Lua plugin  (no extra container)
      │                          └──► security.* Lua plugin (no extra container)
      │
      ├── task.run ──────► Docker container  (user script — container needed)
      └── test.run ──────► Docker container  (user script — container needed)
```

This means a full suite with mocks, load testing, and security probes requires exactly two things beyond the BabelSuite control plane: the application under test and its APISIX sidecar. Everything else is embedded.

## Suites Are OCI Artifacts

A suite is not a git branch or a folder convention. It is an OCI artifact published to a registry. Any team can pull it with `babelctl run`, run it against their own backend, and get identical results — on a laptop, in CI, or on Kubernetes. There are no environment-specific scripts, no hardcoded endpoints, no secrets in the file.

Profiles carry the environment-specific overrides (endpoints, credentials, feature flags) separately from the suite topology. The suite stays portable. The profile stays local.

## Modules Replace Plugin Ecosystems

Every major CI, testing, and mocking tool eventually grows the same plugin problem — and it always looks the same: the file that describes what you need is separate from the step that makes it available.

**Jenkins** pipelines reference plugins that live on the controller, not in the repository. **pytest** plugins are declared in `pyproject.toml` but still require a separate `pip install`. **WireMock** extensions are JARs that have to be on the classpath before the server starts. In all three cases, the config says what you need and something else is responsible for making it available. That gap is where "works on my machine" lives.

BabelSuite closes it:

```python
load("@babelsuite/kafka",    "kafka", "create_topic")
load("@babelsuite/postgres", "pg",    "connect")
```

The declaration is the installation. The workspace loader resolves the import, pulls the OCI artifact, and makes the symbols available as part of running the suite — not before it. No separate step, no per-machine state to drift.

Writing a module is equally straightforward — it is Starlark code that returns topology nodes, packaged as an OCI artifact. There is no plugin SDK to implement, no host version to target, no binary to compile. If you can write a suite, you can write a module. The same simplicity that makes modules easy to use makes them easy to build.

## The Graph Is the Documentation

A `suite.star` file that takes two minutes to read tells you more about a service's dependencies than any architecture diagram. It tells you the exact startup order, which components are mocked vs real, what migrations run before the service starts, what traffic profiles are exercised, and what security checks are applied.

This is intentional. The suite is not scaffolding. It is the authoritative description of how the service is tested.
