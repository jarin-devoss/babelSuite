---
title: Mocking
---

# Mocking

[Back to index](index.md)

## What The Mocking Layer Does

BabelSuite's mocking service resolves suite-defined API surfaces and returns mock responses for REST, gRPC, and async operations.

For each incoming mock request, the service locates:

1. the target suite and API surface
2. the matching operation
3. the best-matching example or schema-backed example

## Mock Endpoints

| Endpoint | Purpose |
|----------|---------|
| `GET /mocks/rest/...` | REST mock handler |
| `POST /mocks/grpc/{suiteId}/{surfaceId}/{operationId}` | gRPC mock handler (the HTTP form; gRPC clients are served by the gRPC front end) |
| `POST /mocks/async/{suiteId}/{surfaceId}/{operationId}` | Async mock handler, which the TCP and UDP plugins and the publisher call |
| `GET /mocks/ws/{suiteId}/{surfaceId}/{operationId}` | WebSocket mock handler |
| `/internal/mock-data/` | Internal metadata resolver |

These are the gateway's back end, not an address for a test to call: every one answers `401` unless the request carries the gateway's token. Call the suite's gateway (`BABELSUITE_GATEWAY_URL`, or a mock by its node's name); see [Mock Traffic](architecture.md#mock-traffic) for how each protocol is served.

## Operation Metadata

Each mock operation can carry:

- **adapter** — protocol adapter (`rest`, `grpc`, `async`)
- **dispatcher** — a label the response echoes in `X-Babelsuite-Dispatcher` (`apisix` unless set). It selects nothing: the answer is chosen by the examples' `dispatch:` conditions, then the fallback. There are no `script`, `sequence` or `random` strategies; use `state` for an answer that changes from call to call
- **dispatcherRules** — a generated description of how the operation is resolved; it is not read
- **delayMillis** — simulated latency
- **parameterConstraints** — named constraints for routing by request attributes
- **fallback** — what to return when no example matches
- **state** — stateful behavior rules (lookup/mutation keys, transitions)

For the complete field reference, see [Mocking Reference](mocking-reference.md).

## Exchange Examples

Operations include named exchange examples with:

- `requestHeaders` and `requestBody`
- `responseStatus`, `responseMediaType`, `responseHeaders`, and `responseBody`
- optional `when` conditions — match by state, parameter value, or header

## Fallback Modes

When no example matches the incoming request, the fallback controls the response:

| Mode | Behavior |
|------|---------|
| `static` | Return an inline body defined in the operation metadata |
| `example` | Return a named example from the operation's exchange list |
| `proxy` | Forward the request to an upstream URL |

This keeps a suite useful even when the exact request scenario isn't covered by an example.

## Stateful Mocking

Operations can declare state rules to enable request-driven in-memory mutations:

```yaml
state:
  lookupKeyTemplate: "order-{orderId}"
  mutationKeyTemplate: "order-{orderId}"
  defaults:
    status: pending
  transitions:
    - onExample: confirm-order
      set:
        status: confirmed
    - onExample: cancel-order
      set:
        status: cancelled
```

This supports create-update-delete flows, sequence state, and other stateful patterns without a real database.

### Whose state a request uses

State is kept **per run**. Two runs of the same suite, even on the same profile and at the same time, each start from the mock's `defaults`: one run's requests never change what the other sees, and `reset_mocks=` in one run clears only that run's state. A run's state is dropped when the run ends.

The mock engine learns which run a request belongs to in one of two ways:

- **The request goes through the suite's gateway.** On Docker the sidecar joins the run's network, so a step calls `$BABELSUITE_GATEWAY_URL` (or `http://<suite>.mock.internal:9080`) and the gateway tells the engine which network the request arrived on. Nothing in the suite has to change.
- **The request names its run**, with `X-Babelsuite-Execution: $BABELSUITE_EXECUTION_ID`, for a caller that reaches the engine directly.

A request that does neither (a call from the host, a run on a Kubernetes or remote-agent backend, a header naming a run that does not exist) uses the state **shared by every run of the suite**, as before. The response says which it used:

| `X-Babelsuite-State-Scope` | Meaning |
|---|---|
| `execution` | this run's own state |
| `suite` | the state shared by every run of the suite; concurrent runs can see each other's changes |

The header is absent when the operation keeps no state. `reset_mocks=` in a run with no scope of its own clears the shared state.

## Where Mock Data Lives In A Suite

```text
my-suite/
  api/       # contracts and OpenAPI/Protobuf schemas
  mock/      # mock metadata files and exchange examples
```

The suite hydration layer normalizes metadata paths and generates resolver and runtime URLs so the UI and runtime can reference them consistently.

!!! tip "Authoring guidance"
    - Keep large mock definitions in `mock/` rather than inline in `suite.star`.
    - Use named exchange examples instead of giant inline response bodies.
    - Define fallback behavior explicitly so the mock surface degrades predictably.
    - Use state transitions only when the suite genuinely needs request-driven stateful behavior.

## Related Pages

- [Mocking Reference](mocking-reference.md) — complete field reference for surfaces, operations, state, exchanges, and parameters
- [Suites](suites.md) — suite structure and how mocks fit into the topology
- [Suite Authoring Reference](suite-authoring.md) — mock folder layout and authoring patterns
