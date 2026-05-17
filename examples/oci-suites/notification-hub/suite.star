load("@babelsuite/runtime", "service", "task", "test")
load("@babelsuite/kafka",   "kafka", "create_topic")
load("@babelsuite/postgres", "pg", "connect", "insert")

# ── environment knobs ────────────────────────────────────────────────────────
CHANNELS         = env.get("CHANNELS", "email,sms,push,webhook").split(",")
LOCALES          = env.get("LOCALES", "en,de,fr,ja").split(",")
ENABLE_RATE_LIMIT = env.get("ENABLE_RATE_LIMIT", "true") == "true"
ENABLE_DLQ        = env.get("ENABLE_DLQ",        "true") == "true"
RETRY_POLICY      = env.get("RETRY_POLICY",      "exponential")  # linear | exponential | none

CHANNEL_CONFIGS = {
    "email":   {"partitions": 6,  "mock_def": "mock/email",   "image": "node:22"},
    "sms":     {"partitions": 4,  "mock_def": "mock/sms",     "image": "python:3.12"},
    "push":    {"partitions": 8,  "mock_def": "mock/push",    "image": "node:22"},
    "webhook": {"partitions": 3,  "mock_def": "mock/webhook",  "image": "python:3.12"},
}

TEMPLATE_CATEGORIES = ["transactional", "marketing", "alert", "digest"]

# ── infrastructure ────────────────────────────────────────────────────────────
db     = pg()
conn   = connect(after=[db])
cache  = service.run(name="cache", after=[])
broker = kafka()

migrations = task.run(file="migrate.sh", image="bash:5.2", after=[conn])

# seed templates for all active locales × categories
template_rows = []
for locale in LOCALES:
    for category in TEMPLATE_CATEGORIES:
        template_rows.append({
            "id":       locale + "-" + category,
            "locale":   locale,
            "category": category,
            "active":   True,
        })

seed_templates = insert(table="notification_templates", rows=template_rows, after=[migrations])

# ── per-channel provider mocks and topics ─────────────────────────────────────
channel_mocks  = {}
channel_topics = {}

for channel in CHANNELS:
    if channel not in CHANNEL_CONFIGS:
        continue
    cfg = CHANNEL_CONFIGS[channel]

    mock = service.mock(
        name=channel + "-provider-mock",
        definition=cfg["mock_def"],
        after=[conn],
    )
    channel_mocks[channel] = mock

    events_topic = create_topic("notifications-" + channel,   partitions=cfg["partitions"], after=[broker])
    channel_topics[channel] = [events_topic]

    if ENABLE_DLQ:
        dlq_topic = create_topic("notifications-" + channel + "-dlq", partitions=1, after=[broker])
        channel_topics[channel].append(dlq_topic)

all_mocks  = list(channel_mocks.values())
all_topics = [t for topics in channel_topics.values() for t in topics]

# ── optional rate limiter ─────────────────────────────────────────────────────
if ENABLE_RATE_LIMIT:
    rate_limiter = service.run(name="rate-limiter", after=[cache])
    api_extra    = [rate_limiter]
else:
    api_extra = []

# ── notification API ──────────────────────────────────────────────────────────
notification_api = service.run(
    after=[conn, cache] + all_mocks + api_extra + [seed_templates],
    env={
        "ENABLED_CHANNELS":  ",".join(CHANNELS),
        "ENABLED_LOCALES":   ",".join(LOCALES),
        "RATE_LIMIT_ENABLED": str(ENABLE_RATE_LIMIT).lower(),
        "RETRY_POLICY":       RETRY_POLICY,
    },
)

# ── per-channel dispatcher workers ───────────────────────────────────────────
dispatcher_nodes = []
for channel in CHANNELS:
    if channel not in CHANNEL_CONFIGS:
        continue
    cfg = CHANNEL_CONFIGS[channel]

    dispatcher = service.run(
        name="dispatcher-" + channel,
        after=[cache, notification_api] + channel_topics.get(channel, []),
        env={
            "CHANNEL":        channel,
            "RETRY_POLICY":   RETRY_POLICY,
            "DLQ_ENABLED":    str(ENABLE_DLQ).lower(),
        },
    )
    dispatcher_nodes.append(dispatcher)

# ── smoke tests — one per channel ────────────────────────────────────────────
smoke_nodes = []
for channel in CHANNELS:
    if channel not in CHANNEL_CONFIGS:
        continue
    cfg = CHANNEL_CONFIGS[channel]

    if RETRY_POLICY == "none":
        fail_logs = ["SEND_FAILED"]
    else:
        fail_logs = ["SEND_FAILED", "MAX_RETRIES_EXCEEDED"]

    smoke = test.run(
        name="notify-smoke-" + channel,
        file="notify_smoke.py",
        image=cfg["image"],
        after=dispatcher_nodes,
        env={
            "CHANNEL":          channel,
            "DLQ_ENABLED":      str(ENABLE_DLQ).lower(),
            "RATE_LIMIT_ENABLED": str(ENABLE_RATE_LIMIT).lower(),
        },
        fail_on_logs=fail_logs,
        exports=[
            {"path": "reports/" + channel + "-junit.xml", "name": "notify-smoke-" + channel, "on": "always", "format": "junit"},
        ],
    )
    smoke_nodes.append(smoke)

# ── locale rendering tests ─────────────────────────────────────────────────────
for locale in LOCALES:
    test.run(
        name="template-render-" + locale,
        file="template_render.py",
        image="python:3.12",
        after=smoke_nodes,
        env={"LOCALE": locale, "CATEGORIES": ",".join(TEMPLATE_CATEGORIES)},
        exports=[{"path": "reports/render-" + locale + ".xml", "name": "render-" + locale, "on": "always", "format": "junit"}],
    )

# ── DLQ drain test (only when DLQ is enabled) ────────────────────────────────
if ENABLE_DLQ:
    test.run(
        name="dlq-drain-audit",
        file="dlq_audit.py",
        image="python:3.12",
        after=smoke_nodes,
        fail_on_logs=["DLQ_OVERFLOW", "UNPARSEABLE_MESSAGE"],
        exports=[{"path": "reports/dlq-audit.xml", "name": "dlq-audit", "on": "always", "format": "junit"}],
    )

# ── rate limit boundary test ──────────────────────────────────────────────────
if ENABLE_RATE_LIMIT:
    test.run(
        name="rate-limit-boundary",
        file="rate_limit_boundary.py",
        image="python:3.12",
        after=smoke_nodes,
        fail_on_logs=["LIMIT_NOT_ENFORCED", "BURST_THRESHOLD_MISSED"],
    )
