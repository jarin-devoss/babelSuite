load("@babelsuite/runtime", "service", "task", "test", "traffic")

REGIONS = ["eu-west", "us-east", "ap-south"]
LOAD_TEST_REGIONS = ["eu-west", "us-east"]

redis_cache = service.run()
telemetry_mock = service.mock(after=[redis_cache])
seed_routes = task.run(file="seed_routes.sh", image="bash:5.2", after=[redis_cache])

planners = []
for region in REGIONS:
    dispatcher = service.run(name="dispatcher-" + region, after=[redis_cache, seed_routes])
    planner = service.run(name="planner-" + region, after=[dispatcher])
    planners.append(planner)

    if region in LOAD_TEST_REGIONS:
        traffic.stress(
            name="stress-" + region,
            plan="fleet_stress.star",
            target="http://dispatcher-" + region + ":8080",
            after=[planner],
        )

control_room = service.run(after=planners)
fleet_smoke = test.run(file="fleet_smoke.py", image="python:3.12", after=[control_room, telemetry_mock])
