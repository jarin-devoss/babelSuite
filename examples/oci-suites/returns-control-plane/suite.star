load("@babelsuite/runtime", "service", "task", "test", "traffic", "log")

# ── Level 4: adds devices: in profile, log template placeholders, on_failure ──
# New here: services.<name>.devices in the peak profile gives the seed task
# a GPU for batch processing. Log template uses {{ healthy }}/{{ total }}.
# on_failure=[primary] triggers a rollback task when the smoke test fails.

ENABLE_FRAUD = env.get("ENABLE_FRAUD_SCREENING", "true") == "true"
REFUND_MODES = env.get("REFUND_MODES", "instant,delayed,manual").split(",")

# ── infrastructure ────────────────────────────────────────────────────────────
db     = service.run(name="db")
broker = service.run(name="broker", after=[db])

seed = task.run(
    name     = "seed-policies",
    image    = "python:3.12",
    commands = ["python seed.py --modes " + ",".join(REFUND_MODES) + " --count 5000"],
    after    = [db],
)

returns_api  = service.run(name="returns-api",  after=[seed, broker])
fraud_worker = service.run(name="fraud-worker", after=[broker, returns_api]) if ENABLE_FRAUD else None

all_services = [returns_api] + ([fraud_worker] if fraud_worker else [])

ready = log.info(
    "{{ suite }} on {{ profile }} — {{ healthy }}/{{ total }} nodes healthy",
    after = all_services,
)

# ── traffic ───────────────────────────────────────────────────────────────────
load_test = traffic.baseline(
    name   = "returns-load",
    target = "http://returns-api:8080",
    rps    = 40,
    after  = [ready],
)

# ── tests with failure path ───────────────────────────────────────────────────
smoke = test.run(
    name         = "refund-smoke",
    image        = "python:3.12",
    file         = "refund_smoke.py",
    expect_logs  = "all refund modes validated",
    after        = [load_test],
    exports      = [{"path": "reports/junit.xml", "name": "refund-tests", "format": "junit", "on": "always"}],
)

rollback = task.run(
    name        = "rollback-policies",
    image       = "python:3.12",
    commands    = ["python rollback.py --reason smoke_failure"],
    on_failure  = [smoke],
)

log.info("returns suite complete", after=[smoke, rollback])
