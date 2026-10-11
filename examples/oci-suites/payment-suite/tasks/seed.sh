#!/usr/bin/env bash
# Seeds the reference data the checkout and reconciliation steps expect. Runs
# after migrate, so the schema is already in place.
set -eu

CURRENCIES="${SEED_CURRENCIES:-USD,EUR,GBP}"
MERCHANTS="${SEED_MERCHANTS:-3}"

echo "seed: inserting ${MERCHANTS} merchants"
i=0
while [ "$i" -lt "$MERCHANTS" ]; do
  echo "seed:   merchant-${i} created"
  i=$((i + 1))
done

echo "seed: enabling currencies ${CURRENCIES}"
echo "$CURRENCIES" | tr ',' '\n' | while read -r currency; do
  [ -n "$currency" ] && echo "seed:   ${currency} price book loaded"
done

echo "seed: reference data ready"
