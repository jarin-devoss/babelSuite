probe = traffic.user(
    name="create-return",
    weight=1,
    wait=traffic.constant(0.1),
    tasks=[
        traffic.task(
            name="create-return",
            request=traffic.post("/returns", name="create-return", json={"amountCents": 4200, "orderId": "ord_1001", "returnReason": "damaged"}),
        ),
    ],
)

traffic.plan(
    users=[probe],
    shape=traffic.stages([
        traffic.stage(duration="10s", users=4, spawn_rate=4),
    ]),
    thresholds=[
        traffic.threshold("http.error_rate", "<=", 0.01),
    ],
)
