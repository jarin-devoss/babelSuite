probe = traffic.user(
    name="probe",
    weight=1,
    wait=traffic.constant(0.1),
    tasks=[
        traffic.task(
            name="greeting",
            request=traffic.get("/greeting?scenario=hello", name="greeting", headers={"Accept": "application/json"}),
        ),
    ],
)

traffic.plan(
    users=[probe],
    shape=traffic.stages([
        traffic.stage(duration="4s", users=2, spawn_rate=2),
    ]),
    thresholds=[
        traffic.threshold("http.error_rate", "<=", 0),
    ],
)
