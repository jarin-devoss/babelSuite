load("@babelsuite/runtime", "service", "task", "test", "traffic")
load("@babelsuite/postgres", "pg", "connect", "insert")

# ── environment knobs ────────────────────────────────────────────────────────
CLAIM_TYPES      = env.get("CLAIM_TYPES", "medical,dental,vision,pharmacy").split(",")
LEGACY_MODE      = env.get("LEGACY_MODE", "true") == "true"
ENABLE_ADAPTER   = env.get("ENABLE_ADAPTER", "true") == "true"
VALIDATION_LEVEL = env.get("VALIDATION_LEVEL", "strict")   # strict | lenient | audit
SOAP_VERSIONS    = env.get("SOAP_VERSIONS", "1.1,1.2").split(",")

CLAIM_TYPE_CODES = {
    "medical":  "MED",
    "dental":   "DEN",
    "vision":   "VIS",
    "pharmacy": "PHM",
}

SOAP_FAULT_SCENARIOS = ["invalid_schema", "missing_header", "unsupported_version"]

# ── infrastructure ────────────────────────────────────────────────────────────
db   = pg()
conn = connect(after=[db])

migrations = task.run(file="migrate.py", image="python:3.12", after=[conn])

# seed reference data for all active claim types
ref_rows = [
    {"code": CLAIM_TYPE_CODES[ct], "label": ct.capitalize(), "active": True}
    for ct in CLAIM_TYPES
    if ct in CLAIM_TYPE_CODES
]
seed_reference_data = insert(table="claim_types", rows=ref_rows, after=[migrations])

# ── one SOAP mock per claim type ──────────────────────────────────────────────
claim_mocks = {}
for claim_type in CLAIM_TYPES:
    mock_name = claim_type + "-claims-mock"
    mock = service.mock(
        name=mock_name,
        definition="mock/claims/" + claim_type,
        after=[conn],
    )
    claim_mocks[claim_type] = mock

all_mocks = list(claim_mocks.values())

# ── optional REST adapter (wraps SOAP for modern consumers) ──────────────────
if ENABLE_ADAPTER:
    rest_adapter = service.run(
        name="soap-rest-adapter",
        after=all_mocks,
        env={"CLAIM_TYPES": ",".join(CLAIM_TYPES), "SOAP_VERSIONS": ",".join(SOAP_VERSIONS)},
    )
    bridge_deps = [rest_adapter]
else:
    bridge_deps = []

# ── claims bridge ─────────────────────────────────────────────────────────────
claims_bridge = service.run(
    after=all_mocks + bridge_deps + [seed_reference_data],
    env={
        "LEGACY_MODE":       str(LEGACY_MODE).lower(),
        "VALIDATION_LEVEL":  VALIDATION_LEVEL,
        "SOAP_VERSIONS":     ",".join(SOAP_VERSIONS),
    },
)

# ── traffic — one baseline run per SOAP version ───────────────────────────────
traffic_nodes = []
for version in SOAP_VERSIONS:
    safe_version = version.replace(".", "_")
    t = traffic.baseline(
        name="claims-baseline-soap-" + safe_version,
        target="http://claims-bridge:8080",
        after=[claims_bridge],
        env={"SOAP_VERSION": version},
    )
    traffic_nodes.append(t)

# ── smoke tests — one per claim type ─────────────────────────────────────────
smoke_nodes = []
for claim_type in CLAIM_TYPES:
    if claim_type not in claim_mocks:
        continue

    if VALIDATION_LEVEL == "strict":
        fail_logs = ["SCHEMA_VIOLATION", "MISSING_REQUIRED_FIELD", "INVALID_CLAIM_CODE"]
    elif VALIDATION_LEVEL == "audit":
        fail_logs = ["SCHEMA_VIOLATION"]
    else:
        fail_logs = []

    smoke = test.run(
        name="claims-smoke-" + claim_type,
        file="claims_smoke.py",
        image="python:3.12",
        after=traffic_nodes,
        env={
            "CLAIM_TYPE":       claim_type,
            "CLAIM_CODE":       CLAIM_TYPE_CODES.get(claim_type, "UNK"),
            "LEGACY_MODE":      str(LEGACY_MODE).lower(),
        },
        fail_on_logs=fail_logs,
        exports=[
            {"path": "reports/" + claim_type + "-junit.xml", "name": "claims-smoke-" + claim_type, "on": "always", "format": "junit"},
        ],
    )
    smoke_nodes.append(smoke)

# ── SOAP fault injection tests ────────────────────────────────────────────────
for scenario in SOAP_FAULT_SCENARIOS:
    test.run(
        name="fault-injection-" + scenario,
        file="fault_injection.py",
        image="python:3.12",
        after=smoke_nodes,
        env={"FAULT_SCENARIO": scenario, "SOAP_VERSIONS": ",".join(SOAP_VERSIONS)},
        exports=[{"path": "reports/fault-" + scenario + ".xml", "name": "fault-" + scenario, "on": "always", "format": "junit"}],
    )

# ── legacy mode backward-compat check ────────────────────────────────────────
if LEGACY_MODE:
    test.run(
        name="legacy-compat-check",
        file="legacy_compat.py",
        image="python:3.12",
        after=smoke_nodes,
        fail_on_logs=["LEGACY_PARSE_ERROR", "ENVELOPE_REJECTED"],
    )

# ── adapter contract tests (only when adapter is running) ─────────────────────
if ENABLE_ADAPTER:
    test.run(
        name="adapter-contract",
        file="adapter_contract.py",
        image="python:3.12",
        after=smoke_nodes,
        env={"ADAPTER_URL": "http://soap-rest-adapter:9090"},
        exports=[{"path": "reports/adapter-contract.xml", "name": "adapter-contract", "on": "always", "format": "junit"}],
    )
