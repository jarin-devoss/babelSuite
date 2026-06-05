load("@babelsuite/runtime", "service", "task", "test", "security", "log")

# ── Level 6: adds security.* nodes ───────────────────────────────────────────
# New here: security.probe, .fuzz, .headers, .flood run against the APISIX
# sidecar (provisioned automatically beside every service.mock node).
# The hardware.yaml profile routes verify-findings to a physical USB device.

CLAIM_TYPES = env.get("CLAIM_TYPES", "medical,dental,vision,pharmacy").split(",")
LEGACY_MODE = env.get("LEGACY_MODE", "true") == "true"

# ── infrastructure ────────────────────────────────────────────────────────────
db = service.run(name="db")

adapter   = service.run(name="soap-adapter", after=[db]) if LEGACY_MODE else None
after_infra = [db] + ([adapter] if adapter else [])

claims_api = service.mock(name="claims-api", after=after_infra)

seed = task.run(
    name     = "seed-claims",
    image    = "python:3.12",
    commands = ["python seed.py --types " + ",".join(CLAIM_TYPES) + " --count 500"],
    after    = [db],
)

ready = log.info(
    "{{ suite }} on {{ profile }} — {{ healthy }}/{{ total }} healthy, CLAIM_TYPES={{ env.CLAIM_TYPES }}",
    after = [claims_api, seed],
)

# ── security scan ─────────────────────────────────────────────────────────────
probe   = security.probe(  name="probe",        after=[ready], exports=[{"path": "/findings/probe.json",   "on": "always"}])
headers = security.headers(name="headers-audit", after=[ready], exports=[{"path": "/findings/headers.json", "on": "always"}])
fuzz    = security.fuzz(   name="fuzz",          after=[probe],  technique="sqli", exports=[{"path": "/findings/fuzz.json", "on": "always"}])
flood   = security.flood(  name="flood",         after=[ready],  path="/api/claims", rate=20.0, duration=5.0, expect_throttle=True)

scan_done = log.info("security scan complete", after=[fuzz, flood, headers])

# ── functional test ───────────────────────────────────────────────────────────
smoke = test.run(
    name        = "claims-smoke",
    image       = "python:3.12",
    file        = "claims_smoke.py",
    expect_logs = "all claims validated",
    after       = [scan_done],
    exports     = [{"path": "reports/junit.xml", "name": "claims-tests", "format": "junit", "on": "always"}],
)

verify = task.run(
    name  = "verify-findings",
    image = "python:3.12-slim",
    file  = "verify_findings.py",
    after = [smoke, fuzz, flood, headers],
)

task.run(
    name       = "purge-test-claims",
    image      = "python:3.12",
    commands   = ["python purge.py --test-only"],
    on_failure = [smoke],
)
