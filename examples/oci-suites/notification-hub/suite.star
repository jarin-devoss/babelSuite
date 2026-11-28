load("@babelsuite/runtime", "mock", "service", "test", "suite", "log")

# ── environment knobs ────────────────────────────────────────────────────────
CHANNELS          = env.get("CHANNELS", "email,sms,push,webhook").split(",")
LOCALES           = env.get("LOCALES", "en,de,fr,ja").split(",")
ENABLE_RATE_LIMIT = env.get("ENABLE_RATE_LIMIT", "true") == "true"
ENABLE_DLQ        = env.get("ENABLE_DLQ",        "true") == "true"
RETRY_POLICY      = env.get("RETRY_POLICY",      "exponential")  # linear | exponential | none
ENABLE_PAYMENT_NOTIFICATIONS = env.get("ENABLE_PAYMENT_NOTIFICATIONS", "true") == "true"

# suites this topology depends on
IDENTITY_BROKER_REF = env.get("IDENTITY_BROKER_REF", "identity-broker")
PAYMENT_SUITE_REF   = env.get("PAYMENT_SUITE_REF",   "payment-suite")

PYTHON = "python:3.12-alpine"
TEMPLATE_CATEGORIES = ["transactional", "marketing", "alert", "digest"]

# ── upstream suites ──────────────────────────────────────────────────────────
# identity-broker's broker-api vouches for the callers of notify-api; its mocks
# (the identity providers) are served by this run's gateway like our own.
identity_broker = suite.run(
    name = "identity-broker",
    ref  = IDENTITY_BROKER_REF,
)

# payment-suite's payment-api is where the payment events come from.
if ENABLE_PAYMENT_NOTIFICATIONS:
    payment_suite = suite.run(
        name  = "payment-suite",
        ref   = PAYMENT_SUITE_REF,
        after = [identity_broker],
    )
    upstream_suites = [identity_broker, payment_suite]
else:
    payment_suite   = None
    upstream_suites = [identity_broker]

# ── channel providers: one mock per channel, each answering to its own name ──
channel_mocks = {}
for channel in CHANNELS:
    channel_mocks[channel] = mock.api(name = channel + "-provider-mock")
all_mocks = list(channel_mocks.values())

# ── the service under test ───────────────────────────────────────────────────
# tasks/notify_api.py renders a template for a locale, sends it to the channel's
# provider, keeps what was delivered, and dead-letters what a provider refuses.
notify_api = service.run(
    name  = "notify-api",
    image = PYTHON,
    file  = "notify_api.py",
    env   = {
        "PORT":              "8080",
        "CHANNELS":          ",".join(CHANNELS),
        "LOCALES":           ",".join(LOCALES),
        "RETRY_POLICY":      RETRY_POLICY,
        "ENABLE_DLQ":        "true" if ENABLE_DLQ else "false",
        "ENABLE_RATE_LIMIT": "true" if ENABLE_RATE_LIMIT else "false",
        "BROKER_URL":        "http://broker-api:8080",
        "PAYMENTS_URL":      "http://payment-api:8080",
    },
    after = all_mocks + upstream_suites,
)

notify_ready = test.run(
    name     = "notify-api-ready",
    image    = PYTHON,
    after    = [notify_api],
    commands = ["for i in $(seq 1 60); do python -c \"import urllib.request; urllib.request.urlopen('http://notify-api:8080/health')\" 2>/dev/null && exit 0; sleep 1; done; echo 'notify-api never answered /health'; exit 1"],
)

channels_ready = log.info(str(len(CHANNELS)) + " channel providers up; notify-api is answering", after=[notify_ready])

# ── smoke tests — one per channel ────────────────────────────────────────────
smoke_nodes = []
for channel in CHANNELS:
    smoke_nodes.append(test.run(
        name    = "notify-smoke-" + channel,
        file    = "notify_smoke.py",
        image   = PYTHON,
        after   = [channels_ready],
        env     = {"CHANNEL": channel, "DLQ_ENABLED": "true" if ENABLE_DLQ else "false"},
        exports = [{"path": "reports/" + channel + "-junit.xml", "name": "notify-smoke-" + channel, "on": "always", "format": "junit"}],
    ))

# ── locale rendering tests ───────────────────────────────────────────────────
for locale in LOCALES:
    test.run(
        name    = "template-render-" + locale,
        file    = "template_render.py",
        image   = PYTHON,
        after   = smoke_nodes,
        env     = {"LOCALE": locale, "CATEGORIES": ",".join(TEMPLATE_CATEGORIES)},
        exports = [{"path": "reports/render-" + locale + ".xml", "name": "render-" + locale, "on": "always", "format": "junit"}],
    )

# ── DLQ audit (only when the DLQ is on) ──────────────────────────────────────
if ENABLE_DLQ and "sms" in CHANNELS:
    test.run(
        name         = "dlq-drain-audit",
        file         = "dlq_audit.py",
        image        = PYTHON,
        after        = smoke_nodes,
        fail_on_logs = ["DLQ_OVERFLOW", "UNPARSEABLE_MESSAGE"],
        exports      = [{"path": "reports/dlq-audit.xml", "name": "dlq-audit", "on": "always", "format": "junit"}],
    )

# ── rate limit boundary ──────────────────────────────────────────────────────
if ENABLE_RATE_LIMIT and "push" in CHANNELS:
    test.run(
        name         = "rate-limit-boundary",
        file         = "rate_limit_boundary.py",
        image        = PYTHON,
        after        = smoke_nodes,
        fail_on_logs = ["LIMIT_NOT_ENFORCED"],
        exports      = [{"path": "reports/rate-limit.xml", "name": "rate-limit", "on": "always", "format": "junit"}],
    )

# ── payment events (only when the payment suite is loaded) ───────────────────
if ENABLE_PAYMENT_NOTIFICATIONS and payment_suite != None and "email" in CHANNELS:
    for event_type in ["charge.succeeded", "charge.failed", "refund.created", "dispute.opened"]:
        safe_name = event_type.replace(".", "-")
        test.run(
            name         = "payment-notify-" + safe_name,
            file         = "payment_notify_smoke.py",
            image        = PYTHON,
            after        = smoke_nodes,
            env          = {"PAYMENT_EVENT_TYPE": event_type},
            fail_on_logs = ["EVENT_DROPPED", "ROUTING_FAILED"],
            exports      = [{"path": "reports/payment-notify-" + safe_name + ".xml", "name": "payment-notify-" + safe_name, "on": "always", "format": "junit"}],
        )
