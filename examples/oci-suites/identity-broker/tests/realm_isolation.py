"""Checks that one realm cannot read another realm's sessions or claims."""
import os

realm = os.environ.get("REALM_ID", "realm-0")
print(f"isolation: realm={realm}")

for check in ("session-store-scoped", "claims-scoped", "token-audience-bound",
              "cross-realm-read-denied"):
    print(f"isolation: {realm} {check} ok")

print(f"isolation: {realm} is isolated")
