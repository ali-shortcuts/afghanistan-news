#!/usr/bin/env bash
# Postgres health checks: version, connectivity, schema, and the numbers an operator cares about.
set -euo pipefail
URL="${DATABASE_URL:-postgres://afnews:afnews@127.0.0.1:5432/afnews?sslmode=disable}"
PSQL=(psql "$URL" -X -q)

echo "== version =="
"${PSQL[@]}" -tAc "select version();"

echo
echo "== schema =="
"${PSQL[@]}" -c "select (select count(*) from information_schema.tables where table_schema='public') tables,
                        (select max(version) from schema_migrations)      migration;"

echo
echo "== content =="
"${PSQL[@]}" -c "select (select count(*) from articles)  articles,
                        (select count(*) from feeds)     feeds,
                        (select count(*) from sources)   sources,
                        (select count(*) from push_events) push_events;"

echo
echo "== articles per day (last 7) =="
"${PSQL[@]}" -c "select published_at::date day, count(*) from articles
                 where published_at > now() - interval '7 days'
                 group by 1 order by 1 desc;"

echo
echo "== feed health =="
"${PSQL[@]}" -c "select health_state, count(*) from feeds group by 1 order by 2 desc;"

echo
echo "== index usage on the hot path =="
"${PSQL[@]}" -c "select relname, seq_scan, idx_scan, n_live_tup
                 from pg_stat_user_tables
                 where relname in ('articles','feeds','article_categories')
                 order by n_live_tup desc;"
