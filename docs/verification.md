# Verification

This document records **how each surface is verified** and — equally important — **what cannot be
verified here**, so nobody mistakes an unchecked box for a passing one.

## What runs here

| Layer | Command | Result |
|---|---|---|
| Backend build + vet | `cd backend && go build ./... && go vet ./...` | clean |
| Backend tests | `cd backend && go test ./...` | 9 packages green (parser · normalize · sanitize · OPML · classify · ingest · store · httpapi · observability), 77 cases |
| Live stack acceptance | `./scripts/acceptance.sh` | **48 / 48 checks pass** |
| Smoke | `./scripts/smoke.sh` | health, home, articles, sources, config |
| Web bundles | `node --check admin/app.js app/app.js` | syntax clean |
| Android sources | `python3 android/tools/check_sources.py` | every internal import, every `R.*` reference and every resource reference resolves (40 files / 25 packages / 142 declarations) |
| Feed pack | `cd backend && go test ./internal/feed/opml/ -run TestRealFeedPack` | 676 feeds, 37 folders, sha256 pinned |
| Compose / CI / contract | parsed as YAML | `docker-compose.yml`, `.github/workflows/ci.yml`, `contracts/openapi.yaml` |

## What `scripts/acceptance.sh` asserts

It is not a status-code smoke test. It asserts the **contract the clients depend on**:

1. **Public API** — 11 endpoints answer 200; `/v1/home` carries the documented sections; article
   detail resolves a real ID; an unknown ID returns the `404` error envelope (not a 200 with
   nulls); search answers a Persian query.
2. **Push** — a device token can be registered and revoked (204); registration is anonymous.
3. **Admin API** — login issues a session; `/me` reports role + `expiresAt` + permissions; the
   dashboard, feed registry, moderation table, import history, push events, audit log, reference
   data and rollout waves all answer 200.
4. **Console field-level contracts** — every key the console renders exists in the response
   (feed rows: `healthStatus`, `healthScore`, `consecutiveFailures`, `pollTier`, `needsReview`, …);
   the moderation table returns `total` **and** `status` for offset paging; `enabled=true|false`
   is tri-state; feed detail is flat and carries `healthEvents` + `recentArticles`; the OPML dry
   run reports 676 outlines with `committed: false` and writes nothing; the push preview accepts
   the exact payload the console sends.
5. **Observability** — `/metrics` exposes the per-health feed gauges as a labelled series and the
   article/feed totals as gauges, so a scraper never sees a stale `0`.
6. **Web surfaces** — `/`, `/admin/*`, `/app/*` are served.
7. **Android layout** — module files exist and the static reference checker passes.

Every item above fixed a real defect found while building: `/me` had no `expiresAt`; feed detail
was wrapped in `{feed, health, …}` while the console read it flat; the moderation table had no
`total`/`status`; `enabled=false` silently returned enabled feeds; the dry run reported
`missingFromPack` while the console read `missing`; the push preview rejected the console's
`confirm` field; `feeds_by_health_total` rendered as a constant `0`.

## Correctness fixes that only appear on live data

These were found by running the pipeline against the real 97-feed Wave-1 set and inspecting what
readers would actually have seen. Each is now covered by a unit test:

| Symptom on live data | Root cause | Fix |
|---|---|---|
| Every magnitude-1.0 seismograph reading was flagged **breaking** and pushed to subscribers | an official realtime feed at priority 5 already scores exactly the 0.60 threshold, so source ranking alone decided it | breaking now needs a *qualifying* signal (local relevance · primary safety-critical topic · corroboration · explicit severity) on top of the score — `decideBreaking`, plus a per-run cap (`BREAKING_MAX_PER_RUN`, default 2) |
| Front page was a tremor log | the seismograph feeds' folder tags (`disasters`, `climate`, `science`) counted as a safety category | only the **primary** (highest-confidence) topic may qualify — `safetyCategoryOf` |
| The same | keyword rules read the article URL, and the USGS feed's URL contains `earthquake` | classification reads title + summary only |
| `topStories` / `world` led with instrument readings | no digest-level topic filter | `Digest` queries drop items whose primary category is non-editorial (`science`, `reference`), and curated sections cap a single source at one card over a 6× candidate window |
| 13 feeds showed as `QUARANTINED` for an ordinary `HTTP 403` | the fetch-error handler treated any 401/403 as a security incident | 401/403 → `DISABLED` with a 12 h backoff; quarantine is reserved for SSRF/DNS blocks |
| Console reported pushes as `SENT` although `PUSH_DRY_RUN=true` and no FCM credential exists | delivery status ignored the sender's dry-run result (both the worker and the admin send path) | status is now `DRY_RUN` unless the sender really contacted FCM |

The 13 hardened feeds were re-labelled with a one-off `UPDATE` so the live registry matches the
corrected rule; nothing else in the database was doctored.

## What this workspace cannot run

| Not run here | Why | Covered instead by |
|---|---|---|
| Android build (`assembleDebug`, `lintDebug`, unit tests) | no Android SDK, JDK 17 or Gradle in the sandbox | `android/tools/check_sources.py` + the CI `android` job, which bootstraps the wrapper, builds both ABIs and runs `lintDebug` as the API-21 gate |
| `docker compose up` | no container runtime | the compose file is the same process shape as the verified live stack: `postgres:17` + `api` + `worker` |
| FCM delivery | no Firebase project/credentials | `push.Sender` is an interface: `DryRunSender` records honest `DRY_RUN` events (verified live), `FCMSender` implements HTTP v1 |
| Publisher traffic at full scale | sandbox politeness and time | the worker polls the real Wave-1 set live; health, backoff, dedup and the breaking rule are all observable in `/admin/` and `/metrics` |
| PostgreSQL-specific SQL paths | the demo runs on the embedded SQLite dialect here (the sandbox has no PostgreSQL) | the CI `backend` job runs the same binary against `postgres:17`, plus `go test ./... -race` |

## Reproducing the live state

```bash
# Go toolchain (if needed): https://go.dev/dl → /usr/local/go
cd backend && go build -o ../bin/afnews-api ./cmd/api && go build -o ../bin/afnews-worker ./cmd/worker

# Demo database (embedded SQLite, zero dependencies) — or point DATABASE_URL at PostgreSQL
DB_DRIVER=sqlite SQLITE_PATH=../data/afnews.db DATABASE_URL=file:../data/afnews.db \
  WORKER_ENABLED=true ../bin/afnews-api

./scripts/acceptance.sh                 # 48 checks
./scripts/smoke.sh                      # quick health pass
```

## Numbers observed at the time of writing

```
feeds              106 in registry, 97 enabled and polled (Wave 1)
sources             43 publisher identities
articles          2 080 collected from the live feed set
feed health         82 HEALTHY · 4 STALE · 3 DEGRADED · 3 UNSTABLE · 9 UNKNOWN · 2 DISABLED
                    · 1 EMPTY · 1 HTTP_ERROR · 1 PARSER_ERROR  (real publisher behaviour)
feed_health_events  90 (real HTTP 403/404/timeouts, backoff working)
push_events          1 (editorial dry run → status DRY_RUN, nothing delivered)
breaking flagged     0  ← honest: nothing in the current window cleared the qualifying bar
```
