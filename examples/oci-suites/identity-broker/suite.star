load("@babelsuite/runtime", "mock", "service", "test", "traffic", "log")

# ── environment knobs ────────────────────────────────────────────────────────
PROVIDERS   = env.get("AUTH_PROVIDERS", "oidc,saml,ldap").split(",")
REALM_COUNT = int(env.get("REALM_COUNT", "3"))
ENABLE_MFA  = env.get("ENABLE_MFA", "true") == "true"

PYTHON = "python:3.12-alpine"

# ── provider mocks: the identity providers the broker federates to ──────────
# One mock per provider, each answering to its own name (http://oidc-mock:9080).
provider_mocks = []
for provider in PROVIDERS:
    provider_mocks.append(mock.api(name=provider + "-mock"))

# ── the service under test ──────────────────────────────────────────────────
# tasks/broker_api.py signs a user in through the provider's mock, keeps the
# session in the session store and the upstream IdP the pre-hook starts, and
# answers who a session belongs to. It needs both of them: its /health is 200
# only when they answer.
broker_api = service.run(
    name  = "broker-api",
    image = PYTHON,
    file  = "broker_api.py",
    env   = {
        "PORT":        "8080",
        "PROVIDERS":   ",".join(PROVIDERS),
        "REALM_COUNT": str(REALM_COUNT),
        "ENABLE_MFA":  "true" if ENABLE_MFA else "false",
    },
    after = provider_mocks,
)

broker_ready = test.run(
    name     = "broker-api-ready",
    image    = PYTHON,
    after    = [broker_api],
    commands = ["for i in $(seq 1 60); do python -c \"import urllib.request; urllib.request.urlopen('http://broker-api:8080/health')\" 2>/dev/null && exit 0; sleep 1; done; echo 'broker-api never answered /health'; exit 1"],
)

providers_ready = log.info("broker-api is answering; " + str(len(PROVIDERS)) + " provider mocks up", after=[broker_ready])

# ── login traffic: real logins through the broker ───────────────────────────
login_load = traffic.baseline(
    name   = "login-baseline",
    target = "http://broker-api:8080",
    plan   = "login.star",
    after  = [providers_ready],
)

# ── smoke tests — one per provider ──────────────────────────────────────────
smoke_nodes = []
for provider in PROVIDERS:
    smoke_nodes.append(test.run(
        name    = "login-smoke-" + provider,
        file    = "login_smoke.py",
        image   = PYTHON,
        after   = [login_load],
        env     = {"AUTH_PROVIDER": provider},
        exports = [{"path": "reports/" + provider + "-junit.xml", "name": "login-smoke-" + provider, "on": "always", "format": "junit"}],
    ))

expired = test.run(
    name    = "expired-session",
    file    = "expired_session.py",
    image   = PYTHON,
    after   = smoke_nodes,
    exports = [{"path": "reports/expired-session.xml", "name": "expired-session", "on": "always", "format": "junit"}],
)

# ── MFA (only when it is on) ────────────────────────────────────────────────
mfa_after = [expired]
if ENABLE_MFA:
    mfa_after = [test.run(
        name         = "mfa-smoke",
        file         = "mfa_smoke.py",
        image        = PYTHON,
        after        = [expired],
        fail_on_logs = ["MFA_BYPASS_ATTEMPTED", "TOKEN_REPLAY_DETECTED"],
        exports      = [{"path": "reports/mfa-junit.xml", "name": "mfa-smoke", "on": "always", "format": "junit"}],
    )]

# ── realm isolation — one per realm ─────────────────────────────────────────
for i in range(REALM_COUNT):
    test.run(
        name    = "realm-isolation-" + str(i),
        file    = "realm_isolation.py",
        image   = PYTHON,
        after   = mfa_after,
        env     = {"REALM_ID": "realm-" + str(i), "REALM_COUNT": str(REALM_COUNT)},
        exports = [{"path": "reports/realm-" + str(i) + "-isolation.xml", "name": "realm-isolation-" + str(i), "on": "always", "format": "junit"}],
    )
