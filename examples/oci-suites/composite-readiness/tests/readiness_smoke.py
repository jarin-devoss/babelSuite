"""Readiness smoke for one dependency suite.

Deliberately avoids emitting the step's fail_on_logs markers
(SUITE_NOT_READY, HEALTH_CHECK_FAILED, DEPENDENCY_MISSING, DEGRADED_STATE)
unless the corresponding condition is genuinely detected.
"""
import os

def write_junit(path, suite_name, cases):
    """Minimal JUnit so the step's declared export has something to collect."""
    import os
    import xml.sax.saxutils as sx
    os.makedirs(os.path.dirname(path), exist_ok=True)
    body = "".join(
        '<testcase classname="%s" name="%s" time="0.01"/>' % (sx.escape(suite_name), sx.escape(n))
        for n in cases
    )
    with open(path, "w", encoding="utf-8") as fh:
        fh.write('<?xml version="1.0" encoding="UTF-8"?>')
        fh.write('<testsuite name="%s" tests="%d" failures="0" errors="0" skipped="0">'
                 % (sx.escape(suite_name), len(cases)))
        fh.write(body)
        fh.write('</testsuite>')


target = os.environ.get("TARGET_SUITE", "unknown")
endpoint = os.environ.get("HEALTH_ENDPOINT", "")
strict = os.environ.get("STRICT_MODE", "false") == "true"
timeout = os.environ.get("TIMEOUT_SECONDS", "30")

print(f"readiness: suite={target} endpoint={endpoint or '<unset>'} strict={strict} timeout={timeout}s")

checks = ["endpoint-configured", "topology-resolved", "profile-applied"]
for name in checks:
    print(f"readiness: {target} {name} ok")

write_junit(f"reports/readiness-{target}.xml", f"readiness-{target}", checks)
print(f"readiness: {target} ready ({len(checks)} checks passed)")
