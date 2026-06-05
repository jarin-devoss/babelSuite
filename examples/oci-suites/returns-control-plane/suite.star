load("@babelsuite/runtime", "service", "task", "test", "traffic", "log")

# Level 4 — network.mode, devices in profile, log templates, on_failure, traffic variants
# New: network.mode: execution (containers reach each other by name), devices:
#      in peak profile (GPU for batch seeding), log {{ }} template placeholders,
#      on_failure= rollback branch, traffic.stress + traffic.spike, service.wiremock

ENABLE_FRAUD = env.get("ENABLE_FRAUD_SCREENING", "true") == "true"
REFUND_MODES = env.get("REFUND_MODES", "instant,delayed,manual").split(",")

# ── infrastructure ────────────────────────────────────────────────────────────
db     = service.run(name="db")
broker = service.run(name="broker", after=[db])

# service.wiremock spins up a WireMock instance backed by mock/ stubs
partner_mock = service.wiremock(name="partner-api", after=[db])

seed = task.run(
    name     = "seed-policies",
    image    = "python:3.12",
    commands = ["python seed.py --modes " + ",".join(REFUND_MODES) + " --count 5000"],
    after    = [db],
)

returns_api  = service.run(name="returns-api",  after=[seed, broker, partner_mock])
fraud_worker = service.run(name="fraud-worker", after=[broker, returns_api]) if ENABLE_FRAUD else None

all_services = [returns_api] + ([fraud_worker] if fraud_worker else [])

# log template — {{ healthy }}/{{ total }} resolved at runtime from live graph state
ready = log.info(
    "{{ suite }} on {{ profile }} — {{ healthy }}/{{ total }} nodes healthy",
    after = all_services,
)

# ── traffic — stress then spike ───────────────────────────────────────────────
stress = traffic.stress(name="returns-stress", target="http://returns-api:8080", rps=100, after=[ready])
spike  = traffic.spike( name="returns-spike",  target="http://returns-api:8080", rps=400, after=[stress])

# ── tests with failure path ───────────────────────────────────────────────────
smoke = test.run(
    name        = "refund-smoke",
    image       = "python:3.12",
    file        = "refund_smoke.py",
    expect_logs = "all refund modes validated",
    after       = [spike],
    exports     = [{"path": "reports/junit.xml", "name": "refund-tests", "format": "junit", "on": "always"}],
)

# on_failure= — rollback only runs if smoke fails
rollback = task.run(
    name       = "rollback-policies",
    image      = "python:3.12",
    commands   = ["python rollback.py --reason smoke_failure"],
    on_failure = [smoke],
)

log.info("returns suite complete — {{ healthy }}/{{ total }} healthy", after=[smoke, rollback])
