probe = traffic.user(
    name="authorize-payment",
    weight=1,
    wait=traffic.constant(0.1),
    tasks=[
        traffic.task(
            name="authorize-payment",
            request=traffic.post("/payments?status=approved", name="authorize-payment", json={"amount": 1299, "cardToken": "tok_visa", "currency": "USD", "merchantId": "m-117"}),
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
