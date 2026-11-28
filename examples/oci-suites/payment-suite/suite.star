load("@babelsuite/runtime", "mock", "service", "test", "traffic", "log")

FRAUD_STRATEGY   = env.get("FRAUD_STRATEGY",   "standard")
CURRENCY_MARKETS = env.get("CURRENCY_MARKETS", "usd,eur,gbp").split(",")
ACTIVE_TRAFFIC   = env.get("TRAFFIC_PROFILE",  "baseline")

if FRAUD_STRATEGY == "shadow":
    log.warn("shadow mode: decisions logged but not enforced")

# ── the card processor is the Stripe mock; the service under test is payment-api ──
stripe_mock = mock.api(name="stripe-mock")

# tasks/payment_api.py: takes a payment, asks the processor (the mock, by its name)
# and keeps the payment, its refunds and a ledger. FRAUD_STRATEGY comes from the
# profile.
payment_api = service.run(
    name  = "payment-api",
    image = "python:3.12-alpine",
    file  = "payment_api.py",
    env   = {"PORT": "8080", "STRIPE_URL": "http://stripe-mock:9080"},
    after = [stripe_mock],
)

# A started container is not an answering service: wait until /health answers, and
# say so if it never does.
api_ready = test.run(
    name     = "payment-api-ready",
    image    = "python:3.12-alpine",
    after    = [payment_api],
    commands = ["for i in $(seq 1 60); do python -c \"import urllib.request; urllib.request.urlopen('http://payment-api:8080/health')\" 2>/dev/null && exit 0; sleep 1; done; echo 'payment-api never answered /health'; exit 1"],
)

infra_ready = log.info("payment-api is answering; stripe-mock is its processor", after=[api_ready])

# ── traffic: real payments through the service, which asks the mock each time ─────
if ACTIVE_TRAFFIC == "stress":
    t = traffic.stress(  name="checkout-stress",   target="http://payment-api:8080", plan="checkout.star", rps=100, after=[infra_ready])
else:
    t = traffic.baseline(name="checkout-baseline", target="http://payment-api:8080", plan="checkout.star", rps=50,  after=[infra_ready])

# ── tests: they call the service and check what it did ──────────────────────────
checkout_smoke = test.run(
    name         = "checkout-smoke",
    image        = "python:3.12-alpine",
    file         = "checkout_smoke.py",
    reset_mocks  = [stripe_mock],
    fail_on_logs = ["Traceback"],
    after        = [t],
    exports      = [{"path": "reports/junit.xml", "name": "checkout-tests", "format": "junit", "on": "always"}],
)

refund_regression = test.run(
    name    = "refund-regression",
    image   = "python:3.12-alpine",
    file    = "refund_regression.py",
    after   = [checkout_smoke],
    exports = [{"path": "reports/refund-regression.xml", "name": "refund-regression", "format": "junit", "on": "always"}],
)

for currency in CURRENCY_MARKETS:
    test.run(
        name    = "reconcile-" + currency,
        image   = "python:3.12-alpine",
        file    = "reconcile_smoke.py",
        after   = [refund_regression],
        env     = {"TARGET_CURRENCY": currency},
        exports = [{"path": "reports/reconcile-" + currency + ".xml", "name": "reconcile-" + currency, "format": "junit", "on": "always"}],
    )
