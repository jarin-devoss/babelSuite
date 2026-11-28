# Storefront Browser Lab

A small web shop checked in real browsers. The shopper adds an item, checks out and is told when it is sold out. The shop's page and API are a service the suite ships; the catalog it calls is a mock.

## Execution order

```
catalog-api (mock) ── storefront ── storefront-ready ── ready ── cart-soak ── api-smoke
                                                                              ├── checkout-spec-ts-<browser>-desktop
                                                                              └── cart-abandonment-spec-ts-<browser>-desktop
                                                                                    └── lab_complete
```

## What breaks it

An order the catalog refuses is never confirmed to the shopper: the `if status == 409:` line in `place_order()` (`tasks/storefront.py`). Make it `if False:` and `api-smoke` fails (it orders the sold-out item), and so does the browser test that checks out with it.

## Profiles

| Profile | What it does |
|---|---|
| `local.yaml` | Chromium. |
| `ci.yaml` | Chromium, quiet logs. |
| `promo.yaml` | Chromium, Firefox and WebKit. |

## Structure

| Path | Description |
|---|---|
| `suite.star` | The topology; `browser_test(...)` runs each spec once per browser. |
| `tasks/storefront.py` | The shop. |
| `mock/` | The catalog: a product list (one item sold out) and an order endpoint. |
| `tests/api.test.ts` | The API checks, run by the image's own Node. |
| `tests/*.spec.ts` | `@playwright/test` specs. |
| `traffic/catalog.star` | The load plan. |

## Running

```bash
python scripts/push-to-zot.py --suite storefront-browser-lab --sync http://localhost:8090
```

Then launch it from the UI, or `POST /api/v1/executions` with `{"suiteId": "storefront-browser-lab", "profile": "local.yaml"}`.

The browser steps install `@playwright/test` at the image's version when they start, so the first run needs the npm registry.
