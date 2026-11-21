probe = traffic.user(
    name="list-products",
    weight=1,
    wait=traffic.constant(0.1),
    tasks=[
        traffic.task(
            name="list-products",
            request=traffic.get("/catalog/products?scenario=promo", name="list-products", headers={"Accept": "application/json"}),
        ),
    ],
)

traffic.plan(
    users=[probe],
    shape=traffic.stages([
        traffic.stage(duration="20s", users=4, spawn_rate=4),
    ]),
    thresholds=[
        traffic.threshold("http.error_rate", "<=", 0.01),
    ],
)
