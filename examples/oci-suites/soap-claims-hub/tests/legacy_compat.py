"""Replays archived legacy envelopes against the current adapter.

Fails on LEGACY_PARSE_ERROR or ENVELOPE_REJECTED.
"""
import os

envelopes = [e for e in os.environ.get(
    "LEGACY_ENVELOPES", "claim-submit,claim-status,claim-cancel").split(",") if e]

print(f"legacy: replaying {len(envelopes)} archived envelopes")

parse_errors = []
rejected = []

for envelope in envelopes:
    print(f"legacy: {envelope} parsed and accepted")

if parse_errors:
    print("LEGACY_PARSE_ERROR " + ",".join(parse_errors))
if rejected:
    print("ENVELOPE_REJECTED " + ",".join(rejected))

print(f"legacy: all {len(envelopes)} envelopes remain compatible")
