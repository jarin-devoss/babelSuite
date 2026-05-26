---
title: "ADR-003: CUE schemas for mock exchange definitions"
---

# ADR-003: CUE schemas for mock exchange definitions

**Status:** Accepted  
**Date:** 2024-12-01

## Context

BabelSuite's internal mocking engine needs a way for suite authors to define mock HTTP/gRPC responses — the request matchers, response bodies, headers, and state machine transitions that govern how a mock service behaves. Several formats were evaluated:

1. **Static JSON/YAML files** — simple but cannot express constraints, conditional values, or schema-level validation.
2. **WireMock stubs** — well-known format but requires a WireMock container to run and couples the mock definition to a specific tool's semantics.
3. **OpenAPI + hand-rolled templates** — expressive but verbose; requires a separate templating engine.
4. **CUE** — a constraint-based configuration language that unifies schema validation, data templating, and value constraints into one representation.

## Decision

BabelSuite uses CUE files (`*.cue`) under the `mock/` directory of a suite package to define mock exchange behaviors. The mocking engine evaluates CUE files to produce validated response templates at runtime.

## Consequences

**Positive:**
- CUE schemas validate request and response structures at definition time, catching authoring errors before a suite runs.
- Constraints express conditional mock behaviors (e.g., return a 404 for unknown IDs) without external scripting.
- No WireMock container — the internal mocking engine serves responses directly, keeping the suite self-contained.
- CUE's `@gen` and `@resolve` annotations allow generated and computed mock fields without imperative code.

**Negative:**
- CUE is not widely known; teams adopting BabelSuite have a learning curve for mock authoring.
- The CUE toolchain must be available or vendored in the backend build.
- Complex stateful mock scenarios require careful use of CUE's value lattice semantics, which can be non-obvious for developers coming from procedural languages.
