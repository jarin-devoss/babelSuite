load("@babelsuite/runtime", "test", "suite", "task", "log")
load("@babelsuite/postgres", "pg", "connect")

# ── environment knobs ────────────────────────────────────────────────────────
SUITES_TO_CHECK   = env.get("SUITES_TO_CHECK", "payment-suite,identity-broker,returns-control-plane").split(",")
READINESS_TIMEOUT = int(env.get("READINESS_TIMEOUT_SECONDS", "120"))
STRICT_MODE       = env.get("STRICT_MODE", "true") == "true"
ENABLE_DB_PROBE   = env.get("ENABLE_DB_PROBE", "true") == "true"
REGIONS           = env.get("REGIONS", "eu,us").split(",")

SUITE_REGISTRY_REFS = {
    "payment-suite":          "localhost:5000/core-platform/payment-suite",
    "identity-broker":        "localhost:5000/security/identity-broker",
    "returns-control-plane":  "localhost:5000/qa/returns-control-plane",
    "notification-hub":       "localhost:5000/platform/notification-hub",
    "fleet-control-room":     "localhost:5000/platform/fleet-control-room",
}

SUITE_HEALTH_ENDPOINTS = {
    "payment-suite":          "http://payment-gateway:8080/health",
    "identity-broker":        "http://broker-api:9000/health",
    "returns-control-plane":  "http://returns-api:8080/health",
    "notification-hub":       "http://notification-api:8080/health",
    "fleet-control-room":     "http://control-room:8080/health",
}

# ── optional DB connectivity probe ────────────────────────────────────────────
if ENABLE_DB_PROBE:
    db   = pg()
    conn = connect(after=[db])
    db_deps = [conn]
else:
    db_deps = []

# ── load dependent suites ─────────────────────────────────────────────────────
suite_nodes  = []
unknown_suites = []

for suite_name in SUITES_TO_CHECK:
    if suite_name not in SUITE_REGISTRY_REFS:
        unknown_suites.append(suite_name)
        continue

    s   = suite.run(name=suite_name, ref=suite_name, after=db_deps)
    suite_nodes.append(s)

# ── pre-flight connectivity checks ────────────────────────────────────────────
preflight_nodes = []
for suite_name in SUITES_TO_CHECK:
    if suite_name not in SUITE_HEALTH_ENDPOINTS:
        continue
    endpoint = SUITE_HEALTH_ENDPOINTS[suite_name]
    probe = task.run(
        name="preflight-" + suite_name,
        file="probe_health.sh",
        image="bash:5.2",
        after=suite_nodes,
        env={
            "ENDPOINT":  endpoint,
            "TIMEOUT":   str(READINESS_TIMEOUT),
            "SUITE":     suite_name,
        },
    )
    preflight_nodes.append(probe)

# ── readiness smoke — one per suite ──────────────────────────────────────────
readiness_nodes = []
for suite_name in SUITES_TO_CHECK:
    if suite_name not in SUITE_HEALTH_ENDPOINTS:
        continue

    fail_logs = ["SUITE_NOT_READY", "HEALTH_CHECK_FAILED", "DEPENDENCY_MISSING"]
    if STRICT_MODE:
        fail_logs.append("DEGRADED_STATE")

    smoke = test.run(
        name="readiness-smoke-" + suite_name,
        file="readiness_smoke.py",
        image="python:3.12",
        after=preflight_nodes,
        env={
            "TARGET_SUITE":       suite_name,
            "HEALTH_ENDPOINT":    SUITE_HEALTH_ENDPOINTS[suite_name],
            "STRICT_MODE":        str(STRICT_MODE).lower(),
            "TIMEOUT_SECONDS":    str(READINESS_TIMEOUT),
        },
        fail_on_logs=fail_logs,
        exports=[
            {"path": "reports/readiness-" + suite_name + ".xml", "name": "readiness-" + suite_name, "on": "always", "format": "junit"},
        ],
    )
    readiness_nodes.append(smoke)

# ── per-region end-to-end readiness ──────────────────────────────────────────
for region in REGIONS:
    region_suites = [s for s in SUITES_TO_CHECK if s in SUITE_HEALTH_ENDPOINTS]

    test.run(
        name="e2e-readiness-" + region,
        file="e2e_readiness.py",
        image="python:3.12",
        after=readiness_nodes,
        env={
            "REGION":        region,
            "TARGET_SUITES": ",".join(region_suites),
            "STRICT_MODE":   str(STRICT_MODE).lower(),
        },
        exports=[{"path": "reports/e2e-" + region + ".xml", "name": "e2e-readiness-" + region, "on": "always", "format": "junit"}],
    )

# ── unknown suites report (fail if any were requested but not registered) ─────
if len(unknown_suites) > 0 and STRICT_MODE:
    task.run(
        name="report-unknown-suites",
        file="report_unknown.sh",
        image="bash:5.2",
        after=readiness_nodes,
        env={"UNKNOWN_SUITES": ",".join(unknown_suites)},
    )

# ── composite health summary ──────────────────────────────────────────────────
log.info(
    str(len(SUITES_TO_CHECK)) + " suites checked across " + str(len(REGIONS)) + " regions — running summary",
    after=readiness_nodes,
)
test.run(
    name="composite-health-summary",
    file="composite_summary.py",
    image="python:3.12",
    after=readiness_nodes,
    env={
        "CHECKED_SUITES": ",".join(SUITES_TO_CHECK),
        "REGIONS":        ",".join(REGIONS),
        "STRICT_MODE":    str(STRICT_MODE).lower(),
    },
    exports=[
        {"path": "reports/composite-summary.xml", "name": "composite-health-summary", "on": "always", "format": "junit"},
    ],
)
