load("@babelsuite/runtime", "service", "task", "security", "log")

# Level 8 — dedicated security reference
# This suite exists as a clean, focused reference for all 8 security modes.
# The hardware.yaml profile routes verify-findings to a physical USB device
# via services.verify-findings.devices = ["/dev/ttyUSB0"].
# The mock is set up with network.mode: execution so the APISIX sidecar
# can reach the mock API by name.

api = service.mock(name="api")

log.info("APISIX sidecar provisioned — starting passive surface checks", after=[api])

# ── passive surface checks (no authentication required) ───────────────────────
probe   = security.probe(  name="probe",   after=[api], exports=[{"path": "/findings/probe.json",   "on": "always"}])
headers = security.headers(name="headers", after=[api], exports=[{"path": "/findings/headers.json", "on": "always"}])
verbs   = security.verbs(  name="verbs",   after=[api], exports=[{"path": "/findings/verbs.json",   "on": "always"}])
graphql = security.graphql(name="graphql", after=[api], exports=[{"path": "/findings/graphql.json", "on": "always"}])
cors    = security.cors(   name="cors",    after=[api], exports=[{"path": "/findings/cors.json",    "on": "always"}])

passive_done = log.info(
    "passive checks complete (probe, headers, verbs, graphql, cors) — starting active injection",
    after = [probe, headers, verbs, graphql, cors],
)

# ── active injection tests ─────────────────────────────────────────────────────
fuzz = security.fuzz(
    name      = "fuzz",
    technique = "sqli",
    after     = [passive_done],
    exports   = [{"path": "/findings/fuzz.json", "on": "always"}],
)

auth = security.auth(
    name    = "auth-check",
    after   = [passive_done],
    exports = [{"path": "/findings/auth.json", "on": "always"}],
)

# ── rate-limit validation ──────────────────────────────────────────────────────
flood = security.flood(
    name            = "flood",
    path            = "/api/v1/resource",
    rate            = 50.0,
    duration        = 5.0,
    expect_throttle = True,
    after           = [passive_done],
    exports         = [{"path": "/findings/flood.json", "on": "always"}],
)

log.info("all 8 security modes complete — aggregating findings", after=[fuzz, auth, flood])

# verify-findings: in hardware.yaml profile this task gets /dev/ttyUSB0
# so it can relay findings to a physical security audit device
verify = task.run(
    name  = "verify-findings",
    image = "python:3.12-slim",
    file  = "verify_findings.py",
    after = [fuzz, auth, flood, headers, verbs, graphql, cors],
)
