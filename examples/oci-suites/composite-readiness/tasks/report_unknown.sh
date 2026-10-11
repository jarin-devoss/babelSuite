#!/usr/bin/env bash
# Names the suites listed for checking that the topology could not resolve, so
# a typo in SUITES_TO_CHECK is visible instead of silently skipped.
set -eu

UNKNOWN="${UNKNOWN_SUITES:-}"

if [ -z "$UNKNOWN" ]; then
  echo "unknown-suites: none — every requested suite resolved"
  exit 0
fi

echo "unknown-suites: the following were requested but have no health endpoint:"
echo "$UNKNOWN" | tr ',' '\n' | while read -r suite; do
  [ -n "$suite" ] && echo "  - ${suite}"
done
echo "unknown-suites: add them to SUITE_HEALTH_ENDPOINTS or drop them from SUITES_TO_CHECK"
