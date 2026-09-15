#!/usr/bin/env bash
# Registers a demo device token and sends one breaking push through the configured sender.
# With the default configuration the sender is a dry-run recorder, so nothing is delivered.
set -euo pipefail
API_BASE="${API_BASE:-http://localhost:8080}"

curl -fsS -X POST "${API_BASE}/v1/push/register" -H 'Content-Type: application/json' \
  -d '{"token":"demo-device-token-0001","platform":"ANDROID","language":"fa","topics":["breaking","afghanistan"]}' | python3 -m json.tool

TOKEN="$(curl -fsS -X POST "${API_BASE}/admin/api/login" -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"afnews-admin"}' | python3 -c 'import json,sys;print(json.load(sys.stdin)["token"])')"

ARTICLE="$(curl -fsS "${API_BASE}/v1/articles?limit=1" | python3 -c 'import json,sys;items=json.load(sys.stdin)["items"];print(items[0]["id"] if items else "")')"

curl -fsS -X POST "${API_BASE}/admin/api/push/send" -H "Authorization: Bearer ${TOKEN}" -H 'Content-Type: application/json' \
  -d "{\"articleId\":\"${ARTICLE}\",\"topic\":\"breaking\",\"title\":\"آزمایش اعلان\",\"body\":\"این یک اعلان آزمایشی است.\",\"confirm\":true}" | python3 -m json.tool
