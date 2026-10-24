load("@babelsuite/runtime", "mock", "task", "test", "traffic", "log")
load("@babelsuite/kafka",   "kafka", "create_topic")
load("@babelsuite/redis",   "redis")

# ── environment knobs ────────────────────────────────────────────────────────
REGIONS          = env.get("REGIONS",          "eu-west,us-east,ap-south").split(",")
VEHICLE_CLASSES  = env.get("VEHICLE_CLASSES",  "van,truck,motorcycle").split(",")
LOAD_TEST_REGIONS = env.get("LOAD_TEST_REGIONS", "eu-west,us-east").split(",")
ENABLE_TELEMETRY = env.get("ENABLE_TELEMETRY", "true") == "true"
ENABLE_GEO_FENCE = env.get("ENABLE_GEO_FENCE", "true") == "true"
SPIKE_MULTIPLIER = int(env.get("SPIKE_MULTIPLIER", "5"))

TRAFFIC_PHASES = ["ramp", "steady", "spike", "drain"]

# ── infrastructure ────────────────────────────────────────────────────────────
cache          = redis()
broker         = kafka()
telemetry_mock = mock.api(name="telemetry-mock", after=[cache]) if ENABLE_TELEMETRY else None
geo_fence_mock = mock.api(name="geo-fence-mock", after=[cache]) if ENABLE_GEO_FENCE else None

infra_mocks = [m for m in [telemetry_mock, geo_fence_mock] if m != None]

# In perf.yaml profile this step gets devices: ["gpu"] for fast graph init
seed_routes = task.run(file="seed_routes.sh", image="bash:5.2", after=[cache])

# ── per-vehicle-class telemetry topics ───────────────────────────────────────
telemetry_topics = []
if ENABLE_TELEMETRY:
    for vehicle_class in VEHICLE_CLASSES:
        t = create_topic(broker, "telemetry-" + vehicle_class, partitions=6)
        telemetry_topics.append(t)

infra_ready = [cache, seed_routes] + infra_mocks + telemetry_topics

# ── per-region load tests, for the designated regions ────────────────────────
for region in REGIONS:
    if region in LOAD_TEST_REGIONS:
        traffic.stress(
            name   = "stress-" + region,
            target = "http://dispatcher-" + region + ":8080",
            rps    = min(20 * SPIKE_MULTIPLIER, 80),
            after  = infra_ready,
            env    = {"REGION": region},
        )

fleet_ready = log.info(
    str(len(REGIONS)) + " regions configured",
    after=infra_ready,
)

# ── multi-phase traffic simulation ────────────────────────────────────────────
phase_nodes = []
prev_phase  = None
for phase in TRAFFIC_PHASES:
    after_nodes = [fleet_ready] + ([prev_phase] if prev_phase != None else [])

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
