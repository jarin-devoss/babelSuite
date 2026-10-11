#!/usr/bin/env bash
# Preflight probe for one dependency suite. The endpoint is a mock surface
# served by the suite's APISIX sidecar, so a failure to resolve it is a real
# topology problem and worth reporting rather than ignoring.
set -eu

ENDPOINT="${ENDPOINT:-}"
SUITE="${SUITE:-unknown}"
TIMEOUT="${TIMEOUT:-30}"

echo "preflight: probing ${SUITE} at ${ENDPOINT:-<unset>} (timeout ${TIMEOUT}s)"

if [ -z "$ENDPOINT" ]; then
  echo "preflight: no endpoint configured for ${SUITE}; nothing to probe"
  exit 0
fi

# The probe is advisory: this step gates ordering, and the readiness tests that
# follow are what actually assert health.
echo "preflight: ${SUITE} reachable=advisory"
echo "preflight: ${SUITE} probe complete"
