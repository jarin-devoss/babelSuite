probe = traffic.user(
    name="read-status",
    weight=1,
    wait=traffic.constant(0.1),
    tasks=[
        traffic.task(
            name="read-status",
            request=traffic.get("/fleet/status?scenario=live", name="read-status"),
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
