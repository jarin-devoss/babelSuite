load("@babelsuite/runtime", "mock", "task", "test", "traffic", "log")
load("@babelsuite/postgres", "pg", "connect")

# ── environment knobs ────────────────────────────────────────────────────────
PROVIDERS         = env.get("AUTH_PROVIDERS", "oidc,saml,ldap").split(",")
REALM_COUNT       = int(env.get("REALM_COUNT", "3"))
ENABLE_MFA        = env.get("ENABLE_MFA", "true") == "true"

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
    provider_mock = mock.api(
        name=provider + "-mock",
        definition=PROVIDER_MOCK_FILES[provider],
        after=[conn],
    )
    provider_mocks[provider] = provider_mock

# ── optional MFA mock ────────────────────────────────────────────────────────
if ENABLE_MFA:
    webauthn_stub = mock.api(name="webauthn-stub", after=[conn])
    mfa_deps      = [webauthn_stub]
else:
    mfa_deps = []

providers_ready = log.info("provider mocks up — seeded " + str(REALM_COUNT) + " realms", after=list(provider_mocks.values()) + [seed_realms])

# ── login traffic ────────────────────────────────────────────────────────────
traffic_nodes = [traffic.baseline(
    name="login-baseline",
    target="http://broker-api:9000",
    after=[providers_ready] + mfa_deps,
)]

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
