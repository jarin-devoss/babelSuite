load("@babelsuite/runtime", "test")

report = test.run(
    name="report",
    image="busybox:latest",
    file="junit.sh",
    exports=[{"path": "reports/junit.xml", "name": "go-tests", "on": "always", "format": "junit"}],
)
