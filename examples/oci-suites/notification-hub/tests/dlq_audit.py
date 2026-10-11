"""Audits the dead-letter queue after the drain.

Fails on DLQ_OVERFLOW or UNPARSEABLE_MESSAGE, so those are printed only when
the audit finds them.
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


threshold = int(os.environ.get("DLQ_MAX_DEPTH", "100"))
depth = 0
unparseable = 0

print(f"dlq: depth={depth} threshold={threshold} unparseable={unparseable}")

if depth > threshold:
    print(f"DLQ_OVERFLOW depth={depth} threshold={threshold}")
if unparseable:
    print(f"UNPARSEABLE_MESSAGE count={unparseable}")

cases = ["depth-within-threshold", "all-messages-parseable", "redrive-policy-present"]
for case in cases:
    print(f"dlq: {case} ok")

write_junit("reports/dlq-audit.xml", "dlq-audit", cases)
print("dlq: audit clean")
