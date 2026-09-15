#!/usr/bin/env bash
# Prepare PostgreSQL for the production-shaped stack (§Phase B).
#
# Idempotent: safe to run on a machine that already has PostgreSQL, and safe to re-run.
#   sudo scripts/bootstrap_postgres.sh
#
# After this, start the stack with:
#   scripts/run_production_stack.sh start
set -euo pipefail

DB_NAME="${DB_NAME:-afnews}"
DB_USER="${DB_USER:-afnews}"
DB_PASS="${DB_PASS:-afnews}"
PG_VERSION="${PG_VERSION:-17}"

say() { printf '\033[1;36m==\033[0m %s\n' "$*"; }
ok()  { printf '\033[1;32m✓\033[0m %s\n' "$*"; }

if ! command -v psql >/dev/null 2>&1; then
  say "installing PostgreSQL $PG_VERSION"
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -qq
  apt-get install -y -qq postgresql postgresql-contrib
fi
ok "psql: $(psql --version)"

# Debian/Ubuntu: start the cluster. Other layouts (RDS, managed) already have it running.
if command -v pg_ctlcluster >/dev/null 2>&1; then
  CLUSTER="$(pg_lsclusters -h 2>/dev/null | awk 'NR==1{print $1" "$2}')"
  if [ -n "${CLUSTER:-}" ] && ! pg_lsclusters -h | awk 'NR==1{print $4}' | grep -q online; then
    say "starting cluster $CLUSTER"
    pg_ctlcluster ${CLUSTER} start || true
  fi
fi

for i in $(seq 1 30); do
  if su postgres -c "psql -tAc 'select 1'" >/dev/null 2>&1; then break; fi
  sleep 1
done
su postgres -c "psql -tAc 'select 1'" >/dev/null
ok "server accepting connections"

say "ensuring role and database"
su postgres -c "psql -tAc \"select 1 from pg_roles where rolname='$DB_USER'\"" | grep -q 1 \
  || su postgres -c "psql -c \"CREATE USER $DB_USER WITH PASSWORD '$DB_PASS' SUPERUSER;\""
su postgres -c "psql -tAc \"select 1 from pg_database where datname='$DB_NAME'\"" | grep -q 1 \
  || su postgres -c "createdb -O $DB_USER $DB_NAME"
ok "role $DB_USER · database $DB_NAME"

# TCP + password auth from localhost must work: that is what DATABASE_URL uses.
if pg_hba_file="$(su postgres -c "psql -tAc 'show hba_file'")"; then
  if ! grep -qE '^host\s+all\s+all\s+127\.0\.0\.1/32\s+(scram-sha-256|md5)' "$pg_hba_file"; then
    say "allowing password auth from 127.0.0.1"
    echo "host all all 127.0.0.1/32 scram-sha-256" >> "$pg_hba_file"
    su postgres -c "psql -tAc 'select pg_reload_conf()'" >/dev/null
  fi
fi

PGPASSWORD="$DB_PASS" psql -h 127.0.0.1 -U "$DB_USER" -d "$DB_NAME" -tAc "select current_database();" >/dev/null \
  && ok "DATABASE_URL=postgres://$DB_USER:***@127.0.0.1:5432/$DB_NAME works"

cat <<EOF

Next:
  export DATABASE_URL='postgres://$DB_USER:$DB_PASS@127.0.0.1:5432/$DB_NAME?sslmode=disable'
  scripts/migrate_to_postgres.sh data/afnews.db     # move existing SQLite data (optional)
  scripts/run_production_stack.sh start             # api + worker, no embedded worker
EOF
