load("@babelsuite/runtime", "service", "task", "test", "traffic", "log")

# ── Level 3: adds file=, profile extendsId inheritance, network.mode ─────────
# New here: file= resolves scripts from the suite OCI artifact (tasks/ tests/).
# Staging profile extends base, adds network.mode: execution so containers
# reach each other by name (e.g. payment-gateway, db, stripe-mock).

FRAUD_STRATEGY   = env.get("FRAUD_STRATEGY",   "standard")
CURRENCY_MARKETS = env.get("CURRENCY_MARKETS", "usd,eur,gbp").split(",")
REPLICA_COUNT    = int(env.get("FRAUD_REPLICAS", "1"))
ACTIVE_TRAFFIC   = env.get("TRAFFIC_PROFILE",  "baseline")

# ── infrastructure ────────────────────────────────────────────────────────────
db          = service.run(name="db")
stripe_mock = service.mock(name="stripe-mock", after=[db])

migrate = task.run(
    name  = "migrate",
    image = "python:3.12",
    file  = "migrate.py",
    after = [db],
)

seed = task.run(
    name  = "seed",
    image = "bash:5.2",
    file  = "seed.sh",
    after = [migrate],
)

infra_ready = log.info(
    "db migrated and seeded — stripe mock up, starting gateway",
    after = [seed, stripe_mock],
)

payment_gateway = service.run(name="payment-gateway", after=[infra_ready])

# ── fraud workers (one per replica) ──────────────────────────────────────────
fraud_workers = []
for i in range(REPLICA_COUNT):
    worker = service.run(
        name  = "fraud-worker-" + str(i),
        after = [payment_gateway],
        env   = {"WORKER_INDEX": str(i), "FRAUD_STRATEGY": FRAUD_STRATEGY},
    )
    fraud_workers.append(worker)

# ── traffic ───────────────────────────────────────────────────────────────────
if ACTIVE_TRAFFIC == "stress":
    t = traffic.stress(name="checkout-stress", target="http://payment-gateway:8080", rps=200, after=fraud_workers)
else:
    t = traffic.baseline(name="checkout-baseline", target="http://payment-gateway:8080", rps=50, after=fraud_workers)

# ── tests ─────────────────────────────────────────────────────────────────────
checkout_smoke = test.run(
    name            = "checkout-smoke",
    image           = "python:3.12",
    file            = "checkout_smoke.py",
    after           = [t],
    fail_on_logs    = ["FRAUD_BLOCK", "RISK_THRESHOLD_EXCEEDED"] if FRAUD_STRATEGY == "strict" else [],
    exports         = [
        {"path": "reports/junit.xml",      "name": "checkout-tests",   "format": "junit",     "on": "always"},
        {"path": "coverage/cobertura.xml", "name": "checkout-coverage","format": "cobertura",  "on": "always"},
    ],
)

for currency in CURRENCY_MARKETS:
    test.run(
        name    = "reconcile-" + currency,
        image   = "python:3.12",
        file    = "reconcile_smoke.py",
        after   = [checkout_smoke],
        env     = {"TARGET_CURRENCY": currency},
        exports = [{"path": "reports/reconcile-" + currency + ".xml", "name": "reconcile-" + currency, "format": "junit", "on": "always"}],
    )
