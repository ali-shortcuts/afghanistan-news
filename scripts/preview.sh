#!/usr/bin/env bash
# Bring the whole platform up from a cold machine — one command, idempotent (§Phase B).
#
#   scripts/preview.sh prepare   # PostgreSQL + schema + content, nothing served yet
#   scripts/preview.sh start     # api (no embedded worker) + standalone worker, detached
#   scripts/preview.sh restart
#   scripts/preview.sh status
#   scripts/preview.sh stop
#   scripts/preview.sh all       # prepare + start + status
#
# Why this exists: a fresh container has no PostgreSQL, no Go toolchain and no running processes,
# and the file mode bits of scripts do not survive a snapshot. Everything here is safe to re-run
# and never destroys existing content — an existing database is left alone.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

DB_USER="${DB_USER:-afnews}"; DB_PASS="${DB_PASS:-afnews}"; DB_NAME="${DB_NAME:-afnews}"
PG_URL="postgres://$DB_USER:$DB_PASS@127.0.0.1:5432/$DB_NAME?sslmode=disable"
export DATABASE_URL="${DATABASE_URL:-$PG_URL}"
export DB_DRIVER="${DB_DRIVER:-postgres}"
export HTTP_ADDR="${HTTP_ADDR:-0.0.0.0:8080}"
export PATH="/usr/local/go/bin:$PATH"

say() { printf '\033[1;36m==\033[0m %s\n' "$*"; }
ok()  { printf '\033[1;32m✓\033[0m %s\n' "$*"; }
bad() { printf '\033[1;31m✗\033[0m %s\n' "$*" >&2; }
step() { printf '\033[1;35m·\033[0m %s\n' "$*"; }

# --------------------------------------------------------------------------- 0. file modes
# File permissions are not part of the workspace snapshot, so scripts come back non-executable.
fix_modes() {
  chmod +x "$ROOT"/scripts/*.sh 2>/dev/null || true
  chmod +x "$ROOT"/bin/* 2>/dev/null || true
}

# --------------------------------------------------------------------------- 1. PostgreSQL
pg_installed() { command -v psql >/dev/null 2>&1 && command -v pg_ctlcluster >/dev/null 2>&1; }

install_postgres() {
  pg_installed && return 0
  step "installing PostgreSQL (first run on this machine)"
  export DEBIAN_FRONTEND=noninteractive
  sudo apt-get install -y -qq postgresql postgresql-contrib >/dev/null 2>&1 \
    || { bad "could not install PostgreSQL (no network or no sudo?)"; return 1; }
  ok "PostgreSQL installed: $(psql --version)"
}

start_postgres() {
  local online
  online="$(pg_lsclusters -h 2>/dev/null | awk 'NR==1{print $4}')"
  if [ "$online" != "online" ]; then
    step "starting the PostgreSQL cluster"
    sudo pg_ctlcluster "$(pg_lsclusters -h | awk 'NR==1{print $1}')" \
                       "$(pg_lsclusters -h | awk 'NR==1{print $2}')" start 2>/dev/null || true
  fi
  for _ in $(seq 1 30); do
    sudo -u postgres psql -tAc 'select 1' >/dev/null 2>&1 && { ok "PostgreSQL accepting connections"; return 0; }
    sleep 1
  done
  bad "PostgreSQL did not come up"
  return 1
}

ensure_role_and_db() {
  sudo -u postgres psql -tAc "select 1 from pg_roles where rolname='$DB_USER'" 2>/dev/null | grep -q 1 \
    || sudo -u postgres psql -c "CREATE USER $DB_USER WITH PASSWORD '$DB_PASS' SUPERUSER;" >/dev/null
  sudo -u postgres psql -tAc "select 1 from pg_database where datname='$DB_NAME'" 2>/dev/null | grep -q 1 \
    || sudo -u postgres createdb -O "$DB_USER" "$DB_NAME"
  ok "role $DB_USER · database $DB_NAME present"
}

# --------------------------------------------------------------------------- 2. content
db_articles() {
  PGPASSWORD="$DB_PASS" psql -h 127.0.0.1 -U "$DB_USER" -d "$DB_NAME" -X -q -tA \
    -c 'select count(*) from articles' 2>/dev/null || echo 0
}

load_content() {
  local articles; articles="$(db_articles)"
  if [ "${articles:-0}" -gt 100 ]; then
    ok "database already populated ($articles articles) — leaving it alone"
    return 0
  fi
  local dump; dump="$(ls -t "$ROOT"/backups/*.dump 2>/dev/null | head -1 || true)"
  if [ -n "$dump" ]; then
    step "restoring $dump"
    if "$ROOT/scripts/restore.sh" "$dump" --into "$DB_NAME" >/tmp/preview-restore.log 2>&1; then
      ok "restored from backup: $(db_articles) articles"
      return 0
    fi
    bad "restore failed — see /tmp/preview-restore.log"
  fi
  if [ -f "$ROOT/data/afnews.db" ] && [ -x "$ROOT/bin/afnews-migrate-data" ]; then
    step "no dump found — migrating data/afnews.db"
    "$ROOT/bin/afnews-migrate-data" -from "$ROOT/data/afnews.db" 2>&1 | tail -3
    ok "migrated: $(db_articles) articles"
    return 0
  fi
  ok "starting with an empty database — the worker will populate it from the feed pack"
}

# --------------------------------------------------------------------------- 3. binaries
ensure_binaries() {
  if [ -x "$ROOT/bin/afnews-api" ] && [ -x "$ROOT/bin/afnews-worker" ]; then
    ok "binaries present ($(du -h "$ROOT/bin/afnews-api" | cut -f1) api, $(du -h "$ROOT/bin/afnews-worker" | cut -f1) worker)"
    return 0
  fi
  if ! command -v go >/dev/null 2>&1; then
    step "installing the Go toolchain (binaries are missing and need a build)"
    local tgz; tgz="$(mktemp)"
    curl -sSL -o "$tgz" https://go.dev/dl/go1.25.6.linux-amd64.tar.gz || { bad "Go download failed"; return 1; }
    sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf "$tgz" && rm -f "$tgz"
  fi
  step "building api + worker"
  ( cd "$ROOT/backend" && go build -trimpath -o "$ROOT/bin/afnews-api" ./cmd/api \
      && go build -trimpath -o "$ROOT/bin/afnews-worker" ./cmd/worker \
      && go build -trimpath -o "$ROOT/bin/afnews-migrate-data" ./cmd/migrate-data ) \
    || { bad "build failed"; return 1; }
  ok "binaries built"
}

# --------------------------------------------------------------------------- 4. processes
API_PID="$ROOT/run/api.pid"; WORKER_PID="$ROOT/run/worker.pid"

kill_port() {
  local port="${HTTP_ADDR##*:}" pid
  for pid in $(ss -ltnHp 2>/dev/null | grep ":$port " | grep -o 'pid=[0-9]*' | cut -d= -f2 | sort -u); do
    [ "$pid" = "$$" ] && continue
    kill "$pid" 2>/dev/null || true; sleep 0.3; kill -9 "$pid" 2>/dev/null || true
  done
}

start_process_bg() { # $1=name $2=pidfile $3=binary $4..=env
  local name="$1" pidfile="$2" binary="$3"; shift 3
  if [ -f "$pidfile" ] && kill -0 "$(cat "$pidfile")" 2>/dev/null; then
    ok "$name already running (pid $(cat "$pidfile"))"; return 0
  fi
  ( cd "$ROOT/backend" && setsid env "$@" "$binary" </dev/null >>"$ROOT/logs/$name.log" 2>&1 & echo $! >"$pidfile" )
  sleep 1
  if kill -0 "$(cat "$pidfile" 2>/dev/null)" 2>/dev/null; then
    ok "$name started (pid $(cat "$pidfile"))"
  else
    bad "$name failed to start — tail of $ROOT/logs/$name.log"
    tail -3 "$ROOT/logs/$name.log" >&2
    return 1
  fi
}

start_stack() {
  mkdir -p "$ROOT/run" "$ROOT/logs"
  kill_port
  : >"$ROOT/logs/api.log"; : >"$ROOT/logs/worker.log"

  start_process_bg api "$API_PID" "$ROOT/bin/afnews-api" \
    ENV="${ENV:-production}" DATABASE_URL="$DATABASE_URL" DB_DRIVER="$DB_DRIVER" HTTP_ADDR="$HTTP_ADDR" \
    WORKER_ENABLED=false \
    ADMIN_SESSION_KEY="${ADMIN_SESSION_KEY:-local-dev-session-key-32-bytes-min}" \
    ADMIN_STATIC_DIR="$ROOT/admin" APP_STATIC_DIR="$ROOT/app" \
    FEED_PACK_PATH="$ROOT/backend/resources/feedpacks/afghanistan-global-news-master-v0.3.opml" \
    FEED_ACTIVATION_WAVE="${FEED_ACTIVATION_WAVE:-1}" \
    PUSH_DRY_RUN="${PUSH_DRY_RUN:-true}" ALLOW_PRIVATE_FETCH=false \
    RATE_LIMIT_PER_MINUTE="${RATE_LIMIT_PER_MINUTE:-600}" || return 1

  for _ in $(seq 1 40); do
    curl -fsS "http://127.0.0.1:${HTTP_ADDR##*:}/health/ready" >/dev/null 2>&1 && break
    sleep 0.5
  done

  start_process_bg worker "$WORKER_PID" "$ROOT/bin/afnews-worker" \
    ENV="${ENV:-production}" DATABASE_URL="$DATABASE_URL" DB_DRIVER="$DB_DRIVER" \
    WORKER_CONCURRENCY="${WORKER_CONCURRENCY:-8}" \
    FEED_PACK_PATH="$ROOT/backend/resources/feedpacks/afghanistan-global-news-master-v0.3.opml" \
    FEED_ACTIVATION_WAVE="${FEED_ACTIVATION_WAVE:-1}" \
    PUSH_DRY_RUN="${PUSH_DRY_RUN:-true}" ALLOW_PRIVATE_FETCH=false || true
}

stop_stack() {
  for f in "$WORKER_PID" "$API_PID"; do
    if [ -f "$f" ]; then kill "$(cat "$f")" 2>/dev/null || true; rm -f "$f"; fi
  done
  kill_port
  ok "stack stopped"
}

status() {
  echo
  printf '%-8s %-9s %-7s %s\n' PROCESS STATE PID DETAIL
  for pair in "api:$API_PID" "worker:$WORKER_PID"; do
    local name="${pair%%:*}" pidfile="${pair##*:}" state=stopped pid=- detail=""
    if [ -f "$pidfile" ] && kill -0 "$(cat "$pidfile")" 2>/dev/null; then
      pid="$(cat "$pidfile")"; state=running
    fi
    # The API may have been started by a supervisor (systemd, a container runtime, the preview
    # manager) rather than by this script: report what is actually listening, not what we own.
    if [ "$name" = "api" ] && [ "$state" = stopped ]; then
      pid="$(ss -ltnHp 2>/dev/null | grep ":${HTTP_ADDR##*:} " | grep -o 'pid=[0-9]*' | cut -d= -f2 | sort -u | head -1)"
      [ -n "$pid" ] && { state="running*"; }
    fi
    [ "$state" != stopped ] && detail="$(ps -o rss= -p "$pid" 2>/dev/null | awk '{printf "%.0f MB RSS", $1/1024}')"
    printf '%-8s %-9s %-7s %s\n' "$name" "$state" "$pid" "$detail"
  done
  echo "         * started outside this script (supervisor/preview manager)"
  echo
  printf '  health : %s\n' "$(curl -s -m 3 "http://127.0.0.1:${HTTP_ADDR##*:}/health/ready" || echo 'unreachable')"
  printf '  content: %s articles · %s feeds\n' \
    "$(PGPASSWORD="$DB_PASS" psql -h 127.0.0.1 -U "$DB_USER" -d "$DB_NAME" -X -q -tA -c 'select count(*) from articles' 2>/dev/null || echo '?')" \
    "$(PGPASSWORD="$DB_PASS" psql -h 127.0.0.1 -U "$DB_USER" -d "$DB_NAME" -X -q -tA -c 'select count(*) from feeds' 2>/dev/null || echo '?')"
  echo "  surfaces:"
  for p in / /app/ /admin/ /v1/home /metrics; do
    printf '    %-12s %s\n' "$p" "$(curl -s -m 3 -o /dev/null -w '%{http_code}' "http://127.0.0.1:${HTTP_ADDR##*:}$p")"
  done
}

prepare() {
  fix_modes
  install_postgres || return 1
  start_postgres || return 1
  ensure_role_and_db
  load_content
}

case "${1:-all}" in
  prepare) prepare ;;
  start)   prepare && ensure_binaries && start_stack && status ;;
  restart) prepare && ensure_binaries && stop_stack && start_stack && status ;;
  stop)    stop_stack ;;
  status)  status ;;
  fixmodes) fix_modes && ok "script modes restored" ;;
  all)     prepare && ensure_binaries && start_stack && status
           echo
           echo "  acceptance: ./scripts/acceptance.sh"
           echo "  logs      : $ROOT/logs/{api,worker}.log" ;;
  *) echo "usage: $0 {prepare|start|restart|stop|status|fixmodes|all}" >&2; exit 2 ;;
esac
