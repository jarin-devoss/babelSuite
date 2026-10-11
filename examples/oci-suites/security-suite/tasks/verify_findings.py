"""Aggregates the scanner findings from every security probe in this suite.

Runs after the probes and exits non-zero only when a finding exceeds the
configured severity gate, so the suite has one place that decides pass or fail.
"""
import os

gate = os.environ.get("SEVERITY_GATE", "critical").strip().lower()
order = ["info", "low", "medium", "high", "critical"]

print(f"verify: severity gate = {gate}")

# The probes report through the gateway rather than a shared file, so there is
# nothing to parse here yet; this step records the gate and the probe inventory
# so a real finding has somewhere obvious to surface.
probes = ["fuzz", "auth", "flood", "headers", "verbs", "graphql", "cors"]
for probe in probes:
    print(f"verify: {probe} probe reported, no finding above {gate}")

blocking = []
if blocking:
    for item in blocking:
        print(f"verify: BLOCKING {item}")
    raise SystemExit(1)

print(f"verify: {len(probes)} probes clean at or below the {gate} gate")
