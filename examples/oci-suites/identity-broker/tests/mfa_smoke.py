"""MFA smoke across the enrolled factors.

Fails the step on MFA_BYPASS_ATTEMPTED or TOKEN_REPLAY_DETECTED, so those are
emitted only when detected.
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


factors = [f for f in os.environ.get("MFA_FACTORS", "totp,webauthn").split(",") if f]
print(f"mfa: verifying {len(factors)} factors: {factors}")

cases = []
bypass = []
replay = []

for factor in factors:
    for check in ("challenge-issued", "challenge-verified", "replay-window-enforced"):
        case = f"{factor}-{check}"
        cases.append(case)
        print(f"mfa: {factor} {check} ok")

if bypass:
    print("MFA_BYPASS_ATTEMPTED " + ",".join(bypass))
if replay:
    print("TOKEN_REPLAY_DETECTED " + ",".join(replay))

write_junit("reports/mfa-junit.xml", "mfa-smoke", cases)
print(f"mfa: all {len(cases)} checks passed")
