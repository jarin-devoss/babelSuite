load("@babelsuite/runtime", "service", "task", "test", "log")

# Level 2 — tasks, mocks, log levels, evaluation controls, secrets
# New: task.run with commands=, service.mock, log.info/debug,
#      secretRefs (resolved from profile), expect_exit=, expect_logs=,
#      fail_on_logs=, continue_on_failure=

db    = service.run(name="db")
vault = service.run(name="vault", after=[db])

oidc_mock = service.mock(name="oidc-provider", after=[vault])

seed = task.run(
    name        = "seed-realms",
    image       = "python:3.12",
    commands    = ["python seed_realms.py --env {{ env.REALM_ENV }}"],
    expect_exit = 0,
    after       = [db],
)

log.debug("realm seeding complete — checking vault health", after=[seed, vault])

ready = log.info("identity infrastructure ready — OIDC mock up", after=[oidc_mock, seed])

broker = service.run(name="broker-api", after=[ready])

# JWT_SIGNING_KEY injected from profile secretRefs
login_smoke = test.run(
    name                = "login-smoke",
    image               = "python:3.12",
    commands            = ["python -m pytest tests/login.py -v"],
    expect_logs         = "login succeeded",
    fail_on_logs        = ["INVALID_TOKEN", "AUTH_FAILURE"],
    after               = [broker],
    exports             = [{"path": "reports/login.xml", "name": "login-tests", "format": "junit", "on": "always"}],
)

# continue_on_failure: token refresh flakiness does not block downstream steps
token_smoke = test.run(
    name                = "token-refresh",
    image               = "python:3.12",
    commands            = ["python -m pytest tests/token_refresh.py -v"],
    continue_on_failure = True,
    after               = [login_smoke],
    exports             = [{"path": "reports/token.xml", "name": "token-tests", "format": "junit", "on": "always"}],
)
