load("@babelsuite/runtime", "service", "task", "test", "security", "log")

# Level 8 — dedicated security reference
# This suite exists as a clean, focused reference for all 8 security modes.
# The hardware.yaml profile routes verify-findings to a physical USB device
# via services.verify-findings.devices = ["/dev/ttyUSB0"].
#
# The checks scan a real service, `api`, a small API that sends security headers,
# answers 404 for what must not be public, refuses dangerous methods, has no
# GraphQL endpoint, trusts one CORS origin and rate-limits /api/v1/resource.
# Nothing here is hidden behind the gateway: a check passes because the service
# is careful, and fails the day it stops being so. Change tasks/api.py to see it.
#
# security.* steps run inside the suite's APISIX sidecar. A target that is a
# service of the suite is reached at its address on the run's network, so
# target="http://api:8000" is the service itself.

api = service.run(name="api", image="python:3.12-alpine", file="api.py")

api_ready = test.run(
    name     = "api-ready",
    image    = "python:3.12-alpine",
    commands = ["until python -c \"import urllib.request; urllib.request.urlopen('http://api:8000/api')\"; do sleep 1; done"],
    after    = [api],
)

TARGET = "http://api:8000"

log.info("api is up — starting passive surface checks", after=[api_ready])

# ── passive surface checks (no authentication required) ───────────────────────
probe   = security.probe(  name="probe",   target=TARGET, after=[api_ready], exports=[{"path": "/findings/probe.json",   "on": "always"}])
headers = security.headers(name="headers", target=TARGET, after=[api_ready], exports=[{"path": "/findings/headers.json", "on": "always"}])
verbs   = security.verbs(  name="verbs",   target=TARGET, after=[api_ready], exports=[{"path": "/findings/verbs.json",   "on": "always"}])
graphql = security.graphql(name="graphql", target=TARGET, after=[api_ready], exports=[{"path": "/findings/graphql.json", "on": "always"}])
cors    = security.cors(   name="cors",    target=TARGET, after=[api_ready], exports=[{"path": "/findings/cors.json",    "on": "always"}])

passive_done = log.info(
    "passive checks complete (probe, headers, verbs, graphql, cors) — starting active injection",
    after = [probe, headers, verbs, graphql, cors],
)

# ── active injection tests ─────────────────────────────────────────────────────
fuzz = security.fuzz(
    name      = "fuzz",
    technique = "sqli",
    target    = TARGET,
    after     = [passive_done],
    exports   = [{"path": "/findings/fuzz.json", "on": "always"}],
)

auth = security.auth(
    name    = "auth-check",
    target  = TARGET,
    after   = [passive_done],
    exports = [{"path": "/findings/auth.json", "on": "always"}],
)

# ── rate-limit validation ──────────────────────────────────────────────────────
flood = security.flood(
    name            = "flood",
    target          = TARGET,
    path            = "/api/v1/resource",
    rate            = 50.0,
    duration        = 5.0,
    expect_throttle = True,
    after           = [passive_done],
    exports         = [{"path": "/findings/flood.json", "on": "always"}],
)

log.info("all 8 security modes complete — aggregating findings", after=[fuzz, auth, flood])

# verify-findings: reads the report each probe wrote into the workspace and fails
# on any finding at or above SEVERITY_GATE, or on a missing report.
# In the hardware.yaml profile this task gets /dev/ttyUSB0
# so it can relay findings to a physical security audit device
verify = task.run(
    name  = "verify-findings",
    image = "python:3.12-slim",
    file  = "verify_findings.py",
    after = [fuzz, auth, flood, headers, verbs, graphql, cors, probe],
)
