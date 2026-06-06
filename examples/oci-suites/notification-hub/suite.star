load("@babelsuite/runtime", "service", "test")

# Level 1 — absolute minimum
# Features: service.run, test.run, commands=, profile env:, services.<name>.env:
#
# Everything environment-specific lives in the profile.
# suite.star never changes between local, CI, and staging.

api    = service.run(name="notification-api")
worker = service.run(name="dispatcher", after=[api])

smoke = test.run(
    name     = "delivery-smoke",
    image    = "python:3.12",
    commands = ["python -m pytest tests/smoke.py -v"],
    after    = [worker],
    exports  = [{"path": "reports/junit.xml", "name": "delivery-tests", "format": "junit", "on": "always"}],
)
