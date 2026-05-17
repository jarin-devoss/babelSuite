load("@babelsuite/runtime", "service", "task", "test", "traffic", "suite")
load("@babelsuite/kafka",   "kafka", "create_topic")

# ── environment knobs ────────────────────────────────────────────────────────
BROWSERS         = env.get("BROWSERS", "chromium,firefox,webkit").split(",")
DEVICE_PROFILES  = env.get("DEVICE_PROFILES", "desktop,mobile").split(",")
LOCALES          = env.get("LOCALES", "en,de,ja").split(",")
ENABLE_A11Y      = env.get("ENABLE_A11Y", "true") == "true"
ENABLE_PERF      = env.get("ENABLE_PERF", "false") == "true"
PROMO_CAMPAIGNS  = env.get("PROMO_CAMPAIGNS", "summer,flash").split(",")

DEVICE_VIEWPORTS = {
    "desktop": {"width": 1280, "height": 800},
    "mobile":  {"width": 390,  "height": 844},
    "tablet":  {"width": 768,  "height": 1024},
}

BROWSER_IMAGE = "mcr.microsoft.com/playwright:v1.53.0-noble"

PAYMENT_SUITE_REF = env.get("PAYMENT_SUITE_REF", "localhost:5000/core-platform/payment-suite:stable")

# ── upstream suite dependencies ───────────────────────────────────────────────
# payment-suite provides the checkout and fraud services the browser tests drive
payment_suite = suite.run(
    name="payment-suite",
    ref=PAYMENT_SUITE_REF,
)

# ── infrastructure ───────────────────────────────────────────────────────────
broker       = kafka()
catalog_mock = service.mock(name="catalog-mock", after=[broker])
orders_mock  = service.mock(name="orders-mock",  after=[broker])

# ── event topics ─────────────────────────────────────────────────────────────
EVENT_TOPICS = ["order.created", "order.shipped", "inventory.updated", "promo.activated"]
topic_nodes  = []
for topic_name in EVENT_TOPICS:
    t = create_topic(topic_name, partitions=3, after=[broker])
    topic_nodes.append(t)

# ── promo campaign topics ─────────────────────────────────────────────────────
promo_topic_nodes = []
for campaign in PROMO_CAMPAIGNS:
    t = create_topic("promo-" + campaign, partitions=1, after=[broker])
    promo_topic_nodes.append(t)

# ── seed product catalogue ────────────────────────────────────────────────────
seed_products = task.run(
    file="seed_products.ts",
    image="node:22",
    after=[catalog_mock],
    env={"LOCALE_COUNT": str(len(LOCALES)), "LOCALES": ",".join(LOCALES)},
)

# ── event consumer and storefront services ────────────────────────────────────
event_consumer = service.run(after=topic_nodes + promo_topic_nodes + [orders_mock])
storefront_api = service.run(after=[catalog_mock, orders_mock, seed_products, payment_suite] + topic_nodes)
storefront_ui  = service.run(after=[storefront_api])

# ── performance monitoring (optional) ────────────────────────────────────────
if ENABLE_PERF:
    lighthouse_collector = service.run(name="lighthouse-collector", after=[storefront_ui])
    perf_deps = [lighthouse_collector]
else:
    perf_deps = []

# ── browser tests — matrix of browser × device ───────────────────────────────
browser_test_nodes = []
for browser in BROWSERS:
    for device in DEVICE_PROFILES:
        if device not in DEVICE_VIEWPORTS:
            continue
        viewport = DEVICE_VIEWPORTS[device]
        test_name = "checkout-" + browser + "-" + device

        exports = [
            {"path": "reports/" + test_name + "-junit.xml", "name": test_name, "on": "always", "format": "junit"},
            {"path": "traces/" + test_name + ".zip",        "name": test_name + "-trace", "on": "failure"},
        ]

        if ENABLE_PERF:
            exports.append({"path": "lighthouse/" + test_name + ".json", "name": test_name + "-perf", "on": "always"})

        t = test.run(
            name=test_name,
            file="playwright_checkout.spec.ts",
            image=BROWSER_IMAGE,
            after=[storefront_ui, event_consumer] + perf_deps,
            env={
                "BROWSER":           browser,
                "VIEWPORT_WIDTH":    str(viewport["width"]),
                "VIEWPORT_HEIGHT":   str(viewport["height"]),
                "DEVICE_PROFILE":    device,
                "COLLECT_PERF":      str(ENABLE_PERF).lower(),
            },
            exports=exports,
        )
        browser_test_nodes.append(t)

# ── locale smoke tests ────────────────────────────────────────────────────────
for locale in LOCALES:
    test.run(
        name="locale-smoke-" + locale,
        file="locale_smoke.spec.ts",
        image=BROWSER_IMAGE,
        after=browser_test_nodes,
        env={"LOCALE": locale, "BROWSER": BROWSERS[0]},
        exports=[{"path": "reports/locale-" + locale + ".xml", "name": "locale-" + locale, "on": "always", "format": "junit"}],
    )

# ── accessibility audit (only when enabled) ───────────────────────────────────
if ENABLE_A11Y:
    test.run(
        name="a11y-audit",
        file="a11y_audit.spec.ts",
        image=BROWSER_IMAGE,
        after=browser_test_nodes,
        fail_on_logs=["WCAG_VIOLATION", "AXE_CRITICAL"],
        exports=[{"path": "reports/a11y.xml", "name": "a11y-audit", "on": "always", "format": "junit"}],
    )

# ── promo traffic — one baseline run per campaign ────────────────────────────
for campaign in PROMO_CAMPAIGNS:
    traffic.baseline(
        name="promo-traffic-" + campaign,
        plan="promo_baseline.star",
        target="http://storefront-api:3000",
        after=browser_test_nodes + promo_topic_nodes,
        env={"CAMPAIGN": campaign},
    )
