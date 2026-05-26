---
title: "ADR-001: APISIX sidecar for security and traffic"
---

# ADR-001: APISIX sidecar for security and traffic

**Status:** Accepted  
**Date:** 2024-12-01

## Context

BabelSuite suites need to run security probes (vulnerability scans, fuzz tests, header checks, CORS audits, rate-limit validation) and traffic load generation against services under test. Several approaches were evaluated:

1. **Dedicated containers per scan type** — spin up a ZAP or Nuclei container for each security check, a k6 or Locust container for traffic.
2. **Inline Go implementation** — implement probe logic directly in the backend runner.
3. **APISIX sidecar with Lua plugins** — deploy a single Apache APISIX gateway alongside the suite as a sidecar; implement scan logic as Lua plugins inside that gateway.

Options 1 and 2 both violate BabelSuite's core constraint: suites must not require extra containers or external tooling beyond what is declared in the suite file. Option 1 adds hidden infrastructure dependencies. Option 2 duplicates complex network-level attack logic in Go and cannot easily be extended.

## Decision

BabelSuite deploys an APISIX gateway as a sidecar whenever a suite contains `security.*` or `traffic.*` nodes. All security and traffic execution runs as Lua plugins inside APISIX (`babelsuite-attack-scanner` and `babelsuite-traffic-cannon`). The backend runner delegates to these plugins via HTTP POST and receives structured JSON results.

## Consequences

**Positive:**
- Suites remain self-contained: no external scan tools need to be installed or configured.
- APISIX is the single extension point; new scan types are new Lua plugins, not new backend services.
- The runner's security code is minimal — it only delegates and evaluates thresholds.
- The sidecar model is transparent: suite authors declare security nodes in `suite.star` and results flow through the same event stream as all other steps.

**Negative:**
- APISIX must be available and healthy before security/traffic steps execute; if the sidecar fails to start, those steps fail with a clear error.
- Security steps require `service.mock` (or another APISIX-backed node) to be present in the topology to establish the sidecar gateway URL.

**Constraints enforced:**
Security steps that lack a gateway URL (no sidecar in the topology) return an explicit error: `security step requires an APISIX sidecar — add a service.mock node before this step in the topology`. This prevents silent degradation to a weaker inline implementation.
