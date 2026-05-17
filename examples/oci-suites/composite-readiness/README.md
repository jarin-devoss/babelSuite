# Composite Readiness

Minimal workspace suite that imports another suite as a pinned OCI dependency, then runs a single Go smoke test to confirm end-to-end readiness.

## Overview

This suite demonstrates BabelSuite's nested composition model. Rather than re-declaring a full topology, it references an existing published suite (`payments-module`) and adds one final verification step on top. The dependency lock file ensures reproducible runs — the imported suite is always resolved to the same artifact digest.

Use it as a reference for workspace suites that assemble multiple independently-versioned suite packages, or as a template for a readiness gate that sits on top of an existing environment.

## Execution Order

```
payments  ← imported suite (resolved from dependencies.lock.yaml)
    │
readiness_smoke  ← Go test verifying end-to-end health
```

## Structure

| Path | Description |
|---|---|
| `suite.star` | Two-step topology: import `payments-module`, then run the smoke test |
| `dependencies.yaml` | Alias-to-reference manifest declaring imported suites with version and profile |
| `dependencies.lock.yaml` | Exact artifact digest pins for all imported suites — committed for reproducibility |
| `profiles/` | Profile overrides for the composite suite (passed down to imported suites at launch time) |
| `tests/go/` | `readiness_smoke_test.go` — Go integration test asserting end-to-end health of the composed environment |

## Running

```bash
# Run with resolved dependencies from the lock file
babelctl run localhost:5000/babelsuite/composite-readiness:latest --profile local

# Update the lock file after changing dependencies.yaml
babelctl lock localhost:5000/babelsuite/composite-readiness:latest
```

## Key Concepts Demonstrated

- **Suite composition** — `suite.run(ref="payments-module")` pulls and starts a fully self-contained suite as a single dependency node
- **Dependency locking** — `dependencies.lock.yaml` pins the imported suite to an exact OCI digest, making runs reproducible across environments
- **Thin workspace layer** — the workspace suite adds no new services or mocks; it only orchestrates and validates, keeping environment ownership with the imported suite
- **Profile propagation** — profiles declared in the workspace suite are passed to imported suites at launch time, allowing environment-specific config to flow across composition boundaries
