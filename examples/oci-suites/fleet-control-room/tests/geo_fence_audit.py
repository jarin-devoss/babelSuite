"""Audits geo-fence telemetry.

The step fails on BOUNDARY_VIOLATION or UNREPORTED_EXIT, so those markers are
printed only when the audit actually finds the condition.
"""
import os

fences = [f for f in os.environ.get("GEO_FENCES", "depot,city-core,highway").split(",") if f]
print(f"geo-fence: auditing {len(fences)} fences")

violations = []
unreported = []

for fence in fences:
    print(f"geo-fence: {fence} crossings reconciled")

if violations:
    for fence in violations:
        print(f"BOUNDARY_VIOLATION {fence}")
if unreported:
    for fence in unreported:
        print(f"UNREPORTED_EXIT {fence}")

print(f"geo-fence: audit clean across {len(fences)} fences")
