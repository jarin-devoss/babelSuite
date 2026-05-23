---
title: Mocking Reference
---

# Mocking Reference

[Back to index](index.md)

## Mocking Model

BabelSuite's mock layer combines:

- API surface metadata
- operation metadata
- exchange examples
- fallback behavior
- optional in-memory state transitions

The suite hydration layer also normalizes mock metadata paths and runtime URLs so the UI and runtime can inspect them consistently.

## API Surface Fields

An API surface currently exposes fields such as:

- `id`
- `title`
- `protocol`
- `mockHost`
- `description`
- `operations`

## Operation Fields

Each operation can carry:

- `id`
- `method`
- `name`
- `summary`
- `contractPath`
- `mockPath`
- `mockUrl`
- `curlCommand`
- `dispatcher`
- `mockMetadata`
- `exchanges`

## Mock Metadata Fields

Per-operation mock metadata supports:

- `adapter`
- `dispatcher`
- `dispatcherRules`
- `delayMillis`
- `parameterConstraints`
- `fallback`
- `state`
- `metadataPath`
- `resolverUrl`
- `runtimeUrl`

## Parameter Constraints

Each parameter constraint can define:

- `name`
- `source`
- `required`
- `forward`
- `pattern`

## Fallback Modes

Fallback metadata supports:

- static inline fallback bodies
- named example fallbacks
- proxy fallback URLs

Fields include:

- `mode`
- `exampleName`
- `proxyUrl`
- `status`
- `mediaType`
- `body`
- `headers`

## Stateful Mocking

Mock state can define:

- `lookupKeyTemplate`
- `mutationKeyTemplate`
- `defaults`
- `transitions`

Each transition can define:

- `onExample`
- `mutationKeyTemplate`
- `set`
- `delete`
- `increment`

This supports behaviors like create-update-delete flows, sequence state, and request-driven mutations.

## Exchange Example Fields

Each exchange example can include:

- `name`
- `sourceArtifact`
- `when`
- `requestHeaders`
- `requestBody`
- `responseStatus`
- `responseMediaType`
- `responseHeaders`
- `responseBody`

Each `when` condition can define:

- `from`
- `param`
- `value`

## Protocol Endpoints

The backend currently exposes mock routes for:

- `GET /mocks/rest/...`
- `POST /mocks/grpc/{suiteId}/{surfaceId}/{operationId}`
- `POST /mocks/async/{suiteId}/{surfaceId}/{operationId}`

There is also an internal resolver path rooted under:

- `/internal/mock-data/`

## Defaulting During Hydration

During suite hydration:

- missing mock adapters are derived from protocol
- missing dispatchers are defaulted
- metadata paths are inferred from `mockPath`
- resolver URLs are generated
- runtime URLs are generated
- schema-backed mock references are normalized

## Response Body Annotations

CUE exchange files support three annotations that make response bodies dynamic at serve time. Instead of returning the same hardcoded ID or timestamp on every request, the mock engine generates a fresh value each time.

---

### `@gen` — generate a value

Attach `@gen` to any `string` field to have the mock engine produce a value at serve time.

```cue
body: {
  returnId: string @gen(kind="int", prefix="ret_", min=1001, max=9999)
  traceId:  string @gen(kind="uuid")
  servedAt: string @gen(kind="timestamp")
}
```

| Parameter | Description |
|-----------|-------------|
| `kind` | What to generate — `uuid`, `timestamp`, `int`, or `string` |
| `prefix` | String prepended to the generated value (e.g. `"ret_"` → `"ret_4821"`) |
| `min` | Minimum value for `kind="int"` |
| `max` | Maximum value for `kind="int"` |

**Supported kinds:**

| Kind | Output example |
|------|---------------|
| `uuid` | `"a3f1c2d4-..."` |
| `timestamp` | `"2026-05-23T08:14:00Z"` |
| `int` | `"4821"` (with optional `prefix`, `min`, `max`) |
| `string` | Random alphanumeric string |

**Example — REST response with generated IDs:**

```cue
responseSchema: {
  status: "201"
  mediaType: "application/json"
  body: {
    status:    "approved"
    returnId:  string @gen(kind="int", prefix="ret_", min=1001, max=9999)
    traceId:   string @gen(kind="uuid")
    servedAt:  string @gen(kind="timestamp")
  }
}
```

---

### `@resolve` — read a value from request context

Attach `@resolve` to pull a value from the incoming request or from in-memory mock state at serve time.

```cue
body: {
  previousAttempts: string @resolve(path="state.count")
  profile:          string @resolve(path="request.headers.x-suite-profile", fallback="local.yaml")
}
```

| Parameter | Description |
|-----------|-------------|
| `path` | Dot-separated path into `request` or `state` |
| `fallback` | Value to use when the path resolves to nothing |

**Supported path roots:**

| Root | Description |
|------|-------------|
| `request.headers.<name>` | Value of an incoming request header |
| `request.body.<field>` | Field from the request body |
| `state.<key>` | Value from the operation's in-memory state store |

---

### `@compose` — build a string from mixed static and generated parts

Use `@compose` when the response body is a single string (e.g. XML, plain text) that needs generated values embedded at specific positions.

```cue
body: string @compose(
  "<Response><Id>",
  gen(kind="int", prefix="clm_", min=1001, max=9999),
  "</Id><TraceId>",
  gen(kind="uuid"),
  "</TraceId><ServedAt>",
  gen(kind="timestamp"),
  "</ServedAt></Response>"
)
```

Each argument is either a literal string or a `gen(...)` / `resolve(...)` call. The mock engine concatenates them left to right.

**Example — SOAP response with dynamic claim ID and timestamp:**

```cue
responseSchema: {
  status: "200"
  mediaType: "text/xml; charset=utf-8"
  body: string @compose(
    "<?xml version=\"1.0\"?><Envelope><Body><SubmitClaimResponse>",
    "<ClaimId>clm_", gen(kind="int", min=1001, max=9999), "</ClaimId>",
    "<TraceId>",     gen(kind="uuid"),                    "</TraceId>",
    "<ServedAt>",    gen(kind="timestamp"),               "</ServedAt>",
    "</SubmitClaimResponse></Body></Envelope>"
  )
}
```

---

## Authoring Guidance

- Keep contracts in `api/` and mock artifacts in `mock/`.
- Prefer named exchange examples over giant inline bodies in `suite.star`.
- Use fallback behavior intentionally so the mock surface degrades predictably.
- Use state transitions only when the suite really needs request-driven stateful behavior.
