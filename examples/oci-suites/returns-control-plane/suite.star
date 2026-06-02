load("@babelsuite/runtime", "service", "task", "test", "traffic", "suite", "log")
load("@babelsuite/kafka",    "kafka", "create_topic")
load("@babelsuite/postgres", "pg", "connect", "insert")

# ── environment knobs ────────────────────────────────────────────────────────
REGIONS           = env.get("REGIONS", "eu,us,apac").split(",")
CURRENCIES        = env.get("CURRENCIES", "usd,eur,gbp,jpy").split(",")
REFUND_POLICIES   = env.get("REFUND_POLICIES", "standard,expedited").split(",")
ENABLE_FRAUD_GATE = env.get("ENABLE_FRAUD_GATE", "true") == "true"
ENABLE_SOAK       = env.get("ENABLE_SOAK", "false") == "true"

PAYMENT_SUITE_REF        = env.get("PAYMENT_SUITE_REF",        "localhost:5000/core-platform/payment-suite:stable")
NOTIFICATION_HUB_REF     = env.get("NOTIFICATION_HUB_REF",     "localhost:5000/core-platform/notification-hub:stable")
ENABLE_REFUND_NOTIFY     = env.get("ENABLE_REFUND_NOTIFY",      "true") == "true"

REGION_LIMITS = {
    "eu":   {"max_refund_usd": 5000, "sla_hours": 48},
    "us":   {"max_refund_usd": 10000, "sla_hours": 24},
    "apac": {"max_refund_usd": 3000, "sla_hours": 72},
}

# ── upstream suite dependencies ───────────────────────────────────────────────
# payment-suite owns the charge records that refunds are issued against
payment_suite = suite.run(
    name="payment-suite",
    ref=PAYMENT_SUITE_REF,
)

# notification-hub is pulled in only when refund confirmation notifications are active
if ENABLE_REFUND_NOTIFY:
    notification_hub = suite.run(
        name="notification-hub",
        ref=NOTIFICATION_HUB_REF,
        after=[payment_suite],
    )
    notify_deps = [notification_hub]
else:
    notification_hub = None
    notify_deps      = []

# ── infrastructure ───────────────────────────────────────────────────────────
db     = pg()
conn   = connect(after=[db])
broker = kafka()

# ── migrations and reference data ────────────────────────────────────────────
migrations   = task.run(file="migrate.py",   image="python:3.12", after=[conn])
seed_routes  = task.run(file="seed_routes.ts", image="node:22",   after=[conn, migrations])

# seed currency exchange rates
exchange_rows = [{"from_currency": c, "to_currency": "usd", "rate": 1.0} for c in CURRENCIES]
seed_rates    = insert(table="exchange_rates", rows=exchange_rows, after=[migrations])

# ── mock services ─────────────────────────────────────────────────────────────
refunds_mock = service.mock(name="refunds-mock",  after=[conn])
pricing_mock = service.mock(name="pricing-mock",  after=[conn])
events_mock  = service.mock(name="events-mock",   after=[broker])

# ── optional fraud gate ───────────────────────────────────────────────────────
if ENABLE_FRAUD_GATE:
    fraud_mock = service.mock(name="fraud-gate-mock", after=[conn])
    api_deps   = [refunds_mock, pricing_mock, seed_routes, seed_rates, fraud_mock]
else:
    fraud_mock = None
    api_deps   = [refunds_mock, pricing_mock, seed_routes, seed_rates]

# ── per-region topic bootstrap ────────────────────────────────────────────────
region_topics = {}
for region in REGIONS:
    topics = []
    for policy in REFUND_POLICIES:
        t = create_topic("returns-" + region + "-" + policy, partitions=3, after=[broker])
        topics.append(t)
    dlq = create_topic("returns-" + region + "-dlq", partitions=1, after=[broker])
    topics.append(dlq)
    region_topics[region] = topics

all_topics = [t for topics in region_topics.values() for t in topics]

# ── returns API ───────────────────────────────────────────────────────────────
topics_ready = log.info(
    "region topics ready — " + str(len(all_topics)) + " partitions across " + str(len(REGIONS)) + " regions",
    after=all_topics,
)
returns_api = service.run(
    after=api_deps + [payment_suite, topics_ready] + notify_deps,
    env={
        "ENABLED_REGIONS":   ",".join(REGIONS),
        "FRAUD_GATE_ENABLED": str(ENABLE_FRAUD_GATE).lower(),
    },
)

# ── per-region refund workers ─────────────────────────────────────────────────
refund_workers = []
for region in REGIONS:
    limits = REGION_LIMITS.get(region, {"max_refund_usd": 1000, "sla_hours": 72})
    worker = service.run(
        name="refund-worker-" + region,
        after=[broker, returns_api, events_mock] + region_topics[region],
        env={
            "REGION":           region,
            "MAX_REFUND_USD":   str(limits["max_refund_usd"]),
            "SLA_HOURS":        str(limits["sla_hours"]),
        },
    )
    refund_workers.append(worker)

# ── traffic ───────────────────────────────────────────────────────────────────
traffic_nodes = []
for policy in REFUND_POLICIES:
    t = traffic.baseline(
        name="returns-baseline-" + policy,
        target="http://returns-api:8080",
        after=refund_workers,
        env={"REFUND_POLICY": policy},
    )
    traffic_nodes.append(t)

if ENABLE_SOAK:
    traffic.soak(
        name="returns-soak",
        target="http://returns-api:8080",
        after=traffic_nodes,
    )

# ── smoke tests ───────────────────────────────────────────────────────────────
returns_smoke = test.run(
    file="returns_smoke.py",
    image="python:3.12",
    after=traffic_nodes,
    fail_on_logs=["REFUND_LIMIT_EXCEEDED", "INVALID_CURRENCY", "SLA_BREACH"],
    exports=[
        {"path": "reports/returns-junit.xml", "name": "returns-smoke",    "on": "always", "format": "junit"},
        {"path": "reports/returns-coverage.xml", "name": "returns-coverage", "on": "always", "format": "cobertura"},
    ],
)

# ── per-region compliance checks ──────────────────────────────────────────────
for region in REGIONS:
    limits = REGION_LIMITS.get(region, {"max_refund_usd": 1000, "sla_hours": 72})
    test.run(
        name="compliance-" + region,
        file="compliance_smoke.py",
        image="python:3.12",
        after=[returns_smoke],
        env={
            "REGION":         region,
            "MAX_REFUND_USD": str(limits["max_refund_usd"]),
            "SLA_HOURS":      str(limits["sla_hours"]),
        },
        exports=[{"path": "reports/compliance-" + region + ".xml", "name": "compliance-" + region, "on": "always", "format": "junit"}],
    )

# ── fraud gate audit (only when enabled) ─────────────────────────────────────
if ENABLE_FRAUD_GATE:
    test.run(
        name="fraud-gate-audit",
        file="fraud_gate_audit.py",
        image="python:3.12",
        after=[returns_smoke],
        fail_on_logs=["FRAUD_GATE_BYPASS", "UNREVIEWED_HIGH_VALUE"],
    )
