load("@babelsuite/runtime", "service", "task", "test", "traffic")
load("@babelsuite/kafka",   "kafka", "create_topic")
load("@babelsuite/postgres", "pg", "connect", "insert")

# ── environment knobs ────────────────────────────────────────────────────────
FRAUD_STRATEGY   = env.get("FRAUD_STRATEGY",   "strict")      # strict | permissive | shadow
CURRENCY_MARKETS = env.get("CURRENCY_MARKETS", "usd,eur,gbp").split(",")
REPLICA_COUNT    = int(env.get("FRAUD_REPLICAS", "1"))
ENABLE_CANARY    = env.get("ENABLE_CANARY", "false") == "true"
TRAFFIC_PROFILES = {
    "baseline": {"rps": 50,  "duration": "2m"},
    "stress":   {"rps": 500, "duration": "5m"},
    "soak":     {"rps": 120, "duration": "30m"},
}
ACTIVE_TRAFFIC = env.get("TRAFFIC_PROFILE", "baseline")

# ── infrastructure ───────────────────────────────────────────────────────────
db     = pg()
conn   = connect(after=[db])
broker = kafka()

stripe_mock    = service.mock(after=[conn])
migrations     = task.run(file="migrate.py", image="python:3.12", after=[conn])

# seed merchant and card fixture data
seed_merchants = insert(
    table="merchants",
    rows=[
        {"id": "m-usd", "name": "USD Merchant", "currency": "usd"},
        {"id": "m-eur", "name": "EUR Merchant", "currency": "eur"},
        {"id": "m-gbp", "name": "GBP Merchant", "currency": "gbp"},
    ],
    after=[migrations],
)

# ── per-currency topic bootstrap ─────────────────────────────────────────────
topic_nodes = []
for currency in CURRENCY_MARKETS:
    charge_topic  = create_topic("charges-"  + currency, partitions=6, after=[broker])
    refund_topic  = create_topic("refunds-"  + currency, partitions=3, after=[broker])
    dlq_topic     = create_topic("dlq-"      + currency, partitions=1, after=[broker])
    topic_nodes  += [charge_topic, refund_topic, dlq_topic]

# ── payment gateway ──────────────────────────────────────────────────────────
payment_gateway = service.run(after=[conn, stripe_mock, migrations, seed_merchants])

# ── fraud workers (one per replica) ─────────────────────────────────────────
fraud_workers = []
for i in range(REPLICA_COUNT):
    worker = service.run(
        name="fraud-worker-" + str(i),
        after=topic_nodes + [payment_gateway],
        env={"WORKER_INDEX": str(i), "FRAUD_STRATEGY": FRAUD_STRATEGY},
    )
    fraud_workers.append(worker)

# ── canary gateway (optional) ────────────────────────────────────────────────
if ENABLE_CANARY:
    canary_gateway = service.run(
        name="payment-gateway-canary",
        after=[conn, stripe_mock, migrations],
        env={"FEATURE_FLAGS": "new_checkout_v2=true"},
    )
    traffic_targets = [payment_gateway, canary_gateway]
else:
    traffic_targets = [payment_gateway]

# ── traffic ──────────────────────────────────────────────────────────────────
traffic_profile = TRAFFIC_PROFILES[ACTIVE_TRAFFIC]
traffic_nodes   = []

for target in traffic_targets:
    target_name = target.name if ENABLE_CANARY else "gateway"
    if ACTIVE_TRAFFIC == "baseline":
        t = traffic.baseline(
            name="checkout-baseline-" + target_name,
            target="http://" + target_name + ":8080",
            rps=traffic_profile["rps"],
            after=fraud_workers + [target],
        )
    elif ACTIVE_TRAFFIC == "stress":
        t = traffic.stress(
            name="checkout-stress-" + target_name,
            target="http://" + target_name + ":8080",
            rps=traffic_profile["rps"],
            after=fraud_workers + [target],
        )
    else:
        t = traffic.soak(
            name="checkout-soak-" + target_name,
            target="http://" + target_name + ":8080",
            rps=traffic_profile["rps"],
            after=fraud_workers + [target],
        )
    traffic_nodes.append(t)

# ── smoke tests ───────────────────────────────────────────────────────────────
common_exports = [
    {"path": "reports/junit.xml",         "name": "checkout-test-report", "on": "always", "format": "junit"},
    {"path": "coverage/cobertura.xml",    "name": "checkout-coverage",    "on": "always", "format": "cobertura"},
]

if FRAUD_STRATEGY == "permissive":
    checkout_smoke = test.run(
        file="checkout_smoke.py",
        image="python:3.12",
        after=traffic_nodes,
        exports=common_exports,
    )
elif FRAUD_STRATEGY == "shadow":
    checkout_smoke = test.run(
        file="checkout_smoke.py",
        image="python:3.12",
        after=traffic_nodes,
        env={"ASSERT_SHADOW_DECISIONS": "true"},
        exports=common_exports,
    )
else:
    checkout_smoke = test.run(
        file="checkout_smoke.py",
        image="python:3.12",
        after=traffic_nodes,
        fail_on_logs=["FRAUD_BLOCK", "RISK_THRESHOLD_EXCEEDED"],
        exports=common_exports,
    )

# per-currency settlement reconciliation tests
for currency in CURRENCY_MARKETS:
    test.run(
        name="reconcile-" + currency,
        file="reconcile_smoke.py",
        image="python:3.12",
        after=[checkout_smoke],
        env={"TARGET_CURRENCY": currency},
        exports=[{"path": "reports/reconcile-" + currency + ".xml", "name": "reconcile-" + currency, "on": "always", "format": "junit"}],
    )
