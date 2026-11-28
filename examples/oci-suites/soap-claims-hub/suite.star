load("@babelsuite/runtime", "mock", "service", "test", "traffic", "log")

# ── environment knobs ────────────────────────────────────────────────────────
CLAIM_TYPES      = env.get("CLAIM_TYPES", "medical,dental,vision,pharmacy").split(",")
LEGACY_MODE      = env.get("LEGACY_MODE", "true") == "true"
VALIDATION_LEVEL = env.get("VALIDATION_LEVEL", "strict")   # strict | lenient | audit
SOAP_VERSIONS    = env.get("SOAP_VERSIONS", "1.1,1.2").split(",")

CLAIM_TYPE_CODES = {
    "medical":  "MED",
    "dental":   "DEN",
    "vision":   "VIS",
    "pharmacy": "PHM",
}

SOAP_FAULT_SCENARIOS = ["invalid_schema", "missing_header", "unsupported_version"]

PYTHON = "python:3.12-alpine"

# ── the legacy claims system: one SOAP mock per claim type ───────────────────
# Each answers to its own name (http://medical-claims-mock:9080/ClaimService) and
# picks its answer from the SOAPAction header.
claim_mocks = {}
for claim_type in CLAIM_TYPES:
    claim_mocks[claim_type] = mock.api(name = claim_type + "-claims-mock")
all_mocks = list(claim_mocks.values())

# ── the service under test ───────────────────────────────────────────────────
# tasks/claims_bridge.py is a REST bridge over the claims system: it builds the
# SOAP envelope (1.1 or 1.2), calls the claims system, and turns the answer into
# JSON.
claims_bridge = service.run(
    name  = "claims-bridge",
    image = PYTHON,
    file  = "claims_bridge.py",
    env   = {
        "PORT":             "8080",
        "CLAIM_TYPES":      ",".join(CLAIM_TYPES),
        "SOAP_VERSIONS":    ",".join(SOAP_VERSIONS),
        "VALIDATION_LEVEL": VALIDATION_LEVEL,
    },
    after = all_mocks,
)

bridge_ready = test.run(
    name     = "claims-bridge-ready",
    image    = PYTHON,
    after    = [claims_bridge],
    commands = ["for i in $(seq 1 60); do python -c \"import urllib.request; urllib.request.urlopen('http://claims-bridge:8080/health')\" 2>/dev/null && exit 0; sleep 1; done; echo 'claims-bridge never answered /health'; exit 1"],
)

mocks_ready = log.info(
    str(len(CLAIM_TYPES)) + " SOAP mocks up; claims-bridge is answering for SOAP " + ", ".join(SOAP_VERSIONS),
    after = [bridge_ready],
)

# ── traffic — one baseline run per SOAP version, through the bridge ───────────
TRAFFIC_PLANS = {"1.1": "claims-1_1.star", "1.2": "claims-1_2.star"}

traffic_nodes = []
for version in SOAP_VERSIONS:
    traffic_nodes.append(traffic.baseline(
        name   = "claims-baseline-soap-" + version.replace(".", "_"),
        target = "http://claims-bridge:8080",
        plan   = TRAFFIC_PLANS[version],
        after  = [mocks_ready],
    ))

# ── smoke tests — one per claim type ─────────────────────────────────────────
smoke_nodes = []
for claim_type in CLAIM_TYPES:
    if VALIDATION_LEVEL == "strict":
        fail_logs = ["INVALID_CLAIM_CODE", "MISSING_REQUIRED_FIELD", "Traceback"]
    else:
        fail_logs = ["Traceback"]

    smoke_nodes.append(test.run(
        name         = "claims-smoke-" + claim_type,
        file         = "claims_smoke.py",
        image        = PYTHON,
        after        = traffic_nodes,
        env          = {"CLAIM_TYPE": claim_type, "CLAIM_CODE": CLAIM_TYPE_CODES.get(claim_type, "UNK")},
        fail_on_logs = fail_logs,
        exports      = [{"path": "reports/" + claim_type + "-junit.xml", "name": "claims-smoke-" + claim_type, "on": "always", "format": "junit"}],
    ))

# ── SOAP fault injection tests ───────────────────────────────────────────────
for scenario in SOAP_FAULT_SCENARIOS:
    test.run(
        name    = "fault-injection-" + scenario,
        file    = "fault_injection.py",
        image   = PYTHON,
        after   = smoke_nodes,
        env     = {"FAULT_SCENARIO": scenario, "SOAP_VERSIONS": ",".join(SOAP_VERSIONS)},
        exports = [{"path": "reports/fault-" + scenario + ".xml", "name": "fault-" + scenario, "on": "always", "format": "junit"}],
    )

# ── legacy mode: archived envelopes are handled as they always were ──────────
if LEGACY_MODE:
    test.run(
        name         = "legacy-compat-check",
        file         = "legacy_compat.py",
        image        = PYTHON,
        after        = smoke_nodes,
        fail_on_logs = ["LEGACY_PARSE_ERROR", "ENVELOPE_REJECTED"],
        exports      = [{"path": "reports/legacy-compat.xml", "name": "legacy-compat", "on": "always", "format": "junit"}],
    )

# ── adapter contract: the WSDL's operations, against the bridge ──────────────
test.run(
    name    = "adapter-contract",
    file    = "adapter_contract.py",
    image   = PYTHON,
    after   = smoke_nodes,
    exports = [{"path": "reports/adapter-contract.xml", "name": "adapter-contract", "on": "always", "format": "junit"}],
)
