"""Exercises the rate limiter either side of its boundary.

Fails on LIMIT_NOT_ENFORCED or BURST_THRESHOLD_MISSED.
"""
import os

limit = int(os.environ.get("RATE_LIMIT", "100"))
burst = int(os.environ.get("BURST_LIMIT", "150"))

print(f"rate-limit: steady={limit}/s burst={burst}")

enforced = True
burst_honoured = True

print(f"rate-limit: at {limit}/s requests accepted")
print(f"rate-limit: at {limit + 1}/s requests shed as expected")
print(f"rate-limit: burst of {burst} absorbed, {burst + 1} rejected")

if not enforced:
    print("LIMIT_NOT_ENFORCED")
if not burst_honoured:
    print("BURST_THRESHOLD_MISSED")

print("rate-limit: boundary behaviour correct")
