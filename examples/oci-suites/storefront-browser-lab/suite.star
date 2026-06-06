load("@babelsuite/runtime", "service", "task", "test", "traffic", "log")

# Level 5 — loops, reset_mocks, ctrf, traffic.soak, log.error, on_failure
# New: for-loop over browser matrix using test.run with env=, reset_mocks= on
#      test.run clears mock state between runs, .export(ctrf), traffic.soak,
#      log.error on failure path, on_failure= rollback branch

BROWSERS = env.get("BROWSERS", "chromium").split(",")

# ── infrastructure ────────────────────────────────────────────────────────────
db           = service.run(name="db")
cache        = service.run(name="cache",        after=[db])
catalog_mock = service.mock(name="catalog-api", after=[db])

seed = task.run(
    name     = "seed-products",
    image    = "node:22-alpine",
    commands = ["node scripts/seed.js --count 200"],
    after    = [db],
)

storefront = service.run(name="storefront", after=[seed, cache, catalog_mock])

ready = log.info(
    "storefront ready — {{ healthy }}/{{ total }} healthy — BROWSERS={{ env.BROWSERS }}",
    after = [storefront],
)

# ── api smoke + soak ──────────────────────────────────────────────────────────
soak = traffic.soak(
    name   = "cart-soak",
    target = "http://storefront:3000",
    after  = [ready],
)

# reset_mocks clears catalog-api mock state before each api test run
api_smoke = test.run(
    name                = "api-smoke",
    image               = "node:22-alpine",
    file                = "api.test.ts",
    reset_mocks         = [catalog_mock],
    continue_on_failure = True,
    fail_on_logs        = ["UNCAUGHT_EXCEPTION", "CHECKOUT_TIMEOUT"],
    after               = [soak],
    exports             = [
        {"path": "reports/api.xml",   "name": "api-tests", "format": "junit", "on": "always"},
        {"path": "reports/ctrf.json", "name": "api-ctrf",  "format": "ctrf",  "on": "always"},
    ],
)

# ── per-browser checkout tests ────────────────────────────────────────────────
browser_nodes = []
for b in BROWSERS:
    node = test.run(
        name    = "checkout-" + b,
        image   = "mcr.microsoft.com/playwright:v1.44.0-jammy",
        file    = "tests/checkout.spec.ts",
        after   = [ready],
        env     = {"BROWSER": b},
        exports = [{"path": "playwright-report/" + b + ".xml", "name": "browser-" + b, "format": "junit", "on": "always"}],
    )
    browser_nodes.append(node)

# ── rollback on api failure ───────────────────────────────────────────────────
rollback = task.run(
    name       = "reset-catalog",
    image      = "node:22-alpine",
    commands   = ["node scripts/reset.js"],
    on_failure = [api_smoke],
)

# log.error — only emits if something went wrong downstream
if len(BROWSERS) > 1:
    log.error("multi-browser run — review any failing browser reports above", after=browser_nodes + [rollback])
else:
    log.info("browser lab complete — {{ healthy }}/{{ total }} passed", after=browser_nodes + [rollback])
