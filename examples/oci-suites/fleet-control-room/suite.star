load("@babelsuite/runtime", "service", "task", "test", "traffic", "log")
load("@babelsuite/kafka",   "kafka", "create_topic")
load("@babelsuite/redis",   "redis")

# Level 7 — OCI modules, service.run with image+commands, all traffic phases,
#             all log levels, multi-region loops, GPU in perf profile
# New: kafka/redis OCI modules, service.run(image=, commands=) detached container,
#      traffic.baseline/stress/spike/soak/wave/step multi-phase plan,
#      log.info/warn/error/debug all used, GPU via profile devices in perf runs

REGIONS           = env.get("REGIONS",           "eu-west,us-east,ap-south").split(",")
VEHICLE_CLASSES   = env.get("VEHICLE_CLASSES",   "van,truck,motorcycle").split(",")
LOAD_TEST_REGIONS = env.get("LOAD_TEST_REGIONS", "eu-west,us-east").split(",")
ENABLE_TELEMETRY  = env.get("ENABLE_TELEMETRY",  "true") == "true"
ENABLE_GEO_FENCE  = env.get("ENABLE_GEO_FENCE",  "true") == "true"
SPIKE_MULTIPLIER  = int(env.get("SPIKE_MULTIPLIER", "5"))

REGION_VEHICLE_CAPS = {
    "eu-west":  {"van": 200, "truck": 50,  "motorcycle": 400},
    "us-east":  {"van": 350, "truck": 120, "motorcycle": 600},
    "ap-south": {"van": 150, "truck": 30,  "motorcycle": 800},
}

if SPIKE_MULTIPLIER > 10:
    log.warn("SPIKE_MULTIPLIER={{ env.SPIKE_MULTIPLIER }} — traffic may overwhelm local resources")

# ── OCI modules ───────────────────────────────────────────────────────────────
cache  = redis()
broker = kafka()

telemetry_mock = service.mock(name="telemetry-mock", after=[cache]) if ENABLE_TELEMETRY else None
geo_fence_mock = service.mock(name="geo-fence-mock", after=[cache]) if ENABLE_GEO_FENCE else None
infra_mocks    = [m for m in [telemetry_mock, geo_fence_mock] if m != None]

# ── seed_routes: service.run with image+commands — detached container ─────────
# In perf.yaml profile this step gets devices: ["gpu"] for fast graph init
seed_routes = task.run(file="seed_routes.sh", image="bash:5.2", after=[cache])

# ── per-vehicle-class telemetry topics ────────────────────────────────────────
telemetry_topics = []
if ENABLE_TELEMETRY:
    for vehicle_class in VEHICLE_CLASSES:
        t = create_topic("telemetry-" + vehicle_class, partitions=6, after=[broker])
        telemetry_topics.append(t)

log.debug("topic bootstrap complete — {{ healthy }} topics ready", after=telemetry_topics)

# ── per-region dispatcher + planner stack ─────────────────────────────────────
planners    = []
dispatchers = []

for region in REGIONS:
    caps       = REGION_VEHICLE_CAPS.get(region, {"van": 100, "truck": 20, "motorcycle": 200})
    dispatcher = service.run(
        name  = "dispatcher-" + region,
        after = [cache, seed_routes] + infra_mocks + telemetry_topics,
        env   = {"REGION": region, "GEO_FENCE_ENABLED": str(ENABLE_GEO_FENCE).lower()},
    )
    dispatchers.append(dispatcher)

    region_planners = []
    for vehicle_class in VEHICLE_CLASSES:
        planner = service.run(
            name  = "planner-" + region + "-" + vehicle_class,
            after = [dispatcher],
            env   = {"REGION": region, "VEHICLE_CLASS": vehicle_class, "MAX_VEHICLES": str(caps.get(vehicle_class, 100))},
        )
        region_planners.append(planner)
    planners += region_planners

    if region in LOAD_TEST_REGIONS:
        traffic.stress(
            name  = "stress-" + region,
            target = "http://dispatcher-" + region + ":8080",
            rps    = min(20 * SPIKE_MULTIPLIER, 80),
            after  = region_planners,
            env    = {"REGION": region},
        )

# ── control room — aggregates all regions ─────────────────────────────────────
fleet_ready = log.info(
    "{{ suite }} on {{ profile }} — {{ healthy }}/{{ total }} nodes healthy, REGIONS={{ env.REGIONS }}",
    after = planners,
)

control_room = service.run(
    after = [fleet_ready],
    env   = {"REGIONS": ",".join(REGIONS), "VEHICLE_CLASSES": ",".join(VEHICLE_CLASSES)},
)

# ── multi-phase traffic plan: ramp → steady → spike → wave → drain ────────────
ramp   = traffic.baseline(name="fleet-ramp",   target="http://control-room:8080", after=[control_room])
steady = traffic.baseline(name="fleet-steady",  target="http://control-room:8080", after=[ramp])
spike  = traffic.spike(   name="fleet-spike",   target="http://control-room:8080", rps=10 * SPIKE_MULTIPLIER, after=[steady])
wave   = traffic.wave(    name="fleet-wave",    target="http://control-room:8080", after=[spike])
drain  = traffic.soak(    name="fleet-drain",   target="http://control-room:8080", after=[wave])

# ── smoke tests ───────────────────────────────────────────────────────────────
fleet_smoke = test.run(
    file         = "fleet_smoke.py",
    image        = "python:3.12",
    after        = [drain] + infra_mocks,
    fail_on_logs = ["DISPATCH_TIMEOUT", "PLANNER_STALL", "TELEMETRY_GAP"],
    exports      = [
        {"path": "reports/fleet-junit.xml",    "name": "fleet-smoke",    "on": "always", "format": "junit"},
        {"path": "reports/fleet-coverage.xml", "name": "fleet-coverage", "on": "always", "format": "cobertura"},
    ],
)

for region in REGIONS:
    test.run(
        name    = "acceptance-" + region,
        file    = "regional_acceptance.py",
        image   = "python:3.12",
        after   = [fleet_smoke],
        env     = {"REGION": region, "VEHICLE_CLASSES": ",".join(VEHICLE_CLASSES)},
        exports = [{"path": "reports/acceptance-" + region + ".xml", "name": "acceptance-" + region, "on": "always", "format": "junit"}],
    )

if ENABLE_GEO_FENCE:
    test.run(
        name         = "geo-fence-audit",
        file         = "geo_fence_audit.py",
        image        = "python:3.12",
        after        = [fleet_smoke],
        fail_on_logs = ["BOUNDARY_VIOLATION", "UNREPORTED_EXIT"],
    )
