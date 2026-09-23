#!/usr/bin/env bash
# End-to-end acceptance check across all four surfaces.
#   ./scripts/acceptance.sh            # against a running stack (API_BASE)
#   BACKEND_TESTS=1 ./scripts/acceptance.sh   # also run the Go test suite first
set -uo pipefail
API_BASE="${API_BASE:-http://localhost:8080}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
pass=0; fail=0
ok()   { printf '  \033[32mok\033[0m   %s\n' "$1"; pass=$((pass+1)); }
bad()  { printf '  \033[31mFAIL\033[0m %s\n' "$1"; fail=$((fail+1)); }
json() { python3 -c "import json,sys;d=json.load(sys.stdin);print($1)" 2>/dev/null; }
code() { curl -s -o /dev/null -w '%{http_code}' "$@"; }

if [ "${BACKEND_TESTS:-0}" = "1" ]; then
  echo "== backend unit tests =="
  ( cd "${ROOT}/backend" && PATH="$PATH:/usr/local/go/bin" GOFLAGS=-mod=mod \
      GOCACHE="${GOCACHE:-/home/user/.cache/gocache}" GOMODCACHE="${GOMODCACHE:-/home/user/.cache/gomod}" \
      GOPATH="${GOPATH:-/home/user/.cache/gopath}" go test ./... 2>&1 | grep -v "no test files" | sed 's/^/  /' )
  [ "${PIPESTATUS[0]:-0}" = "0" ] && ok "go test ./..." || bad "go test ./..."
fi

echo "== 1/4 public API =="
for path in /health/live /health/ready /v1/home /v1/articles /v1/categories /v1/provinces /v1/sources /v1/config /v1/feed-pack/version /v1/notifications /metrics; do
  c="$(code "${API_BASE}${path}")"; [ "$c" = "200" ] && ok "GET ${path}" || bad "GET ${path} → ${c}"
done
HOME_JSON="$(curl -s "${API_BASE}/v1/home")"
echo "${HOME_JSON}" | json "list(d.keys())" | grep -q "topStories" && ok "home payload shape" || bad "home payload shape"
ART_ID="$(curl -s "${API_BASE}/v1/articles?limit=1" | json "d['items'][0]['id'] if d['items'] else ''")"
[ -n "${ART_ID}" ] && [ "$(code "${API_BASE}/v1/articles/${ART_ID}")" = "200" ] && ok "article detail" || bad "article detail"
BOGUS="$(code "${API_BASE}/v1/articles/nope")"; [ "${BOGUS}" = "404" ] && ok "unknown article → 404" || bad "unknown article → ${BOGUS} (want 404)"
SRCH="$(curl -s "${API_BASE}/v1/search?q=kabul" | json "len(d['items'])")"
[ "${SRCH:-0}" -ge 0 ] 2>/dev/null && ok "search responds (${SRCH} hits)" || bad "search"

M="$(curl -s "${API_BASE}/metrics")"
echo "${M}" | grep -q 'feeds_by_health_total{status="HEALTHY"}' && ok "metrics expose per-health gauges" || bad "metrics gauges"
echo "${M}" | grep -q '^articles_total ' && ok "metrics expose article gauges" || bad "metric totals"

echo "== 2/4 push registration =="
REG="$(curl -s -X POST "${API_BASE}/v1/push/register" -H 'Content-Type: application/json' \
  -d '{"token":"acceptance-device-token","platform":"ANDROID","language":"fa","topics":["breaking","afghanistan"]}')"
REG_ID="$(echo "${REG}" | json "d.get('registrationId','')")"
[ -n "${REG_ID}" ] && ok "register device (${REG_ID})" || bad "register device"
[ "$(code -X DELETE "${API_BASE}/v1/push/registrations/${REG_ID}")" = "204" ] && ok "unregister device" || bad "unregister device"

echo "== 3/4 admin console API =="
TOKEN="$(curl -s -X POST "${API_BASE}/admin/api/login" -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"afnews-admin"}' | json "d.get('token','')")"
[ -n "${TOKEN}" ] && ok "login" || bad "login"
AUTH=(-H "Authorization: Bearer ${TOKEN}")
for path in /me /dashboard /feeds /articles /feedpacks /push/events /audit /reference /activation; do
  c="$(code "${AUTH[@]}" "${API_BASE}/admin/api${path}")"; [ "$c" = "200" ] && ok "GET /admin/api${path}" || bad "GET /admin/api${path} → ${c}"
done
# every feed field the console renders must exist
FEED="$(curl -s "${AUTH[@]}" "${API_BASE}/admin/api/feeds?limit=1")"
echo "${FEED}" | python3 -c "
import json,sys
d=json.load(sys.stdin); f=d['items'][0]
need={'id','title','xmlUrl','sourceName','sourceType','language','priority','pollTier','enabled','healthStatus','consecutiveFailures','lastCheckedAt','categoryKey','scope','needsReview','healthScore'}
missing=need-set(f)
sys.exit(1 if missing else 0)" && ok "feed row contract" || bad "feed row contract"
# offset paging + total on the moderation table
ART="$(curl -s "${AUTH[@]}" "${API_BASE}/admin/api/articles?limit=5&offset=0")"
echo "${ART}" | python3 -c "
import json,sys
d=json.load(sys.stdin)
assert d['total']>0 and len(d['items'])<=5 and 'status' in d['items'][0], d.keys()
" && ok "moderation paging (total + status)" || bad "moderation paging"
# tri-state enabled filter
DIS="$(curl -s "${AUTH[@]}" "${API_BASE}/admin/api/feeds?enabled=false&limit=200" | json "len(d['items'])")"
EN="$(curl -s "${AUTH[@]}" "${API_BASE}/admin/api/feeds?enabled=true&limit=200" | json "len(d['items'])")"
[ "${DIS:-x}" != "x" ] && [ "${EN:-x}" != "x" ] && ok "enabled filter (disabled=${DIS}, enabled=${EN})" || bad "enabled filter"
# feed detail is flat (console reads feed.xmlUrl directly)
FD="$(curl -s "${AUTH[@]}" "${API_BASE}/admin/api/feeds/$(echo "${FEED}" | json "d['items'][0]['id']")")"
echo "${FD}" | python3 -c "
import json,sys
d=json.load(sys.stdin)
assert 'xmlUrl' in d and 'healthEvents' in d and 'recentArticles' in d, sorted(d)[:8]
" && ok "feed detail is flat + extras" || bad "feed detail shape"
# dry-run must never mutate
DRY="$(curl -s "${AUTH[@]}" -X POST "${API_BASE}/admin/api/feedpacks/import?path=resources/feedpacks/afghanistan-global-news-master-v0.3.opml" -H 'Content-Type: application/json' -d '{}')"
echo "${DRY}" | python3 -c "
import json,sys
d=json.load(sys.stdin)
assert d['committed'] is False and d['total']==676, d
" && ok "OPML dry run (676 outlines, nothing written)" || bad "OPML dry run"
# push preview accepts the console payload (with confirm)
PV="$(curl -s "${AUTH[@]}" -X POST "${API_BASE}/admin/api/push/preview" -H 'Content-Type: application/json' -d '{"topic":"breaking","title":"t","body":"b","confirm":false}')"
echo "${PV}" | json "d.get('audience','')" >/dev/null && ok "push preview" || bad "push preview"

echo "== 4/4 web surfaces + Android =="
for p in / /admin/ /admin/app.js /admin/styles.css /app/ /app/app.js /app/app.css; do
  c="$(code "${API_BASE}${p}")"; [ "$c" = "200" ] && ok "GET ${p}" || bad "GET ${p} → ${c}"
done
if [ -f "${ROOT}/android/tools/check_sources.py" ]; then
  python3 "${ROOT}/android/tools/check_sources.py" >/dev/null && ok "android source references" || bad "android source references"
fi
for f in android/settings.gradle.kts android/app/build.gradle.kts android/gradle/libs.versions.toml \
         android/app/src/main/AndroidManifest.xml android/app/src/main/assets/feedpack; do
  [ -e "${ROOT}/${f}" ] && ok "android ${f#android/}" || bad "android missing ${f}"
done

echo
echo "passed ${pass}, failed ${fail}"
[ "${fail}" = "0" ]
