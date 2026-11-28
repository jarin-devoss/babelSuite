load("@babelsuite/runtime", "test", "suite", "log")

# ── environment knobs ────────────────────────────────────────────────────────
SUITES_TO_CHECK   = env.get("SUITES_TO_CHECK", "payment-suite,identity-broker,soap-claims-hub").split(",")
READINESS_TIMEOUT = env.get("READINESS_TIMEOUT_SECONDS", "120")

PYTHON = "python:3.12-alpine"

# The suites this one can import, and the service of each that answers /health.
# A suite's own services keep running while this suite's steps do, so each is
# reachable by its name. Only suites whose mocks are HTTP can be imported: the
# gateway serves an imported suite's HTTP and SOAP mocks, not its gRPC ones, so
# returns-control-plane (which calls a gRPC pricing mock) is not on the list.
KNOWN_SUITES = {
    "payment-suite":         "http://payment-api:8080/health",
    "identity-broker":       "http://broker-api:8080/health",
    "soap-claims-hub":       "http://claims-bridge:8080/health",
}

for name in SUITES_TO_CHECK:
    if name not in KNOWN_SUITES:
        fail("SUITES_TO_CHECK names " + name + ", which this suite does not import; it imports " + ", ".join(KNOWN_SUITES.keys()))

# ── the imported suites ──────────────────────────────────────────────────────
# Each runs as it would on its own: its services, and the mocks they call, which
# this run's gateway serves.
imported = []
for name in SUITES_TO_CHECK:
    imported.append(suite.run(name = name, ref = name))

# ── readiness: one step per imported suite, asking its service for /health ───
readiness_nodes = []
for name in SUITES_TO_CHECK:
    readiness_nodes.append(test.run(
        name    = "readiness-smoke-" + name,
        file    = "readiness_smoke.py",
        image   = PYTHON,
        after   = imported,
        env     = {
            "TARGET_SUITE":     name,
            "HEALTH_ENDPOINT":  KNOWN_SUITES[name],
            "TIMEOUT_SECONDS":  READINESS_TIMEOUT,
        },
        fail_on_logs = ["SUITE_NOT_READY"],
        exports = [{"path": "reports/readiness-" + name + ".xml", "name": "readiness-" + name, "on": "always", "format": "junit"}],
    ))

# ── the composed environment as a whole ──────────────────────────────────────
endpoints = []
for name in SUITES_TO_CHECK:
    endpoints.append(name + "=" + KNOWN_SUITES[name])

summary = test.run(
    name    = "composite-health-summary",
    file    = "composite_summary.py",
    image   = PYTHON,
    after   = readiness_nodes,
    env     = {"ENDPOINTS": ",".join(endpoints), "TIMEOUT_SECONDS": READINESS_TIMEOUT},
    fail_on_logs = ["HEALTH_CHECK_FAILED"],
    exports = [{"path": "reports/composite-summary.xml", "name": "composite-health-summary", "on": "always", "format": "junit"}],
)

log.info(str(len(SUITES_TO_CHECK)) + " imported suites are ready", after = [summary])
