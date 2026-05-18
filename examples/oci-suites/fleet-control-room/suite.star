load("@babelsuite/runtime", "service", "task", "test", "traffic")
load("@babelsuite/kafka",   "kafka", "create_topic")
load("@babelsuite/redis",   "redis")

# ── environment knobs ────────────────────────────────────────────────────────
REGIONS          = env.get("REGIONS",          "eu-west,us-east,ap-south").split(",")
VEHICLE_CLASSES  = env.get("VEHICLE_CLASSES",  "van,truck,motorcycle").split(",")
LOAD_TEST_REGIONS = env.get("LOAD_TEST_REGIONS", "eu-west,us-east").split(",")
ENABLE_TELEMETRY = env.get("ENABLE_TELEMETRY", "true") == "true"
ENABLE_GEO_FENCE = env.get("ENABLE_GEO_FENCE", "true") == "true"
SPIKE_MULTIPLIER = int(env.get("SPIKE_MULTIPLIER", "5"))

REGION_VEHICLE_CAPS = {
    "eu-west":  {"van": 200, "truck": 50,  "motorcycle": 400},
    "us-east":  {"van": 350, "truck": 120, "motorcycle": 600},
    "ap-south": {"van": 150, "truck": 30,  "motorcycle": 800},
}

TRAFFIC_PHASES = ["ramp", "steady", "spike", "drain"]

# ── infrastructure ────────────────────────────────────────────────────────────
cache          = redis()
broker         = kafka()
telemetry_mock = service.mock(name="telemetry-mock", after=[cache]) if ENABLE_TELEMETRY else None
geo_fence_mock = service.mock(name="geo-fence-mock", after=[cache]) if ENABLE_GEO_FENCE else None

infra_mocks = [m for m in [telemetry_mock, geo_fence_mock] if m != None]

seed_routes = task.run(file="seed_routes.sh", image="bash:5.2", after=[cache])

# ── per-vehicle-class telemetry topics ───────────────────────────────────────
telemetry_topics = []
if ENABLE_TELEMETRY:
    for vehicle_class in VEHICLE_CLASSES:
        t = create_topic("telemetry-" + vehicle_class, partitions=6, after=[broker])
        telemetry_topics.append(t)

# ── per-region dispatcher + planner stack ────────────────────────────────────
planners    = []
dispatchers = []

for region in REGIONS:
    caps = REGION_VEHICLE_CAPS.get(region, {"van": 100, "truck": 20, "motorcycle": 200})

    dispatcher = service.run(
        name="dispatcher-" + region,
        after=[cache, seed_routes] + infra_mocks + telemetry_topics,
        env={
            "REGION":             region,
            "GEO_FENCE_ENABLED":  str(ENABLE_GEO_FENCE).lower(),
            "TELEMETRY_ENABLED":  str(ENABLE_TELEMETRY).lower(),
        },
    )
    dispatchers.append(dispatcher)

    # one planner per vehicle class per region
    region_planners = []
    for vehicle_class in VEHICLE_CLASSES:
        cap = caps.get(vehicle_class, 100)
        planner = service.run(
            name="planner-" + region + "-" + vehicle_class,
            after=[dispatcher],
            env={
                "REGION":         region,
                "VEHICLE_CLASS":  vehicle_class,
                "MAX_VEHICLES":   str(cap),
            },
        )
        region_planners.append(planner)
    planners += region_planners

    # load tests only for designated regions
    if region in LOAD_TEST_REGIONS:
        traffic.stress(
            name="stress-" + region,
            target="http://dispatcher-" + region + ":8080",
            rps=100 * SPIKE_MULTIPLIER,
            after=region_planners,
            env={"REGION": region},
        )

# ── control room — aggregates all regions ─────────────────────────────────────
control_room = service.run(
    after=planners,
    env={"REGIONS": ",".join(REGIONS), "VEHICLE_CLASSES": ",".join(VEHICLE_CLASSES)},
)

# ── multi-phase traffic simulation ────────────────────────────────────────────
phase_nodes = []
prev_phase  = None
for phase in TRAFFIC_PHASES:
    after_nodes = [control_room] + ([prev_phase] if prev_phase != None else [])

    if phase == "ramp":
        t = traffic.baseline(name="fleet-ramp",   target="http://control-room:8080", after=after_nodes)
    elif phase == "steady":
        t = traffic.baseline(name="fleet-steady",  target="http://control-room:8080", after=after_nodes)
    elif phase == "spike":
        t = traffic.spike(   name="fleet-spike",   target="http://control-room:8080", after=after_nodes, rps=10 * SPIKE_MULTIPLIER)
    else:
        t = traffic.soak(    name="fleet-drain",   target="http://control-room:8080", after=after_nodes)

    phase_nodes.append(t)
    prev_phase = t

# ── smoke tests ───────────────────────────────────────────────────────────────
fleet_smoke = test.run(
    file="fleet_smoke.py",
    image="python:3.12",
    after=phase_nodes + infra_mocks,
    fail_on_logs=["DISPATCH_TIMEOUT", "PLANNER_STALL", "TELEMETRY_GAP"],
    exports=[
        {"path": "reports/fleet-junit.xml",    "name": "fleet-smoke",    "on": "always", "format": "junit"},
        {"path": "reports/fleet-coverage.xml", "name": "fleet-coverage", "on": "always", "format": "cobertura"},
    ],
)

# ── per-region acceptance tests ───────────────────────────────────────────────
for region in REGIONS:
    test.run(
        name="acceptance-" + region,
        file="regional_acceptance.py",
        image="python:3.12",
        after=[fleet_smoke],
        env={"REGION": region, "VEHICLE_CLASSES": ",".join(VEHICLE_CLASSES)},
        exports=[{"path": "reports/acceptance-" + region + ".xml", "name": "acceptance-" + region, "on": "always", "format": "junit"}],
    )

# ── geo-fence validation (only when enabled) ──────────────────────────────────
if ENABLE_GEO_FENCE:
    test.run(
        name="geo-fence-audit",
        file="geo_fence_audit.py",
        image="python:3.12",
        after=[fleet_smoke],
        fail_on_logs=["BOUNDARY_VIOLATION", "UNREPORTED_EXIT"],
    )
