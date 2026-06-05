load("@babelsuite/runtime", "service", "task", "test", "security", "traffic", "log")

# Level 6 — all 8 security modes, traffic.scalability, log.error, hardware profile
# New: security.probe/fuzz/auth/flood/headers/verbs/graphql/cors (all 8 modes),
#      traffic.scalability, log.error, hardware profile routes verify task
#      to a physical USB-connected device via services.<name>.devices

CLAIM_TYPES = env.get("CLAIM_TYPES", "medical,dental,vision,pharmacy").split(",")
LEGACY_MODE = env.get("LEGACY_MODE", "true") == "true"

# ── infrastructure ────────────────────────────────────────────────────────────
db      = service.run(name="db")
adapter = service.run(name="soap-adapter", after=[db]) if LEGACY_MODE else None

after_infra = [db] + ([adapter] if adapter else [])
claims_api  = service.mock(name="claims-api", after=after_infra)

if LEGACY_MODE:
    log.error("LEGACY_MODE is enabled — soap-adapter is active; expect slower claim processing")

seed = task.run(
    name     = "seed-claims",
    image    = "python:3.12",
    commands = ["python seed.py --types " + ",".join(CLAIM_TYPES) + " --count 500"],
    after    = [db],
)

ready = log.info(
    "{{ suite }} on {{ profile }} — {{ healthy }}/{{ total }} healthy, types={{ env.CLAIM_TYPES }}",
    after = [claims_api, seed],
)

# ── all 8 security modes ──────────────────────────────────────────────────────
probe    = security.probe(   name="probe",     after=[ready],  exports=[{"path": "/findings/probe.json",    "on": "always"}])
headers  = security.headers( name="headers",   after=[ready],  exports=[{"path": "/findings/headers.json",  "on": "always"}])
verbs    = security.verbs(   name="verbs",     after=[ready],  exports=[{"path": "/findings/verbs.json",    "on": "always"}])
cors     = security.cors(    name="cors",      after=[ready],  exports=[{"path": "/findings/cors.json",     "on": "always"}])
graphql  = security.graphql( name="graphql",   after=[ready],  exports=[{"path": "/findings/graphql.json",  "on": "always"}])
auth     = security.auth(    name="auth",      after=[probe],  exports=[{"path": "/findings/auth.json",     "on": "always"}])
fuzz     = security.fuzz(    name="fuzz",      after=[probe],  technique="sqli", exports=[{"path": "/findings/fuzz.json", "on": "always"}])
flood    = security.flood(   name="flood",     after=[ready],  path="/api/claims", rate=20.0, duration=5.0, expect_throttle=True,
                             exports=[{"path": "/findings/flood.json", "on": "always"}])

scan_done = log.info("all 8 security modes complete", after=[fuzz, flood, auth, headers, verbs, cors, graphql])

# ── scalability probe — finds max sustainable RPS before thresholds break ────
scale = traffic.scalability(
    name   = "claims-scale",
    target = "http://claims-api:8080",
    after  = [scan_done],
)

# ── functional test ───────────────────────────────────────────────────────────
smoke = test.run(
    name        = "claims-smoke",
    image       = "python:3.12",
    file        = "claims_smoke.py",
    expect_logs = "all claims validated",
    after       = [scale],
    exports     = [{"path": "reports/junit.xml", "name": "claims-tests", "format": "junit", "on": "always"}],
)

# verify-findings — in hardware.yaml profile this task gets /dev/ttyUSB0 via
# services.verify-findings.devices so it can probe the physical claims device
verify = task.run(
    name  = "verify-findings",
    image = "python:3.12-slim",
    file  = "verify_findings.py",
    after = [smoke, fuzz, flood, headers],
)

rollback = task.run(
    name       = "purge-test-claims",
    image      = "python:3.12",
    commands   = ["python purge.py --test-only"],
    on_failure = [smoke],
)

log.info("assessment complete — findings written to /findings/", after=[verify, rollback])
