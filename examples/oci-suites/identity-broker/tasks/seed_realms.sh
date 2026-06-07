#!/bin/sh
set -e
URL="${POSTGRES_URL:-postgresql://postgres:postgres@db:5432/app}"
COUNT="${REALM_COUNT:-3}"

psql "$URL" -v ON_ERROR_STOP=1 -c "
  CREATE TABLE IF NOT EXISTS realms (id text PRIMARY KEY, name text, tier text);
"

i=0
while [ "$i" -lt "$COUNT" ]; do
    if [ "$i" -eq "0" ]; then
        TIER="enterprise"
    else
        TIER="standard"
    fi
    psql "$URL" -v ON_ERROR_STOP=1 -c "
        INSERT INTO realms (id, name, tier)
        VALUES ('realm-$i', 'Realm $i', '$TIER')
        ON CONFLICT DO NOTHING;
    "
    i=$((i + 1))
done
