load("@babelsuite/playwright", "browser_test", "a11y_audit", "visual_diff")
load("@babelsuite/runtime",   "service")

BROWSERS = ["chromium", "firefox", "webkit"]
DEVICES  = ["desktop", "mobile", "tablet"]
LOCALES  = ["en", "de", "fr"]

BASE_URL  = env.get("APP_URL",    "http://storefront-ui:3000")
VISUAL    = env.get("VISUAL",     "false") == "true"
A11Y      = env.get("A11Y",       "true")  == "true"

ui = service.run(name="storefront-ui")

# full browser × device matrix for the checkout flow
checkout_nodes = browser_test(
    spec     = "checkout.spec.ts",
    base_url = BASE_URL,
    browsers = BROWSERS,
    devices  = DEVICES,
    after    = [ui],
    env      = {"LOCALE": "en"},
)

# per-locale smoke — only on desktop chromium to keep the matrix manageable
locale_nodes = []
for locale in LOCALES:
    nodes = browser_test(
        spec     = "locale_smoke.spec.ts",
        base_url = BASE_URL,
        browsers = ["chromium"],
        devices  = ["desktop"],
        after    = checkout_nodes,
        env      = {"LOCALE": locale},
    )
    locale_nodes += nodes

# accessibility audit on the checkout and account pages
if A11Y:
    for page_path in ["/checkout", "/account", "/product/1"]:
        a11y_audit(
            url              = BASE_URL + page_path,
            after            = checkout_nodes,
            wcag_level       = "AA",
            fail_on_critical = True,
        )

# visual regression — chromium + firefox only, off by default
if VISUAL:
    visual_diff(
        spec      = "visual_baseline.spec.ts",
        base_url  = BASE_URL,
        browsers  = ["chromium", "firefox"],
        threshold = 0.02,
        after     = checkout_nodes,
    )
