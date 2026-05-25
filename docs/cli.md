---
title: CLI
---

# CLI Reference

[Back to index](index.md)

`babelctl` is the BabelSuite command-line tool. It connects to a running control plane, persists a local session, and lets you manage suites, executions, and environments from a terminal or CI pipeline.

---

## Installation

Build from source:

```bash
go build -o babelctl ./backend/cmd/ctl
```

Release builds inject the version string at link time:

```bash
go build \
  -ldflags "-X github.com/babelsuite/babelsuite/cli/ctl.Version=v1.2.3" \
  -o babelctl ./backend/cmd/ctl
```

---

## Global Options

These flags are accepted before any command:

| Flag | Default | Description |
|------|---------|-------------|
| `--server <url>` | `http://localhost:8090` | Control-plane URL |
| `--output text\|json` | `text` | Output format |
| `--config <path>` | `~/.babelsuite/config.yaml` | Local config file |

---

## Session Commands

### `login`

```bash
babelctl login
babelctl --server https://babelsuite.example.com login
```

Prompts for email and password, obtains a JWT, and persists it to the local config file. Subsequent commands use this token automatically.

### `logout`

```bash
babelctl logout
```

Removes the saved session token from the local config file.

### `whoami`

```bash
babelctl whoami
babelctl --output json whoami
```

Prints the currently authenticated user (email, workspace ID, admin flag).

---

## Suite Commands

### `catalog list`

```bash
babelctl catalog list
babelctl --output json catalog list
```

Lists all packages discovered from configured OCI registries.

### `catalog inspect <package>`

```bash
babelctl catalog inspect payment-suite
babelctl catalog inspect localhost:5000/qa/storefront-lab:v1.3.0
```

Prints the full package metadata, suite topology, and available profiles.

### `create <name> [destination]`

```bash
babelctl create my-suite
babelctl create payment-flow ./suites/payment-flow
```

Scaffolds a starter suite layout on disk. Creates `suite.star`, `README.md`, and a `mocks/` directory. Use `--force` to overwrite an existing destination.

### `suites list`

```bash
babelctl suites list
```

Lists all suites registered in the workspace.

### `suites get <suite>`

```bash
babelctl suites get payment-suite
```

Prints suite metadata and launch configuration.

### `suites inspect <suite>`

```bash
babelctl suites inspect payment-suite
```

Prints the full Starlark topology resolved from the suite source.

### `profiles list <suite>`

```bash
babelctl profiles list payment-suite
babelctl --output json profiles list payment-suite
```

Lists the launch profiles available for a suite. Profiles override environment variables and execution parameters at launch time.

---

## Execution Commands

### `runs list`

```bash
babelctl runs list
babelctl --output json runs list
```

Lists recent executions with status, suite name, start time, and duration.

### `runs get <id>`

```bash
babelctl runs get run-abc123
```

Prints full execution details including per-step status, duration, and any evaluation results.

### `run <suite|ref> [--profile <file>]`

```bash
babelctl run payment-suite
babelctl run localhost:5000/qa/payment-suite:v2.0 --profile ./local.yaml
babelctl --output json run payment-suite
```

Creates a new execution. Optionally supply a profile YAML to override environment variables and step parameters.

**Flags:**

| Flag | Description |
|------|-------------|
| `--profile <path>` | Path to a YAML profile file |
| `--backend <id>` | Override the execution backend |
| `--watch` | Stream live logs until the execution completes |

### `fork <suite|ref> [destination]`

```bash
babelctl fork payment-suite
babelctl fork localhost:5000/qa/payment-suite:v2.0 ./local-payment
```

Downloads a suite's source files to a local directory so you can inspect or modify the Starlark topology. Useful for debugging or contributing changes back.

---

## Environment Commands

### `environments list`

```bash
babelctl environments list
babelctl envs list
```

Lists all managed sandbox environments with their status and age.

### `environments reap <id>`

```bash
babelctl environments reap env-abc123
babelctl envs reap env-abc123
```

Forcibly terminates and removes a specific environment.

### `environments reap-all`

```bash
babelctl envs reap-all
```

Terminates and removes all managed environments. Use with care in shared workspaces.

---

## System Commands

### `version`

```bash
babelctl version
```

Prints the CLI version label (e.g. `babelctl v1.2.3`).

---

## Config File

The local config file stores the server URL and session token. Default location:

- Linux/macOS: `~/.babelsuite/config.yaml`
- Windows: `%USERPROFILE%\.babelsuite\config.yaml`

Override with `--config <path>` or `BABELSUITE_CONFIG` environment variable.

Example:

```yaml
server: https://babelsuite.example.com
token: eyJhbGciOiJIUzI1NiJ9...
```

---

## CI / Automation

Use `--output json` to integrate with scripts:

```bash
# Get the latest execution ID
RUN_ID=$(babelctl --output json runs list | jq -r '.[0].id')

# Trigger a run and capture the ID
RUN_ID=$(babelctl --output json run payment-suite | jq -r '.id')

# Check status
babelctl --output json runs get "$RUN_ID" | jq '.status'
```

Use `BABELSUITE_SERVER` and `BABELSUITE_TOKEN` environment variables to avoid storing credentials in shell history:

```bash
export BABELSUITE_SERVER=https://babelsuite.example.com
export BABELSUITE_TOKEN=eyJhbGciOiJIUzI1NiJ9...
babelctl runs list
```

---

## See Also

- [Getting Started](getting-started.md) — first-time setup
- [Suite Authoring](suite-authoring.md) — writing Starlark suites
- [Profiles](profiles.md) — launch-time environment overrides
- [API](api.md) — REST API reference
