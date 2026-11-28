load("@babelsuite/runtime", "mock", "service", "test", "traffic", "log")

PYTHON = "python:3.12-alpine"

# ── what the refund service depends on, both mocks ───────────────────────────
# partner-api: the carrier/partner the return is registered with (REST).
# pricing-service: quotes a refund over gRPC; the gateway serves it from the
# suite's proto (api/proto/refund_pricing.proto).
partner_mock = mock.api(name="partner-api")

# ── the service under test ───────────────────────────────────────────────────
# tasks/refund_worker.py takes a return, asks the pricing service for a quote,
# registers the return with the partner, and holds back any refund above
# REFUND_LIMIT_CENTS for manual review.
refund_worker = service.run(
    name  = "refund-worker",
    image = PYTHON,
    file  = "refund_worker.py",
    env   = {
        "PORT":                "8080",
        "GATEWAY_URL":         "http://babelsuite-gateway:9080",
        "PARTNER_URL":         "http://partner-api:9080",
    },
    after = [partner_mock],
)

worker_ready = test.run(
    name     = "refund-worker-ready",
    image    = PYTHON,
    after    = [refund_worker],
    commands = ["for i in $(seq 1 60); do python -c \"import urllib.request; urllib.request.urlopen('http://refund-worker:8080/health')\" 2>/dev/null && exit 0; sleep 1; done; echo 'refund-worker never answered /health'; exit 1"],
)

# log template — {{ healthy }}/{{ total }} resolved at runtime from live graph state
ready = log.info(
    "{{ suite }} on {{ profile }} — {{ healthy }}/{{ total }} nodes healthy",
    after = [worker_ready],
)

# ── traffic — real returns through the service, which quotes and registers each ──
stress = traffic.stress(name="returns-stress", target="http://refund-worker:8080", plan="returns.star", rps=100, after=[ready])
spike  = traffic.spike( name="returns-spike",  target="http://refund-worker:8080", plan="returns.star", rps=80,  after=[stress])

# ── tests ────────────────────────────────────────────────────────────────────
returns_smoke = test.run(
    name    = "returns-smoke",
    image   = PYTHON,
    file    = "returns_smoke.py",
    after   = [spike],
    exports = [{"path": "reports/returns-smoke.xml", "name": "returns-smoke", "format": "junit", "on": "always"}],
)

smoke = test.run(
    name                = "refund-smoke",
    image               = PYTHON,
    file                = "refund_smoke.py",
    after               = [returns_smoke],
    exports             = [{"path": "reports/junit.xml", "name": "refund-tests", "format": "junit", "on": "always"}],
)

# on_failure= — rollback only runs if the smoke test fails
rollback = test.run(
    name       = "rollback-returns",
    image      = PYTHON,
    commands   = ["echo 'a refund test failed: returns made by this run are test data and are discarded with the run'"],
    on_failure = [smoke],
)

done = log.info("returns suite complete — {{ healthy }}/{{ total }} healthy", after=[smoke, rollback])
