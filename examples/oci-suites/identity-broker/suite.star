load("@babelsuite/runtime", "service", "task", "test", "traffic", "log")
load("@babelsuite/postgres", "pg", "connect")

# ── environment knobs ────────────────────────────────────────────────────────
PROVIDERS         = env.get("AUTH_PROVIDERS", "oidc,saml,ldap").split(",")
REALM_COUNT       = int(env.get("REALM_COUNT", "3"))
ENABLE_MFA        = env.get("ENABLE_MFA", "true") == "true"
ENABLE_CANARY     = env.get("ENABLE_CANARY", "false") == "true"
SESSION_BACKENDS  = env.get("SESSION_BACKENDS", "jwt,cookie").split(",")

PROVIDER_IMAGES = {
    "oidc": "python:3.12",
    "saml": "python:3.12",
    "ldap": "python:3.12",
}

PROVIDER_MOCK_FILES = {
    "oidc": "mock/oidc",
    "saml": "mock/saml",
    "ldap": "mock/ldap",
}

# ── infrastructure ───────────────────────────────────────────────────────────
db   = pg()
conn = connect(db)

# ── seed realms ──────────────────────────────────────────────────────────────
realms = []
for i in range(REALM_COUNT):
    realm_id = "realm-" + str(i)
    tier = "enterprise" if i == 0 else "standard"
    realms.append({"id": realm_id, "name": "Realm " + str(i), "tier": tier})

seed_realms = task.run(
    name  = "seed-realms",
    image = "postgres:16",
    file  = "tasks/seed_realms.sh",
    env   = {"REALM_COUNT": str(REALM_COUNT)},
    after = [conn],
)

# ── provider mocks (only the ones listed in PROVIDERS) ───────────────────────
provider_mocks = {}
for provider in PROVIDERS:
    if provider not in PROVIDER_MOCK_FILES:
        continue
    mock = service.mock(
        name=provider + "-mock",
        definition=PROVIDER_MOCK_FILES[provider],
        after=[conn],
    )
    provider_mocks[provider] = mock

# ── optional MFA service ─────────────────────────────────────────────────────
if ENABLE_MFA:
    totp_service  = service.run(name="totp-service",  after=list(provider_mocks.values()) + [conn])
    webauthn_stub = service.mock(name="webauthn-stub", after=[conn])
    mfa_deps      = [totp_service, webauthn_stub]
else:
    mfa_deps = []

# ── broker API ───────────────────────────────────────────────────────────────
providers_ready = log.info("provider mocks up — seeded " + str(REALM_COUNT) + " realms", after=list(provider_mocks.values()) + [seed_realms])
broker_api = service.run(
    name  = "broker-api",
    after = [providers_ready] + mfa_deps,
    env   = {"ENABLED_PROVIDERS": ",".join(PROVIDERS), "MFA_ENABLED": str(ENABLE_MFA).lower()},
)

# ── session workers (one per backend type) ───────────────────────────────────
session_workers = []
for backend in SESSION_BACKENDS:
    worker = service.run(
        name="session-worker-" + backend,
        after=[broker_api],
        env={"SESSION_BACKEND": backend},
    )
    session_workers.append(worker)

# ── canary broker (optional) ─────────────────────────────────────────────────
if ENABLE_CANARY:
    canary_broker = service.run(
        name="broker-api-canary",
        after=list(provider_mocks.values()) + [seed_realms],
        env={"ENABLED_PROVIDERS": ",".join(PROVIDERS), "FEATURE_FLAGS": "token_binding=true"},
    )
    api_targets = [broker_api, canary_broker]
else:
    api_targets = [broker_api]

# ── login traffic ────────────────────────────────────────────────────────────
traffic_nodes = []
for target in api_targets:
    t = traffic.baseline(
        name="login-baseline-" + target.name,
        target="http://" + target.name + ":9000",
        after=session_workers + [target],
    )
    traffic_nodes.append(t)

# ── smoke tests — one per provider ───────────────────────────────────────────
smoke_nodes = []
for provider in PROVIDERS:
    if provider not in provider_mocks:
        continue
    smoke = test.run(
        name="login-smoke-" + provider,
        file="login_smoke.py",
        image=PROVIDER_IMAGES.get(provider, "python:3.12"),
        after=traffic_nodes,
        env={"AUTH_PROVIDER": provider},
        exports=[
            {"path": "reports/" + provider + "-junit.xml", "name": "login-smoke-" + provider, "on": "always", "format": "junit"},
        ],
    )
    smoke_nodes.append(smoke)

# ── MFA smoke (only when MFA is enabled) ─────────────────────────────────────
if ENABLE_MFA:
    test.run(
        name="mfa-smoke",
        file="mfa_smoke.py",
        image="python:3.12",
        after=smoke_nodes,
        fail_on_logs=["MFA_BYPASS_ATTEMPTED", "TOKEN_REPLAY_DETECTED"],
        exports=[{"path": "reports/mfa-junit.xml", "name": "mfa-smoke", "on": "always", "format": "junit"}],
    )

# ── realm isolation test — one per realm ─────────────────────────────────────
for i in range(REALM_COUNT):
    test.run(
        name="realm-isolation-" + str(i),
        file="realm_isolation.py",
        image="python:3.12",
        after=smoke_nodes,
        env={"REALM_ID": "realm-" + str(i)},
    )
