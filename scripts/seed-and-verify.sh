#!/usr/bin/env bash
# Prove the whole stack works against a database that starts EMPTY.
#
# This is the CI scenario in one command: drop a scratch PostgreSQL database, migrate it,
# load the repository fixtures (reference data → OPML feed pack → activation wave →
# articles through the real parser and the real dedup path), boot the API against it and
# run the full acceptance suite. Anything that only works because a developer's database
# happens to be full of content cannot hide here.
#
#   bash scripts/seed-and-verify.sh            # scratch database afnews_verify
#   DB=other bash scripts/seed-and-verify.sh   # use another name
#   PORT=8090 bash scripts/seed-and-verify.sh  # another port
#
# Exits non-zero if any acceptance check fails.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}"

DB="${DB:-afnews_verify}"
PORT="${PORT:-8097}"
PG_USER="${PG_USER:-afnews}"
PG_PASSWORD="${PG_PASSWORD:-afnews}"
PG_HOST="${PG_HOST:-127.0.0.1}"
PG_PORT="${PG_PORT:-5432}"
DSN="postgres://${PG_USER}:${PG_PASSWORD}@${PG_HOST}:${PG_PORT}/${DB}?sslmode=disable"
RUN_DIR="${ROOT}/run"
LOG_DIR="${ROOT}/logs"
mkdir -p "${RUN_DIR}" "${LOG_DIR}"

say()  { printf '\033[1m==\033[0m %s\n' "$*"; }
ok()   { printf '  \033[32mok\033[0m   %s\n' "$*"; }
bad()  { printf '  \033[31mFAIL\033[0m %s\n' "$*"; }

stop_api() {
  if [ -f "${RUN_DIR}/verify.pid" ]; then
    local pid; pid="$(cat "${RUN_DIR}/verify.pid" 2>/dev/null || true)"
    if [ -n "${pid}" ] && kill -0 "${pid}" 2>/dev/null; then kill "${pid}" 2>/dev/null || true; fi
    rm -f "${RUN_DIR}/verify.pid"
  fi
}
trap stop_api EXIT

say "1/4 a database that starts empty (${DB})"
if command -v psql >/dev/null 2>&1; then
  psql "postgres://${PG_USER}:${PG_PASSWORD}@${PG_HOST}:${PG_PORT}/postgres?sslmode=disable" -q -c \
    "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname='${DB}' AND pid<>pg_backend_pid()" >/dev/null
  psql "postgres://${PG_USER}:${PG_PASSWORD}@${PG_HOST}:${PG_PORT}/postgres?sslmode=disable" -q -c \
    "DROP DATABASE IF EXISTS ${DB}" -c "CREATE DATABASE ${DB} OWNER ${PG_USER}" >/dev/null
  ok "dropped and recreated ${DB}"
else
  bad "psql not found — start PostgreSQL first (make pg-up)"
  exit 1
fi

say "2/4 migrate + seed the repository fixtures"
cd "${ROOT}/backend"
DATABASE_URL="${DSN}" go run ./cmd/seed-fixtures -driver postgres \
  -pack resources/feedpacks/afghanistan-global-news-master-v0.2.opml \
  -fixtures testdata/feeds -wave 1 -synth 8

say "3/4 boot the API against the seeded database (port ${PORT})"
# Anything still holding the verify port is a previous run of this script: reap it, so the
# suite never talks to a stale process from an older build.
STALE="$(ss -ltnpH "sport = :${PORT}" 2>/dev/null | grep -o 'pid=[0-9]*' | head -1 | cut -d= -f2 || true)"
if [ -n "${STALE}" ]; then kill "${STALE}" 2>/dev/null || true; sleep 1; fi

# The pid is written by the child itself: `setsid` may fork, so $! would be the wrong process
# and the trap would leave a live server behind (it did).
setsid bash -c "echo \$\$ > '${RUN_DIR}/verify.pid'
  exec env DATABASE_URL='${DSN}' DB_DRIVER=postgres HTTP_ADDR='0.0.0.0:${PORT}' \
    WORKER_ENABLED=false ENV=test PUSH_DRY_RUN=true \
    ADMIN_SESSION_KEY=seed-and-verify-session-key-32-bytes \
    ADMIN_STATIC_DIR='${ROOT}/admin' APP_STATIC_DIR='${ROOT}/app' RATE_LIMIT_PER_MINUTE=600 \
    '${ROOT}/bin/afnews-api'" </dev/null >"${LOG_DIR}/verify-api.log" 2>&1 &
sleep 1

ready=false
for _ in $(seq 1 30); do
  if curl -fsS -m 2 "http://127.0.0.1:${PORT}/health/ready" >/dev/null 2>&1; then ready=true; break; fi
  sleep 1
done
if [ "${ready}" != "true" ]; then
  bad "API did not become ready — see ${LOG_DIR}/verify-api.log"
  exit 1
fi
ok "$(curl -s "http://127.0.0.1:${PORT}/health/ready")"

say "4/4 full acceptance suite against the freshly seeded database"
cd "${ROOT}"
API_BASE="http://127.0.0.1:${PORT}" bash scripts/acceptance.sh
