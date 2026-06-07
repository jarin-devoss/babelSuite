#!/bin/sh
set -e
URL="${POSTGRES_URL:-postgresql://postgres:postgres@db:5432/app}"
COUNT="${PRODUCT_COUNT:-200}"

psql "$URL" -v ON_ERROR_STOP=1 << SQL
  CREATE TABLE IF NOT EXISTS products (id serial PRIMARY KEY, name text, price numeric, stock int);
  INSERT INTO products (name, price, stock)
  SELECT 'Product-' || i,
         round((random()*100+1)::numeric, 2),
         (random()*500+1)::int
  FROM generate_series(1, $COUNT) i
  ON CONFLICT DO NOTHING;
SQL
