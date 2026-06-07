load("@babelsuite/runtime",  "service", "task", "test", "traffic", "log")
load("@babelsuite/postgres", "pg", "connect")
load("@babelsuite/kafka",    "kafka", "create_topic")


ENABLE_FRAUD = env.get("ENABLE_FRAUD_SCREENING", "true") == "true"
REFUND_MODES = env.get("REFUND_MODES", "instant,delayed,manual").split(",")

# ── infrastructure ────────────────────────────────────────────────────────────
db     = pg()
conn   = connect(db)
broker = kafka(after=[conn])

returns_topic = create_topic(broker, "returns.events", partitions=3)

partner_mock = service.mock(name="partner-api", after=[conn])

seed = task.run(
    name     = "seed-policies",
    image    = "python:3.12",
    commands = ["python -c \"import sys; modes = '" + ",".join(REFUND_MODES) + "'.split(','); print('seeded', 5000, 'policies for modes:', modes)\""],
    after    = [conn],
)

returns_api  = service.run(name="returns-api",  after=[seed, returns_topic, partner_mock])
fraud_worker = service.run(name="fraud-worker", after=[returns_topic, returns_api]) if ENABLE_FRAUD else None

all_services = [returns_api] + ([fraud_worker] if fraud_worker else [])

# log template — {{ healthy }}/{{ total }} resolved at runtime from live graph state
ready = log.info(
    "{{ suite }} on {{ profile }} — {{ healthy }}/{{ total }} nodes healthy",
    after = all_services,
)

# ── traffic — stress then spike ───────────────────────────────────────────────
stress = traffic.stress(name="returns-stress", target="http://returns-api:8080", rps=100, after=[ready])
spike  = traffic.spike( name="returns-spike",  target="http://returns-api:8080", rps=80,  after=[stress])

# ── tests with failure path ───────────────────────────────────────────────────
smoke = test.run(
    name                = "refund-smoke",
    image               = "python:3.12",
    file                = "refund_smoke.py",
    continue_on_failure = True,
    after               = [spike],
    exports             = [{"path": "reports/junit.xml", "name": "refund-tests", "format": "junit", "on": "always"}],
)

# on_failure= — rollback only runs if smoke fails
rollback = task.run(
    name       = "rollback-policies",
    image      = "python:3.12",
    commands   = ["python -c \"print('rollback complete: smoke_failure')\""],
    on_failure = [smoke],
)

log.info("returns suite complete — {{ healthy }}/{{ total }} healthy", after=[smoke, rollback])
