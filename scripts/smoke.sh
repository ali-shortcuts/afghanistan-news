#!/usr/bin/env bash
# End-to-end smoke test against a running environment.
set -euo pipefail
API_BASE="${API_BASE:-http://localhost:8080}"
fail=0

check() {
  local label="$1" url="$2"
  local code
  code="$(curl -s -o /dev/null -w '%{http_code}' "${url}")"
  if [ "${code}" = "200" ]; then
    printf '  ok   %-28s %s\n' "${label}" "${code}"
  else
    printf '  FAIL %-28s %s\n' "${label}" "${code}"
    fail=1
  fi
}

echo "== public api =="
check "health/ready" "${API_BASE}/health/ready"
check "v1/home" "${API_BASE}/v1/home"
check "v1/articles" "${API_BASE}/v1/articles?limit=5"
check "v1/categories" "${API_BASE}/v1/categories"
check "v1/provinces" "${API_BASE}/v1/provinces"
check "v1/sources" "${API_BASE}/v1/sources?limit=5"
check "v1/config" "${API_BASE}/v1/config"
check "v1/feed-pack/version" "${API_BASE}/v1/feed-pack/version"
check "v1/notifications" "${API_BASE}/v1/notifications"
check "metrics" "${API_BASE}/metrics"
check "admin bundle" "${API_BASE}/admin/"
check "mobile web bundle" "${API_BASE}/app/"

echo "== admin auth =="
TOKEN="$(curl -fsS -X POST "${API_BASE}/admin/api/login" -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"afnews-admin"}' \
  | python3 -c 'import json,sys;print(json.load(sys.stdin)["token"])')"
if [ -n "${TOKEN}" ]; then echo "  ok   admin login"; else echo "  FAIL admin login"; fail=1; fi

for path in dashboard feeds articles feedpacks push/events audit reference activation; do
  code="$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer ${TOKEN}" "${API_BASE}/admin/api/${path}")"
  if [ "${code}" = "200" ]; then printf '  ok   admin/api/%-20s %s\n' "${path}" "${code}"
  else printf '  FAIL admin/api/%-20s %s\n' "${path}" "${code}"; fail=1; fi
done

echo "== article detail =="
ARTICLE_ID="$(curl -fsS "${API_BASE}/v1/articles?limit=1" | python3 -c 'import json,sys;items=json.load(sys.stdin)["items"];print(items[0]["id"] if items else "")')"
if [ -n "${ARTICLE_ID}" ]; then
  check "v1/articles/{id}" "${API_BASE}/v1/articles/${ARTICLE_ID}"
else
  echo "  warn registry has no articles yet (run the worker, then re-run)"
fi

exit "${fail}"
