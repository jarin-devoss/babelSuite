# Dispatches routes through the dispatcher, which asks the geo-fence service and
# the planner for each one, and reads the control room's status.
assign = traffic.user(
    name="assign-route",
    weight=3,
    wait=traffic.constant(0.1),
    tasks=[
        traffic.task(
            name="assign-route",
            request=traffic.post(
                "/routes/assign",
                name="assign-route",
                headers={"Content-Type": "application/json"},
                json={"routeId": "load-1", "vehicleId": "vh-11", "zone": "depot-north", "region": "eu-west"},
            ),
        ),
    ],
)

status = traffic.user(
    name="read-status",
    weight=1,
    wait=traffic.constant(0.1),
    tasks=[
        traffic.task(
            name="read-status",
            request=traffic.get("/fleet/status", name="read-status"),
        ),
    ],
)

traffic.plan(
    users=[assign, status],
    shape=traffic.stages([
        traffic.stage(duration="10s", users=4, spawn_rate=4),
    ]),
    thresholds=[
        traffic.threshold("http.error_rate", "<=", 0.01),
    ],
)
