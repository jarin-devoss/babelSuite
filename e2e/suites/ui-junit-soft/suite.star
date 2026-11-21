load("@babelsuite/runtime", "test")

report = test.run(
    name="report",
    image="busybox:latest",
    commands=["echo UI-JUNIT-SOFT no report written"],
    exports=[{"path": "reports/junit.xml", "name": "go-tests", "on": "always", "format": "junit", "enforce": False}],
)
