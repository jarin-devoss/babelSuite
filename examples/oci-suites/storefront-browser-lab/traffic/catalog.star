# Browses the storefront: the product grid, which the storefront reads from the
# catalog, and an order for an item in stock.
browse = traffic.user(
    name="browse",
    weight=3,
    wait=traffic.constant(0.1),
    tasks=[
        traffic.task(
            name="list-products",
            request=traffic.get("/api/products", name="list-products", headers={"Accept": "application/json"}),
        ),
    ],
)

buy = traffic.user(
    name="buy",
    weight=1,
    wait=traffic.constant(0.1),
    tasks=[
        traffic.task(
            name="order",
            request=traffic.post(
                "/api/orders",
                name="order",
                headers={"Content-Type": "application/json"},
                json={"email": "shopper@demo.test", "sku": "sku_1001", "quantity": 1},
            ),
        ),
    ],
)

traffic.plan(
    users=[browse, buy],
    shape=traffic.stages([
        traffic.stage(duration="20s", users=4, spawn_rate=4),
    ]),
    thresholds=[
        traffic.threshold("http.error_rate", "<=", 0.01),
    ],
)
