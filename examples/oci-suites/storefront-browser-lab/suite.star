load("@babelsuite/runtime", "service", "task", "test", "traffic", "log")
load("@babelsuite/playwright", "browser")

# ── Level 5: adds browser tests, multi-browser loop, continue_on_failure ─────
# New here: Playwright browser() nodes, per-browser loop, continue_on_failure
# lets the suite stay Healthy even when one browser has flaky coverage.

BROWSERS = env.get("BROWSERS", "chromium").split(",")

# ── infrastructure ────────────────────────────────────────────────────────────
db           = service.run(name="db")
cache        = service.run(name="cache",        after=[db])
catalog_mock = service.mock(name="catalog-mock",after=[db])

seed = task.run(
    name     = "seed-products",
    image    = "node:22-alpine",
    commands = ["node scripts/seed.js --count 200"],
    after    = [db],
)

storefront = service.run(name="storefront", after=[seed, cache, catalog_mock])

ready = log.info(
    "storefront ready — {{ healthy }}/{{ total }} healthy, BROWSERS={{ env.BROWSERS }}",
    after = [storefront],
)

# ── api load + smoke ──────────────────────────────────────────────────────────
cart_load = traffic.smoke(
    name   = "cart-load",
    target = "http://storefront:3000",
    after  = [ready],
)

api_smoke = test.run(
    name                = "api-smoke",
    image               = "node:22-alpine",
    file                = "api.test.ts",
    continue_on_failure = True,
    fail_on_logs        = ["UNCAUGHT_EXCEPTION", "CHECKOUT_TIMEOUT"],
    after               = [cart_load],
    exports             = [{"path": "reports/api.xml", "name": "api-tests", "format": "junit", "on": "always"}],
)

# ── per-browser checkout tests ────────────────────────────────────────────────
browser_nodes = []
for b in BROWSERS:
    node = browser(
        name    = "checkout-" + b,
        file    = "tests/checkout.spec.ts",
        browser = b,
        after   = [ready],
        exports = [{"path": "playwright-report/" + b + ".xml", "name": "browser-" + b, "format": "junit", "on": "always"}],
    )
    browser_nodes.append(node)

# ── rollback on api-smoke failure ─────────────────────────────────────────────
task.run(
    name       = "reset-catalog",
    image      = "node:22-alpine",
    commands   = ["node scripts/reset.js"],
    on_failure = [api_smoke],
)

log.info("browser lab complete — {{ healthy }}/{{ total }} passed", after=browser_nodes + [api_smoke])
