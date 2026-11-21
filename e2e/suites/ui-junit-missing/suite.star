load("@babelsuite/runtime", "test")

report = test.run(
    name="report",
    image="busybox:latest",
    commands=["echo UI-JUNIT-MISSING no report written"],
    exports=[{"path": "reports/junit.xml", "name": "go-tests", "on": "always", "format": "junit"}],
)
