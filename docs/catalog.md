---
title: Catalog
---

# Catalog

[Back to index](index.md)

The catalog is the registry-backed discovery surface. It queries configured OCI registries, builds a searchable package view, and lets users pick a suite to run — all without leaving the UI.

---

## How Discovery Works

For each registry configured in [Platform Settings](platform.md), the catalog service:

1. Calls `GET /v2/_catalog` to enumerate repositories.
2. Calls `GET /v2/<repo>/tags/list` on each repository to find available versions.
3. Matches discovered repositories against workspace-known suites to enrich results with local metadata.
4. Merges built-in example module metadata for demo registries.

The result is a unified package list across all configured registries.

---

## Package Fields

| Field | Description |
|-------|-------------|
| `id` | Unique package identifier |
| `kind` | `suite` or `module` |
| `title` | Human-readable name |
| `repository` | OCI repository path |
| `owner` | Team or user that owns the package |
| `provider` | Source registry identifier |
| `version` | Latest tag or pinned version |
| `tags` | Searchable labels |
| `description` | Short summary of what the package does |
| `modules` | Starlark modules bundled with this package |
| `status` | Registry availability status |
| `score` | Relevance or popularity rank |
| `pullCommand` | Command to pull the OCI artifact |
| `forkCommand` | Command to fork the package locally |
| `inspectable` | Whether the suite topology can be previewed |
| `starred` | Whether the current user has starred this package |

---

## Favorites

Any package can be starred. Starred packages appear at the top of the catalog and persist per user.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/catalog/favorites` | List starred packages |
| `POST` | `/api/v1/catalog/favorites/{packageId}` | Star a package |
| `DELETE` | `/api/v1/catalog/favorites/{packageId}` | Unstar a package |

---

## Catalog vs. Suites

The catalog and the runnable suite inventory are related but distinct:

| | Catalog | Suites |
|-|---------|--------|
| Source | OCI registry | Workspace + registry |
| What it shows | All advertised packages | Packages BabelSuite can inspect and run |
| Updated by | Registry sync | Suite resolution at load time |

A package can appear in the catalog before it has been locally resolved. `inspectable: true` indicates the control plane can read and render its topology.

---

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/catalog/packages` | List all catalog packages |
| `GET` | `/api/v1/catalog/packages/{packageId}` | Get a single package |
| `GET` | `/api/v1/catalog/favorites` | List starred packages |
| `POST` | `/api/v1/catalog/favorites/{packageId}` | Star a package |
| `DELETE` | `/api/v1/catalog/favorites/{packageId}` | Unstar a package |

---

## Frontend Route

| Route | Description |
|-------|-------------|
| `/catalog` | Registry-backed package discovery and suite browser |

---

## See Also

- [Platform Settings](platform.md) — configure registries the catalog queries
- [Suites](suites.md) — how suite packages are loaded and resolved
- [Execution](execution.md) — launching a suite from the catalog
