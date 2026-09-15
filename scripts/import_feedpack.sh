#!/usr/bin/env bash
# Dry-run or commit an OPML feed pack.
#   ./scripts/import_feedpack.sh /path/to/pack.opml            # dry run (default)
#   COMMIT=1 ./scripts/import_feedpack.sh /path/to/pack.opml   # commit
set -euo pipefail

API_BASE="${API_BASE:-http://localhost:8080}"
ADMIN_USER="${ADMIN_USER:-admin}"
ADMIN_PASSWORD="${ADMIN_PASSWORD:-afnews-admin}"
PACK_PATH="${1:?usage: import_feedpack.sh <server-side opml path>}"
COMMIT_FLAG="false"
[ "${COMMIT:-0}" = "1" ] && COMMIT_FLAG="true"

TOKEN="$(curl -fsS -X POST "${API_BASE}/admin/api/login" \
  -H 'Content-Type: application/json' \
  -d "{\"username\":\"${ADMIN_USER}\",\"password\":\"${ADMIN_PASSWORD}\"}" \
  | python3 -c 'import json,sys;print(json.load(sys.stdin)["token"])')"

curl -fsS -X POST "${API_BASE}/admin/api/feedpacks/import?commit=${COMMIT_FLAG}&path=${PACK_PATH}" \
  -H "Authorization: Bearer ${TOKEN}" -H 'Content-Type: application/json' -d '{}' \
  | python3 -m json.tool
