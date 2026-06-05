load("@babelsuite/runtime", "service", "task", "test", "log")

# ── Level 2: adds task.run with commands=, service.mock, log.info, secretRefs ─
# New here: inline commands, mock surfaces, log checkpoints, secret injection.
# Profile secretRefs resolve JWT_SIGNING_KEY from vault or local secrets.

db    = service.run(name="db")
vault = service.run(name="vault", after=[db])

oidc_mock = service.mock(name="oidc-provider", after=[vault])

seed = task.run(
    name     = "seed-realms",
    image    = "python:3.12",
    commands = ["python seed_realms.py --env {{ env.REALM_ENV }}"],
    after    = [db],
)

ready = log.info("identity infrastructure ready — db seeded, vault up, OIDC mock healthy", after=[vault, oidc_mock, seed])

broker = service.run(name="broker-api", after=[ready])

login_smoke = test.run(
    name     = "login-smoke",
    image    = "python:3.12",
    commands = ["python -m pytest tests/login.py -v --base-url http://broker-api:9000"],
    after    = [broker],
    exports  = [{"path": "reports/junit.xml", "name": "login-tests", "format": "junit", "on": "always"}],
)

token_smoke = test.run(
    name             = "token-refresh",
    image            = "python:3.12",
    commands         = ["python -m pytest tests/token_refresh.py -v"],
    after            = [login_smoke],
    continue_on_failure = True,
    exports          = [{"path": "reports/token.xml", "name": "token-tests", "format": "junit", "on": "always"}],
)
