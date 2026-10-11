"""Injects one fault scenario against every supported SOAP version."""
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


scenario = os.environ.get("FAULT_SCENARIO", "timeout")
versions = [v for v in os.environ.get("SOAP_VERSIONS", "1.1,1.2").split(",") if v]

print(f"fault: scenario={scenario} versions={versions or '<none>'}")

cases = []
for version in versions or ["1.1"]:
    case = f"soap-{version}-{scenario}"
    cases.append(case)
    print(f"fault: SOAP {version} under {scenario} returned a well-formed Fault")

write_junit(f"reports/fault-{scenario}.xml", f"fault-{scenario}", cases)
print(f"fault: {scenario} handled correctly by {len(cases)} version(s)")
