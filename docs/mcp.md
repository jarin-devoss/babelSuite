---
title: MCP Server
---

# MCP Server

[Back to index](index.md)

`babelsuite-mcp` is a [Model Context Protocol](https://modelcontextprotocol.io) server that exposes the control plane to AI agents. It speaks MCP over stdio and forwards every call to the same REST API the UI and `babelctl` use, so an agent can browse the catalog, launch executions, follow them to completion, and manage plugins without being given shell access.

---

## Building

```bash
cd backend && go build -o babelsuite-mcp ./cmd/mcp
```

The server talks to a control plane that is already running — it does not start one.

## Configuration

| Variable | Required | Purpose |
|----------|----------|---------|
| `BABELSUITE_URL` | No | Control plane base URL (default `http://localhost:8090`) |
| `BABELSUITE_TOKEN` | No | JWT bearer token; skips sign-in entirely |
| `BABELSUITE_EMAIL` | No | Email for automatic sign-in at startup when no token is set |
| `BABELSUITE_PASSWORD` | No | Password for automatic sign-in |
| `BABELSUITE_MCP_READONLY` | No | `true` withholds every state-changing tool (see [Read-Only Mode](#read-only-mode)) |

Supply either a token or an email/password pair. With neither, the server still starts and lists its tools, but every call that needs authentication fails until the `sign_in` tool is used.

## Client Setup

Register it with any MCP client that supports stdio servers:

```json
{
  "mcpServers": {
    "babelsuite": {
      "command": "/path/to/babelsuite-mcp",
      "env": {
        "BABELSUITE_URL": "http://localhost:8090",
        "BABELSUITE_EMAIL": "admin@babelsuite.test",
        "BABELSUITE_PASSWORD": "admin"
      }
    }
  }
}
```

## Read-Only Mode

Set `BABELSUITE_MCP_READONLY=true` to withhold every tool that changes state — launching runs, writing suites, profiles and schedules, deleting plugins, and reaping sandboxes. Those tools are not registered at all rather than failing when called, so an agent never sees a capability it cannot use.

Use it when an agent only needs to inspect an environment. `reap_all_sandboxes` in particular tears down every managed sandbox on the host, including executions started by other people.

## Skills

`get_skill` returns a step-by-step playbook for a multi-step task, including the tool call order and the errors that task tends to produce. Reading the relevant one first is cheaper than rediscovering the sequence by trial and error.

| Skill | Use when |
|-------|----------|
| `author-suite` | Creating or scaffolding a new suite from scratch |
| `debug-execution` | A run failed, stalled, or behaved unexpectedly |
| `schedule-suite` | Setting up recurring runs with notifications |

## Tools

### Authentication

| Tool | Required arguments | Purpose |
|------|--------------------|---------|
| `sign_in` | `email`, `password` | Obtain a JWT; later calls reuse it automatically |

### Suites and catalog

| Tool | Required arguments | Purpose |
|------|--------------------|---------|
| `list_suites` | — | All suites with their profiles and backend options |
| `get_suite` | `suite_id` | One suite in full: topology, profiles, and every source file |
| `create_suite` | `id`, `suite_star` | Create a suite, optionally with its `source_files` |
| `resolve_suite_ref` | `ref` | Resolve an OCI reference to a suite |
| `list_packages` | — | Every package in the OCI catalog |
| `get_package` | `package_id` | Metadata for one catalog package |
| `list_favorites` | — | The current user's starred packages |

A suite is its topology plus the files its steps run, so pass `source_files` alongside `suite_star`:

```json
{
  "id": "checkout-suite",
  "suite_star": "load(\"@babelsuite/runtime\", \"task\", \"test\")\n...",
  "source_files": [
    {"path": "tasks/migrate.py",    "content": "..."},
    {"path": "tests/smoke.py",      "content": "..."},
    {"path": "profiles/local.yaml", "content": "name: Local\ndefault: true\n"}
  ]
}
```

Paths follow the package layout in [Suite Authoring](suite-authoring.md): `file="migrate.py"` on a task resolves to `tasks/migrate.py`, and every `profiles/*.yaml` becomes a launchable profile. Without these files the suite is only a topology shell whose steps have nothing to execute.

### Profiles

| Tool | Required arguments | Purpose |
|------|--------------------|---------|
| `list_profile_suites` | — | Suites that have profiles, with a count for each |
| `get_suite_profiles` | `suite_id` | Every profile for a suite, with its YAML and inheritance |
| `create_suite_profile` | `suite_id`, `name`, `file_name`, `yaml` | Add a launchable profile |
| `update_suite_profile` | `suite_id`, `profile_id` | Replace a profile — omitted fields are cleared |
| `set_default_profile` | `suite_id`, `profile_id` | Choose the profile used when none is named |
| `delete_suite_profile` | `suite_id`, `profile_id` | Remove a profile permanently |

### Scheduled runs

| Tool | Required arguments | Purpose |
|------|--------------------|---------|
| `list_cron_jobs` | — | Scheduled jobs with their next run and last result |
| `get_cron_job` | `cron_job_id` | One job in full, including `lastError` |
| `create_cron_job` | `name`, `schedule` | Schedule suites on a cron expression |
| `update_cron_job` | `cron_job_id` | Replace a job — omitted fields are cleared |
| `delete_cron_job` | `cron_job_id` | Remove a schedule |

### Operations

| Tool | Required arguments | Purpose |
|------|--------------------|---------|
| `list_agents` | — | Remote execution agents with health and last heartbeat |
| `get_engine_overview` | — | Engine-wide queued, running, and completed step counters |
| `get_system_health` | — | Datastore, cache, and runner readiness |

### Executions

| Tool | Required arguments | Purpose |
|------|--------------------|---------|
| `launch_execution` | `suite_id` | Start a suite execution |
| `list_executions` | — | Recent executions, newest first |
| `get_execution` | `execution_id` | Status, step snapshots, events, and artifacts |
| `watch_execution` | `execution_id` | Block until the execution reaches a terminal state |
| `get_execution_logs` | `execution_id` | Snapshot of every log line emitted so far |
| `get_execution_overview` | — | Live dashboard across all active executions |

### Modules and plugins

| Tool | Required arguments | Purpose |
|------|--------------------|---------|
| `list_modules` | — | OCI module packages in the catalog |
| `get_module` | `module_id` | Metadata and exported symbols for one module |
| `list_plugins` | — | Registered APISIX Lua plugins |
| `create_plugin` | `name`, `trigger`, `lua` | Register a Lua plugin |
| `delete_plugin` | `name` | Remove a registered plugin |
| `check_plugin` | `name` | Verify a plugin exists, its trigger is valid, and its CUE schema parses |
| `validate_plugin_config` | `name`, `config` | Validate a config against a plugin's CUE schema without running it |

### Sandboxes

| Tool | Required arguments | Purpose |
|------|--------------------|---------|
| `list_sandboxes` | — | Running containers, networks, volumes, and resource usage |
| `reap_sandbox` | `sandbox_id` | Clean up one sandbox by its execution ID |
| `reap_all_sandboxes` | — | Clean up every BabelSuite-managed sandbox |

!!! warning
    `reap_all_sandboxes` tears down every managed sandbox on the host, including executions started by other people. Agents given this tool can interrupt work that is not theirs.

## Authentication Behaviour

The control plane protects state-changing requests with a double-submit CSRF cookie, and exempts anything carrying an `Authorization: Bearer` header (see [Authentication](auth.md)). Sign-in is the one unauthenticated write, so the client fetches a CSRF cookie from a safe endpoint first and echoes it back in `X-CSRF-Token`. Once a token is held, that round trip is skipped and the bearer exemption applies.

## Troubleshooting

**`auto sign-in failed: API error 403: CSRF token missing.`**
The client did not obtain a CSRF cookie before signing in. Check that `BABELSUITE_URL` points at the control plane itself rather than a proxy that drops `Set-Cookie`.

**`API error 401: Incorrect email or password.`**
Credentials are wrong, or no admin was seeded — the control plane only creates one when both `ADMIN_EMAIL` and `ADMIN_PASSWORD` are set at startup.

**Tools list but every call fails**
The server starts without credentials by design. Set `BABELSUITE_TOKEN`, or set `BABELSUITE_EMAIL` and `BABELSUITE_PASSWORD`, or call `sign_in` first.

**A tool the agent expects is missing**
Read-only mode is on, and the tool changes state. Unset `BABELSUITE_MCP_READONLY`.

**A created suite launches but its steps do nothing**
It was created without `source_files`, so the files its steps name do not exist. Suites are not updated in place — recreate it with the files included.

**A step exits with code 127**
The file's interpreter is missing from the image: `.sh` runs under `bash` and `.py` under `python`. Match the image to the file type — `busybox` has no bash.
