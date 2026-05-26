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

**Jenkins** is the most familiar case. A `Jenkinsfile` references pipeline steps that assume plugins are already installed on the server — a credentials plugin, a Kubernetes plugin, a test reporter. The plugins live on the Jenkins controller, not in the repository. Clone the project onto a fresh Jenkins instance and nothing works. You have to reverse-engineer which plugins the `Jenkinsfile` depends on, install them through the UI or a `plugins.txt` file maintained separately from the pipeline itself, and then pray the versions are compatible with each other. The pipeline and its dependencies are in two completely different places.

**pytest** has the same gap at the test level. A project declares `pytest-cov`, `pytest-asyncio`, and `pytest-httpx` in `pyproject.toml`, but declaring them does nothing — you still have to `pip install` them as a separate step before pytest will find them. Clone the repo, run `pytest`, and it exits immediately with `ModuleNotFoundError` until you figure out which plugins the test suite actually needs and install them by hand. The config file says what you need; something else entirely has to make it available.

**WireMock** has the same problem at the mock level. Custom response transformers, fault injectors, and extension matchers are JAR files that have to be placed on the classpath before the server starts. The `mappings/` config files can reference a transformer by name, but WireMock has no mechanism to fetch it — the JAR has to already be there through some out-of-band process. A stub configuration that works in one environment silently does nothing in another because the extension was never installed.

The common thread across all three: **the config file says what you need, and something else entirely is responsible for making it available**. That gap is where things go wrong. It is where "works on my machine" lives.

BabelSuite closes that gap with a single line at the top of your suite file:

```python
load("@babelsuite/kafka",    "kafka", "create_topic")
load("@babelsuite/postgres", "pg",    "connect")
```

There is no separate install step. The workspace loader resolves the import against your configured registries, pulls the OCI artifact, and makes the exported symbols available as part of running the suite — not before it. The module is declared where it is used, and the declaration is the installation.

Because modules are OCI artifacts, they carry the same portability guarantees as the suite itself. A suite that loads `@babelsuite/kafka` runs identically on a laptop, in CI, and on Kubernetes — the module is pulled from the registry the same way in all three environments. There is no per-machine plugin state to drift, no version to reconcile separately from the suite that uses it.

Compatibility is also simpler. A module is Starlark code that returns topology nodes. It runs in the same interpreter that runs the suite. There is no plugin API to stay in sync with, no host version requirement, no binary that has to match the platform. If the `load()` resolves and the function exists, it works.

Writing your own module is two files: a `module.star` that exports helpers and a `module.yaml` with OCI metadata. Push it to any registry your platform has access to and any suite in the workspace can load it immediately. No plugin SDK, no extension point registration, no approval process. A module is just a function someone else wrote that returns a node.

## The Graph Is the Documentation

A `suite.star` file that takes two minutes to read tells you more about a service's dependencies than any architecture diagram. It tells you the exact startup order, which components are mocked vs real, what migrations run before the service starts, what traffic profiles are exercised, and what security checks are applied.

This is intentional. The suite is not scaffolding. It is the authoritative description of how the service is tested.
