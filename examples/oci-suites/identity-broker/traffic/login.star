probe = traffic.user(
    name="begin-login",
    weight=1,
    wait=traffic.constant(0.1),
    tasks=[
        traffic.task(
            name="begin-login",
            request=traffic.post("/sessions/login?provider=oidc", name="begin-login", json={"email": "admin@babelsuite.test"}),
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
