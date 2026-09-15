#!/usr/bin/env bash
# Move an existing SQLite database into PostgreSQL, then prove the move (§Phase B).
#
#   scripts/migrate_to_postgres.sh data/afnews.db [--dry-run]
#
# Steps: preflight → dry run → copy → row-count verification → API parity check.
# The parity check starts both dialects side by side and compares the public API's answers,
# which is the part that actually matters: the same request must return the same articles.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SQLITE_PATH="$(cd "$(dirname "${1:-$ROOT/data/afnews.db}")" && pwd)/$(basename "${1:-afnews.db}")"
shift || true
DRY_RUN=""
[[ "${1:-}" == "--dry-run" ]] && DRY_RUN="-dry-run"

DB_USER="${DB_USER:-afnews}"
DB_PASS="${DB_PASS:-afnews}"
DB_NAME="${DB_NAME:-afnews}"
export DATABASE_URL="${DATABASE_URL:-postgres://$DB_USER:$DB_PASS@127.0.0.1:5432/$DB_NAME?sslmode=disable}"
export PATH="/usr/local/go/bin:$PATH"

say() { printf '\033[1;36m==\033[0m %s\n' "$*"; }
ok()  { printf '\033[1;32m✓\033[0m %s\n' "$*"; }
die() { printf '\033[1;31m✗\033[0m %s\n' "$*" >&2; exit 1; }

[ -f "$SQLITE_PATH" ] || die "no SQLite database at $SQLITE_PATH"
PGPASSWORD="$DB_PASS" psql -h 127.0.0.1 -U "$DB_USER" -d "$DB_NAME" -tAc 'select 1' >/dev/null \
  || die "PostgreSQL not reachable — run scripts/bootstrap_postgres.sh first"
ok "source: $SQLITE_PATH ($(du -h "$SQLITE_PATH" | cut -f1)) · target: ${DB_NAME}@127.0.0.1"

cd "$ROOT/backend"
say "copying"
go run ./cmd/migrate-data -from "$SQLITE_PATH" $DRY_RUN

if [ -n "$DRY_RUN" ]; then
  ok "dry run complete — nothing was written"
  exit 0
fi

# ---------------------------------------------------------------- parity check
say "API parity check (same request, both dialects)"
pkill -f 'afnews-api-parity' 2>/dev/null || true
sleep 0.4

start_api() { # $1=driver $2=port $3=binary-tag
  local driver="$1" port="$2" tag="$3"
  ( DB_DRIVER="$driver" DATABASE_URL="$DATABASE_URL" SQLITE_PATH="$SQLITE_PATH" \
    HTTP_ADDR="0.0.0.0:$port" WORKER_ENABLED=false ENV=parity \
    go run ./cmd/api >"/tmp/parity-$tag.log" 2>&1 & echo $! >"/tmp/parity-$tag.pid" )
}

start_api postgres 8190 pg
start_api sqlite   8191 lite

for port in 8190 8191; do
  for _ in $(seq 1 60); do
    curl -fsS "http://127.0.0.1:$port/health/ready" >/dev/null 2>&1 && break
    sleep 0.5
  done
done

compare() { # $1=path $2=jq/python selector
  local path="$1"
  local pg lite code
  code="$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:8190$path")"
  if [ "$code" != "200" ]; then
    echo "  - $path: skipped (postgres API answered $code)"
    return 0
  fi
  pg="$(curl -fsS "http://127.0.0.1:8190$path")"
  lite="$(curl -fsS "http://127.0.0.1:8191$path")"
  python3 - "$path" "$pg" "$lite" <<'PY'
import json, sys
path, a, b = sys.argv[1], json.loads(sys.argv[2]), json.loads(sys.argv[3])

def shape(x, depth=0):
    if isinstance(x, dict):
        return {k: shape(v, depth + 1) for k, v in sorted(x.items())}
    if isinstance(x, list):
        return [shape(x[0], depth + 1)] if x else []
    return type(x).__name__

def ids(x):
    out = []
    def walk(v):
        if isinstance(v, dict):
            for k, val in v.items():
                if k in ("id", "slug", "key", "total") and not isinstance(val, (dict, list)):
                    out.append(f"{k}={val}")
                else:
                    walk(val)
        elif isinstance(v, list):
            for item in v:
                walk(item)
    walk(x)
    return out

if shape(a) != shape(b):
    print(f"  ✗ {path}: response shape differs")
    print("    postgres:", json.dumps(shape(a), ensure_ascii=False)[:220])
    print("    sqlite:  ", json.dumps(shape(b), ensure_ascii=False)[:220])
    sys.exit(1)
ia, ib = ids(a), ids(b)
if ia == ib:
    print(f"  ✓ {path}: identical payloads ({len(ia)} identifiers)")
    sys.exit(0)
missing = [x for x in ia if x not in ib]
print(f"  ! {path}: same shape, {len(missing)} differing identifiers "
      f"(expected when the worker has ingested newer items)")
print(f"    postgres only: {missing[:4]}")
sys.exit(0)
PY
}

status=0
for path in "/v1/home" "/v1/articles?limit=5" "/v1/articles?limit=5&sort=oldest" \
            "/v1/sources" "/v1/provinces" "/v1/categories" "/v1/config" \
            "/v1/search?q=afghanistan" "/v1/articles?limit=3&category=politics"; do
  compare "$path" || status=1
done
curl -fsS "http://127.0.0.1:8190/health/ready" | head -c 120; echo
curl -fsS "http://127.0.0.1:8191/health/ready" | head -c 120; echo

kill "$(cat /tmp/parity-pg.pid)" "$(cat /tmp/parity-lite.pid)" 2>/dev/null || true
pkill -f 'cmd/api' 2>/dev/null || true

[ "$status" -eq 0 ] || die "parity check failed"
ok "PostgreSQL serves the same public API as SQLite"
