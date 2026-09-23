#!/usr/bin/env bash
# Run the production-shaped stack without Docker (§Phase B): PostgreSQL + a public API process
# + a standalone ingestion worker process. This is the same shape docker-compose.yml describes,
# which matters because it means the compose file is exercised in development too.
#
#   scripts/run_production_stack.sh start     # build and launch api + worker
#   scripts/run_production_stack.sh status
#   scripts/run_production_stack.sh logs [api|worker]
#   scripts/run_production_stack.sh stop
#   scripts/run_production_stack.sh restart
#
# Environment: DATABASE_URL, HTTP_ADDR (default 0.0.0.0:8080), WORKER_CONCURRENCY,
# FEED_ACTIVATION_WAVE, PUSH_DRY_RUN, ADMIN_SESSION_KEY.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RUN_DIR="${RUN_DIR:-$ROOT/run}"
LOG_DIR="${LOG_DIR:-$ROOT/logs}"
BIN_DIR="$ROOT/bin"

DB_USER="${DB_USER:-afnews}"
DB_PASS="${DB_PASS:-afnews}"
DB_NAME="${DB_NAME:-afnews}"
export DATABASE_URL="${DATABASE_URL:-postgres://$DB_USER:$DB_PASS@127.0.0.1:5432/$DB_NAME?sslmode=disable}"
export DB_DRIVER="${DB_DRIVER:-postgres}"
export HTTP_ADDR="${HTTP_ADDR:-0.0.0.0:8080}"
export PATH="/usr/local/go/bin:$PATH"

say() { printf '\033[1;36m==\033[0m %s\n' "$*"; }
ok()  { printf '\033[1;32m✓\033[0m %s\n' "$*"; }
bad() { printf '\033[1;31m✗\033[0m %s\n' "$*" >&2; }

pit() { echo "$RUN_DIR/$1.pid"; }

build() {
  mkdir -p "$BIN_DIR" "$RUN_DIR" "$LOG_DIR"
  say "building"
  ( cd "$ROOT/backend" && go build -o "$BIN_DIR/afnews-api" ./cmd/api && go build -o "$BIN_DIR/afnews-worker" ./cmd/worker )
  ok "$(du -h "$BIN_DIR/afnews-api" | cut -f1) api · $(du -h "$BIN_DIR/afnews-worker" | cut -f1) worker"
}

wait_ready() {
  local url="$1" name="$2"
  for _ in $(seq 1 60); do
    if curl -fsS "$url" >/dev/null 2>&1; then ok "$name ready"; return 0; fi
    sleep 0.5
  done
  bad "$name did not become ready — see $LOG_DIR/$name.log"
  return 1
}

# Both processes are started from backend/, because resource paths (the bundled feed pack,
# admin/app static bundles when they are not overridden) are relative to it. Getting this wrong
# is silent: the API boots, and only the feed-pack import later fails with "no such file".
start_one() { # $1=name $2=binary $3..=env assignments
  local name="$1" binary="$2"; shift 2
  local pid_file; pid_file="$(pit "$name")"
  if [ -f "$pid_file" ] && kill -0 "$(cat "$pid_file")" 2>/dev/null; then
    ok "$name already running (pid $(cat "$pid_file"))"
    return 0
  fi
  # setsid + full redirection: the child must not inherit this script's stdout, or a caller
  # piping the output would block until the server exits.
  (
    cd "$ROOT/backend" || exit 1
    setsid env "$@" "$BIN_DIR/$binary" </dev/null >>"$LOG_DIR/$name.log" 2>&1 &
    echo $! >"$pid_file"
  )
  sleep 0.3
  local pid; pid="$(cat "$pid_file")"
  kill -0 "$pid" 2>/dev/null && ok "$name started (pid $pid)" || bad "$name failed to start (see $LOG_DIR/$name.log)"
}

# Free the listening port. A leftover `go run` process from a debugging session is the most
# common reason a restart silently keeps serving the previous build.
free_port() {
  local port="${HTTP_ADDR##*:}" pid
  for pid in $(ss -ltnHp 2>/dev/null | grep ":$port " | grep -o 'pid=[0-9]*' | cut -d= -f2 | sort -u); do
    [ "$pid" = "$$" ] && continue
    if kill -0 "$pid" 2>/dev/null; then
      bad "port $port held by pid $pid — terminating it"
      kill "$pid" 2>/dev/null || true
      sleep 0.5
      kill -9 "$pid" 2>/dev/null || true
    fi
  done
}

start() {
  build
  free_port
  : >"$LOG_DIR/api.log"; : >"$LOG_DIR/worker.log"

  # The public API must never poll feeds: that is the worker's job (§66, §113).
  start_one api afnews-api \
    WORKER_ENABLED=false ENV="${ENV:-production}" HTTP_ADDR="$HTTP_ADDR" \
    DB_DRIVER="$DB_DRIVER" DATABASE_URL="$DATABASE_URL" \
    FEED_ACTIVATION_WAVE="${FEED_ACTIVATION_WAVE:-1}" \
    ADMIN_SESSION_KEY="${ADMIN_SESSION_KEY:-local-dev-session-key-32-bytes-min}" \
    ADMIN_STATIC_DIR="$ROOT/admin" APP_STATIC_DIR="$ROOT/app" \
    FEED_PACK_PATH="$ROOT/backend/resources/feedpacks/afghanistan-global-news-master-v0.3.opml" \
    PUSH_DRY_RUN="${PUSH_DRY_RUN:-true}" ALLOW_PRIVATE_FETCH="${ALLOW_PRIVATE_FETCH:-false}" \
    RATE_LIMIT_PER_MINUTE="${RATE_LIMIT_PER_MINUTE:-600}"

  wait_ready "http://127.0.0.1:${HTTP_ADDR##*:}/health/ready" api || return 1

  start_one worker afnews-worker \
    ENV="${ENV:-production}" DB_DRIVER="$DB_DRIVER" DATABASE_URL="$DATABASE_URL" \
    WORKER_CONCURRENCY="${WORKER_CONCURRENCY:-8}" FEED_ACTIVATION_WAVE="${FEED_ACTIVATION_WAVE:-1}" \
    FEED_PACK_PATH="$ROOT/backend/resources/feedpacks/afghanistan-global-news-master-v0.3.opml" \
    PUSH_DRY_RUN="${PUSH_DRY_RUN:-true}" ALLOW_PRIVATE_FETCH="${ALLOW_PRIVATE_FETCH:-false}" \
    RATE_LIMIT_PER_MINUTE="${RATE_LIMIT_PER_MINUTE:-600}"

  status
}

stop_one() {
  local name="$1" pid_file; pid_file="$(pit "$name")"
  if [ -f "$pid_file" ]; then
    local pid; pid="$(cat "$pid_file")"
    if kill -0 "$pid" 2>/dev/null; then
      kill "$pid" 2>/dev/null || true
      for _ in $(seq 1 20); do kill -0 "$pid" 2>/dev/null || break; sleep 0.25; done
      kill -9 "$pid" 2>/dev/null || true
      ok "$name stopped"
    fi
    rm -f "$pid_file"
  fi
}

stop() { stop_one worker; stop_one api; }

status() {
  echo
  printf '%-8s %-8s %-8s %s\n' PROCESS STATE PID DETAIL
  for name in api worker; do
    local pid_file pid state detail
    pid_file="$(pit "$name")"; state="stopped"; pid="-"; detail=""
    if [ -f "$pid_file" ]; then
      pid="$(cat "$pid_file")"
      if kill -0 "$pid" 2>/dev/null; then state="running"; else state="dead"; fi
      detail="$(ps -o rss= -p "$pid" 2>/dev/null | awk '{printf "%.0f MB RSS", $1/1024}')"
    fi
    printf '%-8s %-8s %-8s %s\n' "$name" "$state" "$pid" "$detail"
  done

  echo
  if curl -fsS "http://127.0.0.1:${HTTP_ADDR##*:}/health/ready" 2>/dev/null; then
    echo
  fi
  local n
  n="$(psql "$DATABASE_URL" -X -q -tA -c 'select count(*) from articles' 2>/dev/null || echo '?')"
  echo "  articles in postgres: $n"
  echo "  api    : curl -s http://127.0.0.1:${HTTP_ADDR##*:}/v1/home | head -c 200"
  echo "  logs   : $LOG_DIR/api.log · $LOG_DIR/worker.log"
}

case "${1:-start}" in
  start)   start ;;
  stop)    stop ;;
  restart) stop; sleep 0.5; start ;;
  status)  status ;;
  logs)    tail -n "${2:-40}" "$LOG_DIR/${3:-api}.log" ;;
  build)   build ;;
  *) echo "usage: $0 {start|stop|restart|status|logs [n] [api|worker]|build}" >&2; exit 2 ;;
esac
