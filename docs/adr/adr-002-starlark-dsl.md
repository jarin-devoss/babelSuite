---
title: "ADR-002: Starlark as the topology DSL"
---

# ADR-002: Starlark as the topology DSL

**Status:** Accepted  
**Date:** 2024-12-01

## Context

BabelSuite needs a way for teams to describe the topology of a test environment — which services depend on which databases, which steps must complete before others start, what parameters can vary across runs. Several representation formats were evaluated:

1. **YAML/JSON** — declarative, but static. No conditional logic, no looping, no computed values.
2. **Dockerfile/Docker Compose** — familiar for many developers but couples BabelSuite to Docker as the only execution backend and lacks the ability to express test orchestration.
3. **General scripting languages (Python, JavaScript)** — expressive but dangerous for remote execution; full language semantics make sandboxing hard and evaluation non-deterministic.
4. **Starlark** — a deterministic, sandboxed, Python-like language designed for build files and configuration. Used in Bazel, Buck, and similar tools.

## Decision

BabelSuite uses Starlark as the topology DSL for `suite.star` files and module scripts. The runtime evaluates `suite.star` in a sandboxed Starlark interpreter and returns a dependency graph of topology nodes.

## Consequences

**Positive:**
- Deterministic evaluation: the same `suite.star` always produces the same topology graph given the same inputs.
- Sandboxed execution: file I/O, network access, and non-deterministic operations are unavailable inside the interpreter, making remote evaluation safe.
- Python-like syntax is familiar to most developers without introducing the risk surface of a full Python runtime.
- Loops and conditionals allow concise expression of matrix topologies (e.g., spinning up one service per region in a `for` loop).
- `load()` statements enable module composition and reuse across suites.

**Negative:**
- Starlark is not a standard language; tooling support (IDE completions, type checkers) is limited compared to Python or TypeScript.
- The sandboxed model means suite authors cannot call arbitrary shell commands or import Go packages; side effects must be expressed as topology nodes.
- Error messages from the Starlark interpreter require some learning to interpret, especially for teams unfamiliar with build-system DSLs.
