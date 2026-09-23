#!/usr/bin/env bash
# First-run bootstrap: migrate, import the bundled OPML feed pack, activate the core wave.
# Idempotent: safe to re-run (imports reconcile, never duplicate).
set -euo pipefail

API_BASE="${API_BASE:-http://localhost:8080}"
ADMIN_USER="${ADMIN_USER:-admin}"
ADMIN_PASSWORD="${ADMIN_PASSWORD:-afnews-admin}"
FEED_PACK="${FEED_PACK:-resources/feedpacks/afghanistan-global-news-master-v0.3.opml}"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

echo "== waiting for api at ${API_BASE} =="
for _ in $(seq 1 60); do
  if curl -fsS "${API_BASE}/health/ready" >/dev/null 2>&1; then break; fi
  sleep 1
done

echo "== feed-pack dry run =="
TOKEN="$(curl -fsS -X POST "${API_BASE}/admin/api/login" \
  -H 'Content-Type: application/json' \
  -d "{\"username\":\"${ADMIN_USER}\",\"password\":\"${ADMIN_PASSWORD}\"}" | python3 -c 'import json,sys;print(json.load(sys.stdin)["token"])')"

curl -fsS -X POST "${API_BASE}/admin/api/feedpacks/import?path=${FEED_PACK}" \
  -H "Authorization: Bearer ${TOKEN}" -H 'Content-Type: application/json' -d '{}' \
  | python3 -c 'import json,sys;d=json.load(sys.stdin);print(f"  version={d[\"version\"]} total={d[\"total\"]} new={d[\"new\"]} changed={d[\"changed\"]} invalid={d[\"invalid\"]}")'

read -r -p "Commit this pack? [y/N] " answer
case "${answer}" in
  [yY]*) ;;
  *) echo "aborted (registry unchanged)"; exit 0 ;;
esac

echo "== committing =="
curl -fsS -X POST "${API_BASE}/admin/api/feedpacks/import?commit=true&path=${FEED_PACK}" \
  -H "Authorization: Bearer ${TOKEN}" -H 'Content-Type: application/json' -d '{}' \
  | python3 -c 'import json,sys;d=json.load(sys.stdin);print(f"  committed={d[\"committed\"]} inserted={d.get(\"insertedCount\",0)} updated={d.get(\"updatedCount\",0)}")'

echo "== activating wave 1 (direct + validated + official) =="
curl -fsS -X POST "${API_BASE}/admin/api/activation" \
  -H "Authorization: Bearer ${TOKEN}" -H 'Content-Type: application/json' -d '{"wave":1}'
echo
echo "bootstrap complete — see ${API_BASE}/admin/"
