load("@babelsuite/runtime",    "mock", "service", "test", "traffic", "log")
load("@babelsuite/playwright", "browser_test")

BROWSERS = env.get("BROWSERS", "chromium").split(",")

PYTHON = "python:3.12-alpine"

# ── what the storefront depends on: the catalog, a mock ──────────────────────
catalog_mock = mock.api(name="catalog-api")

# ── the service under test ───────────────────────────────────────────────────
# tasks/storefront.py is the shop: a page, and an API that reads the catalog and
# places orders with it. An order the catalog refuses (sold out) is not confirmed.
storefront = service.run(
    name  = "storefront",
    image = PYTHON,
    file  = "storefront.py",
    env   = {"PORT": "8080", "CATALOG_URL": "http://catalog-api:9080"},
    after = [catalog_mock],
)

storefront_ready = test.run(
    name     = "storefront-ready",
    image    = PYTHON,
    after    = [storefront],
    commands = ["for i in $(seq 1 60); do python -c \"import urllib.request; urllib.request.urlopen('http://storefront:8080/health')\" 2>/dev/null && exit 0; sleep 1; done; echo 'storefront never answered /health'; exit 1"],
)

ready = log.info(
    "storefront ready — {{ healthy }}/{{ total }} healthy — BROWSERS={{ env.BROWSERS }}",
    after = [storefront_ready],
)

# ── traffic: browsing and ordering through the storefront ────────────────────
soak = traffic.soak(
    name   = "cart-soak",
    target = "http://storefront:8080",
    plan   = "catalog.star",
    after  = [ready],
)

# ── api smoke ────────────────────────────────────────────────────────────────
api_smoke = test.run(
    name         = "api-smoke",
    image        = "node:22-alpine",
    file         = "api.test.ts",
    env          = {"STOREFRONT_URL": "http://storefront:8080"},
    fail_on_logs = ["ORDER_CONFIRMED_FOR_SOLD_OUT"],
    after        = [soak],
    exports      = [
        {"path": "reports/api.xml",   "name": "api-tests", "format": "junit", "on": "always"},
        {"path": "reports/ctrf.json", "name": "api-ctrf",  "format": "ctrf",  "on": "always"},
    ],
)

# ── the shopper's journey, in a real browser, once per browser ───────────────
# Each browser_test installs @playwright/test into the step's workspace, and the
# steps share that workspace, so two of them at once corrupt each other's install.
# They run one after another: every spec, in every browser, after the one before.
previous = [api_smoke]
for spec in ["checkout.spec.ts", "cart_abandonment.spec.ts"]:
    for browser in BROWSERS:
        previous = browser_test(
            spec     = spec,
            base_url = "http://storefront:8080",
            browsers = [browser],
            after    = previous,
        )

lab_complete = log.info("browser lab complete — {{ healthy }}/{{ total }} healthy", after = previous)
