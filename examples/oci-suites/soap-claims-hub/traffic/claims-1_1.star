# SOAP 1.1: the bridge builds the envelope and calls the claims system with it.
probe = traffic.user(
    name="submit-claim",
    weight=1,
    wait=traffic.constant(0.1),
    tasks=[
        traffic.task(
            name="submit-claim",
            request=traffic.post(
                "/claims",
                name="submit-claim",
                headers={"X-Soap-Version": "1.1", "Content-Type": "application/json"},
                json={"claimType": "medical", "policyNumber": "MED-1024", "lossType": "water", "amount": 4200},
            ),
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
