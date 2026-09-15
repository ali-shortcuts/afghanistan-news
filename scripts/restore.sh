#!/usr/bin/env bash
# Restore a dump produced by scripts/backup.sh (§Phase B).
#
#   scripts/restore.sh backups/afnews-20260914T031500Z.dump                  # into afnews
#   scripts/restore.sh <dump> --into afnews_restore --create                # drill on a scratch db
#   scripts/restore.sh <dump> --tables articles,article_categories          # partial restore
#
# Restoring into a scratch database is the rehearsal that makes the real restore boring:
# it proves the dump is complete before anyone needs it at 03:00.
set -euo pipefail

DUMP="${1:-}"; shift || true
INTO="${DB_NAME:-afnews}"
CREATE=0
TABLES=""
while [ $# -gt 0 ]; do
  case "$1" in
    --into)   INTO="$2"; shift 2 ;;
    --create) CREATE=1; shift ;;
    --tables) TABLES="$2"; shift 2 ;;
    *) echo "unknown flag: $1" >&2; exit 2 ;;
  esac
done

DB_USER="${DB_USER:-afnews}"
DB_PASS="${DB_PASS:-afnews}"
HOST="${PGHOST:-127.0.0.1}"
say() { printf '\033[1;36m==\033[0m %s\n' "$*"; }
ok()  { printf '\033[1;32m✓\033[0m %s\n' "$*"; }
die() { printf '\033[1;31m✗\033[0m %s\n' "$*" >&2; exit 1; }

[ -n "$DUMP" ] || die "usage: scripts/restore.sh <dump> [--into db] [--create] [--tables a,b]"
[ -f "$DUMP" ] || die "no dump at $DUMP"
export PGPASSWORD="$DB_PASS"
TARGET_URL="postgres://$DB_USER:$DB_PASS@$HOST:5432/$INTO?sslmode=disable"

if [ "$CREATE" = "1" ]; then
  say "creating database $INTO"
  psql "postgres://$DB_USER:$DB_PASS@$HOST:5432/postgres?sslmode=disable" -X -q -tA \
    -c "select 1 from pg_database where datname='$INTO'" | grep -q 1 \
    || createdb -h "$HOST" -U "$DB_USER" "$INTO"
fi

psql "$TARGET_URL" -X -q -tA -c 'select 1' >/dev/null || die "target $INTO not reachable"

say "listing dump (integrity check)"
pg_restore --list "$DUMP" | grep -c 'TABLE DATA' | sed 's/^/  /;s/$/ tables of data/'

RESTORE_ARGS=(--dbname="$TARGET_URL" --no-owner --no-privileges --single-transaction --clean --if-exists)
[ -n "$TABLES" ] && RESTORE_ARGS+=(--table="$(echo "$TABLES" | sed 's/,/ --table=/g')")

say "restoring into $INTO"
if pg_restore "${RESTORE_ARGS[@]}" "$DUMP" 2> >(grep -vE 'does not exist, skipping|errors ignored on restore' >&2); then
  :
else
  die "pg_restore failed"
fi

say "post-restore verification"
psql "$TARGET_URL" -X -q -c "select (select count(*) from articles) articles,
                                     (select count(*) from feeds)    feeds,
                                     (select count(*) from sources)  sources,
                                     (select count(*) from push_events) push_events;"
psql "$TARGET_URL" -X -q -tA -c "select 'saved articles: '||count(*) from push_registrations where 1=1" || true

ok "restored $DUMP → $INTO"
cat <<EOF

Point the stack at the restored database with:
  export DATABASE_URL='$TARGET_URL'
EOF
