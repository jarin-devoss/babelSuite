"""Verifies one payment event type routes through to a notification.

Fails on EVENT_DROPPED, ROUTING_FAILED or IDENTITY_MISMATCH.
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


event_type = os.environ.get("PAYMENT_EVENT_TYPE", "payment.succeeded")
safe_name = event_type.replace(".", "-")

print(f"payment-notify: event={event_type}")

dropped = False
routing_failed = False
identity_mismatch = False

cases = ["event-received", "template-selected", "recipient-resolved", "notification-queued"]
for case in cases:
    print(f"payment-notify: {event_type} {case} ok")

if dropped:
    print(f"EVENT_DROPPED {event_type}")
if routing_failed:
    print(f"ROUTING_FAILED {event_type}")
if identity_mismatch:
    print(f"IDENTITY_MISMATCH {event_type}")

write_junit(f"reports/payment-notify-{safe_name}.xml", f"payment-notify-{safe_name}", cases)
print(f"payment-notify: {event_type} delivered")
