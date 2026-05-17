# @babelsuite/postgres

Pure Starlark Postgres module providing a managed database service and query helpers, built on BabelSuite's runtime primitives.

## Overview

This module wraps Postgres cluster lifecycle and SQL operations into importable Starlark symbols. Suites load it to get a consistent, health-checked database service and a set of typed query helpers without writing raw `service.run()` or shell-based migration tasks.

## Details

| Field | Value |
|---|---|
| Repository | `localhost:5000/babelsuite/postgres` |
| Latest version | `1.4.0` |
| Available tags | `1.4.0`, `1.3.2`, `latest` |
| Entrypoint | `module.star` |

## Exported Symbols

| Symbol | Description |
|---|---|
| `pg` | Starts and returns a managed Postgres service |
| `connect` | Opens a typed connection to a running Postgres instance |
| `query` | Executes a raw SQL query and returns rows |
| `insert` | Inserts a record into a table |
| `select` | Selects rows with optional filtering |
| `delete` | Deletes rows matching a condition |
| `upsert` | Inserts or updates a record based on a conflict key |

## Usage

```python
load("localhost:5000/babelsuite/postgres:1.4.0", "pg", "connect", "insert")

db = pg()
conn = connect(db)
insert(conn, table="users", values={"email": "test@example.com"})
```

See `module.star` for the full implementation and `usage.star` for a complete query example.

## Pull and Fork

```bash
# Pull the module into a suite
babelctl run localhost:5000/babelsuite/postgres:1.4.0

# Fork to a local copy for modification
babelctl fork localhost:5000/babelsuite/postgres:1.4.0 ./stdlib-postgres
```
