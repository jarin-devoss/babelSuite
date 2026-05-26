---
title: "ADR-004: OCI artifacts as the distribution format"
---

# ADR-004: OCI artifacts as the distribution format

**Status:** Accepted  
**Date:** 2024-12-01

## Context

BabelSuite suites and modules need a versioned, pullable, discoverable distribution format. Teams must be able to publish a suite once and run it anywhere — on a laptop, in CI, in Kubernetes — without re-packaging or environment-specific steps. Several options were considered:

1. **Git repositories** — familiar but lack a versioning and pull mechanism suited for immutable release artifacts; branch references are mutable.
2. **npm / Go modules / language-specific registries** — familiar in their respective ecosystems but impose a language dependency and don't generalize across the polyglot nature of BabelSuite suites.
3. **Helm charts** — designed for Kubernetes, carry assumptions about deployment targets that don't match test orchestration.
4. **OCI artifacts** — the Open Container Initiative artifact specification supports arbitrary content types beyond container images; OCI registries (GHCR, ECR, Docker Hub, Zot) are widely deployed, access-controlled, and have mature CLI tooling.

## Decision

BabelSuite distributes suites and modules as OCI artifacts. Each suite or module is published to an OCI registry with a content type specific to BabelSuite. The control plane's catalog discovers packages by scanning configured OCI registries. `babelctl module pull` and `babelctl module fork` operate against OCI references.

## Consequences

**Positive:**
- Any OCI-compatible registry works out of the box — no BabelSuite-specific registry infrastructure is required.
- Artifact immutability is enforced by digest-pinning; running a suite at a specific digest is reproducible across all environments.
- Access control reuses existing registry authentication (OIDC tokens, robot accounts, pull secrets) without a new auth system.
- The catalog UI and CLI can browse, star, and pull packages using the same OCI conventions used for container images.

**Negative:**
- OCI artifact support requires the registry to implement the OCI Distribution Spec v1.1; older registries that only support the Docker Registry v2 API may not support arbitrary content types.
- Teams unfamiliar with OCI tooling need to learn `babelctl module push` / `pull` rather than a language-specific package manager.
- Local development requires a local OCI registry (Zot is bundled for convenience) rather than a simple file copy.
