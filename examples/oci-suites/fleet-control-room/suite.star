load("@babelsuite/runtime", "mock", "service", "test", "traffic", "log")

# ── environment knobs ────────────────────────────────────────────────────────
REGIONS          = env.get("REGIONS", "eu-west,us-east,ap-south").split(",")
SPIKE_MULTIPLIER = int(env.get("SPIKE_MULTIPLIER", "5"))

TRAFFIC_PHASES = ["ramp", "steady", "spike", "drain"]

PYTHON = "python:3.12-alpine"

# ── what the dispatcher depends on, both mocks ───────────────────────────────
# telemetry-mock takes vehicle frames; geo-fence-mock says which zones may be
# entered. The route planner is a gRPC mock the gateway serves from
# api/proto/fleet_control.proto.
telemetry_mock = mock.api(name="telemetry-mock")
geo_fence_mock = mock.api(name="geo-fence-mock", after=[telemetry_mock])

# ── the service under test ───────────────────────────────────────────────────
# tasks/dispatcher.py assigns routes: it asks the geo-fence service whether the
# zone may be entered, then the planner for an assignment. A route into a
# restricted zone is refused before the planner is asked.
dispatcher = service.run(
    name  = "dispatcher",
    image = PYTHON,
    file  = "dispatcher.py",
    env   = {
        "PORT":        "8080",
        "REGIONS":     ",".join(REGIONS),
        "GATEWAY_URL": "http://babelsuite-gateway:9080",
        "GEOFENCE_URL":  "http://geo-fence-mock:9080",
        "TELEMETRY_URL": "http://telemetry-mock:9080",
    },
    after = [telemetry_mock, geo_fence_mock],
)

dispatcher_ready = test.run(
    name     = "dispatcher-ready",
    image    = PYTHON,
    after    = [dispatcher],
    commands = ["for i in $(seq 1 60); do python -c \"import urllib.request; urllib.request.urlopen('http://dispatcher:8080/health')\" 2>/dev/null && exit 0; sleep 1; done; echo 'dispatcher never answered /health'; exit 1"],
)

fleet_ready = log.info(
    str(len(REGIONS)) + " regions configured; dispatcher is answering",
    after = [dispatcher_ready],
)

# ── multi-phase traffic: real route assignments through the dispatcher ───────
phase_nodes = []
prev_phase  = None
for phase in TRAFFIC_PHASES:
    after_nodes = [fleet_ready] + ([prev_phase] if prev_phase != None else [])

    if phase == "ramp":
        t = traffic.baseline(name="fleet-ramp",   target="http://dispatcher:8080", plan="dispatch.star", after=after_nodes)
    elif phase == "steady":
        t = traffic.baseline(name="fleet-steady", target="http://dispatcher:8080", plan="dispatch.star", after=after_nodes)
    elif phase == "spike":
        t = traffic.spike(   name="fleet-spike",  target="http://dispatcher:8080", plan="dispatch.star", after=after_nodes, rps=10 * SPIKE_MULTIPLIER)
    else:
        t = traffic.soak(    name="fleet-drain",  target="http://dispatcher:8080", plan="dispatch.star", after=after_nodes)

    phase_nodes.append(t)
    prev_phase = t

# ── smoke tests ───────────────────────────────────────────────────────────────
fleet_smoke = test.run(
    name         = "fleet-smoke",
    file         = "fleet_smoke.py",
    image        = PYTHON,
    after        = phase_nodes,
    fail_on_logs = ["PLANNER_REFUSED", "Traceback"],
    exports      = [{"path": "reports/fleet-junit.xml", "name": "fleet-smoke", "on": "always", "format": "junit"}],
)

# ── per-region acceptance tests ───────────────────────────────────────────────
for region in REGIONS:
    test.run(
        name    = "acceptance-" + region,
        file    = "regional_acceptance.py",
        image   = PYTHON,
        after   = [fleet_smoke],
        env     = {"REGION": region},
        exports = [{"path": "reports/acceptance-" + region + ".xml", "name": "acceptance-" + region, "on": "always", "format": "junit"}],
    )

# ── geo-fence audit: no route enters the restricted zone ─────────────────────
test.run(
    name         = "geo-fence-audit",
    file         = "geo_fence_audit.py",
    image        = PYTHON,
    after        = [fleet_smoke],
    env          = {"REGIONS": ",".join(REGIONS)},
    fail_on_logs = ["BOUNDARY_VIOLATION"],
    exports      = [{"path": "reports/geo-fence-audit.xml", "name": "geo-fence-audit", "on": "always", "format": "junit"}],
)

# ── degraded dependencies: the planner and telemetry refuse something ────────
test.run(
    name    = "route-degradation",
    file    = "route_degradation.py",
    image   = PYTHON,
    after   = [fleet_smoke],
    exports = [{"path": "reports/route-degradation.xml", "name": "route-degradation", "on": "always", "format": "junit"}],
)
