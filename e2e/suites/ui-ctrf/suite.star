load("@babelsuite/runtime", "test")

report = test.run(
    name="report",
    image="busybox:latest",
    file="ctrf.sh",
    exports=[{"path": "reports/ctrf.json", "name": "ctrf-tests", "on": "always", "format": "ctrf"}],
)
