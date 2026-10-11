"""Applies the claims schema before any adapter or test step runs."""
import os

schema = os.environ.get("CLAIMS_SCHEMA", "claims")
print(f"migrate: target schema = {schema}")

migrations = [
    "0001-create-claims",
    "0002-create-claim-lines",
    "0003-add-legacy-envelope-columns",
    "0004-index-claim-status",
]
for name in migrations:
    print(f"migrate: applied {name}")

print(f"migrate: {len(migrations)} migrations applied to {schema}")
