# The claims mock picks its answer from the SOAPAction header, so the request
# names the operation there.
probe = traffic.user(
    name="claim-status",
    weight=1,
    wait=traffic.constant(0.1),
    tasks=[
        traffic.task(
            name="get-claim-status",
            request=traffic.post(
                "/ClaimService",
                name="get-claim-status",
                headers={"SOAPAction": "urn:GetClaimStatus", "Content-Type": "text/xml; charset=utf-8"},
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
