load("@babelsuite/runtime", "service", "task", "security")

# Mock the API under test — the APISIX sidecar is provisioned automatically
# alongside every mock node, so all security steps derive their gateway URL
# from it without needing an explicit target= argument.
api = service.mock(name="api")

# --- Passive surface checks (no authentication required) ---

probe = security.probe(
    name = "probe",
    after = [api],
    exports = [{"path": "/findings/probe.json", "on": "always"}],
)

headers_audit = security.headers(
    name = "headers-audit",
    after = [api],
    exports = [{"path": "/findings/headers.json", "on": "always"}],
)

verbs_check = security.verbs(
    name = "verbs-check",
    after = [api],
    exports = [{"path": "/findings/verbs.json", "on": "always"}],
)

graphql_introspection = security.graphql(
    name = "graphql-introspection",
    after = [api],
    exports = [{"path": "/findings/graphql.json", "on": "always"}],
)

cors_audit = security.cors(
    name = "cors-audit",
    after = [api],
    exports = [{"path": "/findings/cors.json", "on": "always"}],
)

# --- Active injection tests ---

fuzz = security.fuzz(
    name = "fuzz",
    technique = "sqli",
    after = [probe],
    exports = [{"path": "/findings/fuzz.json", "on": "always"}],
)

auth_check = security.auth(
    name = "auth-check",
    after = [api],
    exports = [{"path": "/findings/auth.json", "on": "always"}],
)

# --- Rate-limit / throttle validation ---

flood = security.flood(
    name = "flood",
    path = "/api/v1/resource",
    rate = 50.0,
    duration = 5.0,
    expect_throttle = True,
    after = [api],
    exports = [{"path": "/findings/flood.json", "on": "always"}],
)

# --- Aggregate verification ---

verify = task.run(
    name = "verify-findings",
    image = "python:3.12-slim",
    file = "verify_findings.py",
    after = [probe, headers_audit, verbs_check, graphql_introspection, cors_audit, fuzz, auth_check, flood],
)
