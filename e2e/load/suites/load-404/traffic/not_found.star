probe = traffic.user(
    name="probe",
    weight=1,
    wait=traffic.constant(0.05),
    tasks=[
        traffic.task(
            name="hit",
            request=traffic.get("/status/404", name="hit"),
        ),
    ],
)

traffic.plan(
    users=[probe],
    shape=traffic.stages([
        traffic.stage(duration="5s", users=2, spawn_rate=2),
    ]),
    thresholds=[
        traffic.threshold("http.error_rate", "<=", 0),
    ],
)
