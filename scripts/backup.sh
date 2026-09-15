#!/usr/bin/env bash
# Back up the production database (§Phase B). Custom-format dump so restores are selective
# and can be parallelised, plus a plain-SQL copy for inspections and a manifest for auditing.
#
#   scripts/backup.sh                    # into ./backups
#   BACKUP_DIR=/srv/backups scripts/backup.sh
#   RETENTION_DAYS=30 scripts/backup.sh
#
# Cron example (03:15 every night):
#   15 3 * * * cd /srv/afnews && scripts/backup.sh >> /var/log/afnews-backup.log 2>&1
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BACKUP_DIR="${BACKUP_DIR:-$ROOT/backups}"
RETENTION_DAYS="${RETENTION_DAYS:-14}"
DB_NAME="${DB_NAME:-afnews}"
DB_USER="${DB_USER:-afnews}"
DB_PASS="${DB_PASS:-afnews}"
DATABASE_URL="${DATABASE_URL:-postgres://$DB_USER:$DB_PASS@127.0.0.1:5432/$DB_NAME?sslmode=disable}"

say() { printf '\033[1;36m==\033[0m %s\n' "$*"; }
ok()  { printf '\033[1;32m✓\033[0m %s\n' "$*"; }
die() { printf '\033[1;31m✗\033[0m %s\n' "$*" >&2; exit 1; }

mkdir -p "$BACKUP_DIR"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
BASE="$BACKUP_DIR/afnews-$STAMP"
LOG="$BACKUP_DIR/backup.log"

export PGPASSWORD="$DB_PASS"
PSQL=(psql "$DATABASE_URL" -X -q -tA)

say "dumping $DB_NAME → $BASE.dump"
pg_dump --dbname="$DATABASE_URL" --format=custom --compress=9 --no-owner --no-privileges \
        --file="$BASE.dump"
pg_dump --dbname="$DATABASE_URL" --format=plain --no-owner --no-privileges \
        --file="$BASE.sql"

say "writing manifest"
{
  echo "stamp        : $STAMP"
  echo "database     : $DB_NAME"
  echo "host         : $(hostname)"
  echo "server       : $("${PSQL[@]}" -c 'select version()')"
  echo "dump bytes   : $(stat -c%s "$BASE.dump")"
  echo "sha256 dumps : $(sha256sum "$BASE.dump" | cut -d' ' -f1)"
  echo "row counts   :"
  for table in articles feeds sources article_clusters article_categories push_events admin_users; do
    printf '  %-20s %s\n' "$table" "$("${PSQL[@]}" -c "select count(*) from $table" 2>/dev/null || echo n/a)"
  done
} | tee "$BASE.manifest"

# A dump nobody has ever restored is not a backup: verify it is readable by listing contents.
say "verifying the dump is readable"
pg_restore --list "$BASE.dump" >/dev/null || die "the dump cannot be listed — treat it as corrupt"
ok "pg_restore --list: $(pg_restore --list "$BASE.dump" | grep -c 'TABLE DATA') tables of data"

say "pruning dumps older than $RETENTION_DAYS days"
find "$BACKUP_DIR" -name 'afnews-*.dump' -mtime "+$RETENTION_DAYS" -print -delete | sed 's/^/  removed /' || true
find "$BACKUP_DIR" -name 'afnews-*.sql'  -mtime "+$RETENTION_DAYS" -print -delete | sed 's/^/  removed /' || true

printf '%s  %s  %s bytes\n' "$STAMP" "$BASE.dump" "$(stat -c%s "$BASE.dump")" >> "$LOG"
ok "backup complete: $BASE.dump ($(du -h "$BASE.dump" | cut -f1))"
