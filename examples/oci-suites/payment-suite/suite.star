load("@babelsuite/runtime",  "service", "task", "test", "traffic", "log")
load("@babelsuite/postgres", "pg", "connect")


FRAUD_STRATEGY   = env.get("FRAUD_STRATEGY",   "standard")
CURRENCY_MARKETS = env.get("CURRENCY_MARKETS", "usd,eur,gbp").split(",")
REPLICA_COUNT    = int(env.get("FRAUD_REPLICAS", "1"))
ACTIVE_TRAFFIC   = env.get("TRAFFIC_PROFILE",  "baseline")

if FRAUD_STRATEGY == "shadow":
    log.warn("shadow mode: decisions logged but not enforced — check fraud-worker logs")

# ── infrastructure ────────────────────────────────────────────────────────────
db   = pg()
conn = connect(db)

stripe_mock = service.mock(name="stripe-mock", after=[conn])

migrate = task.run(name="migrate", image="python:3.12", file="migrate.py", after=[conn])
seed    = task.run(name="seed",    image="bash:5.2",    file="seed.sh",    after=[migrate])

infra_ready = log.info("db migrated and seeded — starting gateway", after=[seed, stripe_mock])

payment_gateway = service.run(name="payment-gateway", after=[infra_ready])

fraud_workers = []
for i in range(REPLICA_COUNT):
    w = service.run(
        name  = "fraud-worker-" + str(i),
        after = [payment_gateway],
        env   = {"WORKER_INDEX": str(i), "FRAUD_STRATEGY": FRAUD_STRATEGY},
    )
    fraud_workers.append(w)

# ── traffic ───────────────────────────────────────────────────────────────────
if ACTIVE_TRAFFIC == "stress":
    t = traffic.stress(  name="checkout-stress",   target="http://payment-gateway:8080", rps=200, after=fraud_workers)
else:
    t = traffic.baseline(name="checkout-baseline", target="http://payment-gateway:8080", rps=50,  after=fraud_workers)

# ── tests — reset_mocks clears stripe-mock state before each test run ─────────
checkout_smoke = test.run(
    name         = "checkout-smoke",
    image        = "python:3.12",
    file         = "checkout_smoke.py",
    reset_mocks  = [stripe_mock],
    fail_on_logs = ["FRAUD_BLOCK", "RISK_THRESHOLD_EXCEEDED"] if FRAUD_STRATEGY == "strict" else [],
    after        = [t],
    exports      = [
        {"path": "reports/junit.xml",      "name": "checkout-tests",    "format": "junit",     "on": "always"},
        {"path": "coverage/cobertura.xml", "name": "checkout-coverage", "format": "cobertura", "on": "always"},
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
