#!/usr/bin/env bash
# Start the ingestion worker as a detached background process (§Phase B).
#
#   scripts/start_worker.sh            # uses DATABASE_URL or the local default
#   scripts/start_worker.sh --foreground
#
# The worker is a separate process from the API on purpose: a publisher that hangs for 30
# seconds must not consume a request goroutine. Detaching with setsid + full redirection means
# it survives the shell that launched it (and the preview proxy stays responsive).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="${BIN:-$ROOT/bin/afnews-worker}"
LOG="${LOG:-$ROOT/logs/worker.log}"
PIDFILE="$ROOT/run/worker.pid"

DB_USER="${DB_USER:-afnews}"; DB_PASS="${DB_PASS:-afnews}"; DB_NAME="${DB_NAME:-afnews}"
export DATABASE_URL="${DATABASE_URL:-postgres://$DB_USER:$DB_PASS@127.0.0.1:5432/$DB_NAME?sslmode=disable}"
export DB_DRIVER="${DB_DRIVER:-postgres}"
export ENV="${ENV:-production}"
export WORKER_CONCURRENCY="${WORKER_CONCURRENCY:-8}"
export FEED_ACTIVATION_WAVE="${FEED_ACTIVATION_WAVE:-1}"
export FEED_PACK_PATH="${FEED_PACK_PATH:-$ROOT/backend/resources/feedpacks/afghanistan-global-news-master-v0.2.opml}"
export PUSH_DRY_RUN="${PUSH_DRY_RUN:-true}"
export ALLOW_PRIVATE_FETCH="${ALLOW_PRIVATE_FETCH:-false}"
export PATH="/usr/local/go/bin:$PATH"

mkdir -p "$ROOT/run" "$ROOT/logs"
[ -x "$BIN" ] || { echo "no worker binary at $BIN — run scripts/run_production_stack.sh build" >&2; exit 1; }

if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
  echo "worker already running (pid $(cat "$PIDFILE"))"
  exit 0
fi

if [ "${1:-}" = "--foreground" ]; then
  cd "$ROOT/backend" && exec "$BIN"
fi

# The worker must run from backend/: the feed pack path is relative to it.
cd "$ROOT/backend"
setsid "$BIN" </dev/null >>"$LOG" 2>&1 &
echo $! >"$PIDFILE"
sleep 2
pid="$(cat "$PIDFILE")"
if kill -0 "$pid" 2>/dev/null; then
  echo "worker started (pid $pid) — log: $LOG"
else
  echo "worker exited immediately; last log lines:" >&2
  tail -5 "$LOG" >&2
  exit 1
fi
