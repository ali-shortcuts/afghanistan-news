# Afghanistan News Platform

A full-stack, offline-first news platform for Afghanistan: a multi-language reader app, a public
news API, an editorial console, and an ingestion pipeline that turns an OPML 2.0 feed pack of
570 publisher feeds into a deduplicated, attributed, province-aware news stream.

```
news-platform/
├── backend/     Go modular monolith — cmd/api (HTTP) + cmd/worker (ingestion), PostgreSQL 17
├── admin/       Editorial console (protected, zero-dependency, served at /admin/)
├── app/         Mobile client preview (offline-first, RTL Persian, same /v1 contract)
├── android/     Kotlin · Compose · Room reader app (minSdk 21, arm32 + arm64, API-21 pinned)
├── feedpacks/   Canonical OPML 2.0 feed pack v0.2 — 570 feeds, 34 folders
├── contracts/   OpenAPI 3.0 public contract + admin API spec
├── docs/        Architecture, deployment, operations runbook, security posture, Android notes
└── scripts/     bootstrap · import_feedpack · seed_demo_push · smoke · acceptance
                 + production stack, PostgreSQL bootstrap, migration, backup, restore
```

## The four surfaces

| # | Surface | Where | State |
|---|---|---|---|
| 1 | **Android reader** — Kotlin 2.2, Compose, Room, Hilt, Retrofit, Paging 3, WorkManager, FCM | `android/` | **builds here**: debug APK for arm32 + arm64, `minSdk 21` / `targetSdk 36`, unit tests + lint green |
| 2 | **Public news API** — `/v1/*`, cursor pagination, attribution, search, clusters, push registration | `backend/` | **live now** |
| 3 | **Editorial console** — feed registry, OPML import, moderation, breaking & push, audit, rollout waves | `admin/` | **live now** |
| 4 | **Ingestion worker** — scheduler → SSRF-guarded fetch → parse → dedup → classify → cluster → push | `backend/cmd/worker` | **live now**, polling 105 feeds |

## Cold start (one command)

A fresh machine has no PostgreSQL, no Go toolchain and no running processes — and file mode bits
do not survive a workspace snapshot, so the scripts come back non-executable. `scripts/preview.sh`
handles all of it and is safe to re-run at any time:

```bash
scripts/preview.sh all       # install PostgreSQL if needed → restore content → start api + worker
scripts/preview.sh status    # process state, health, article count, and every surface's HTTP code
scripts/preview.sh restart   # after a rebuild
scripts/preview.sh stop
```

`prepare` and `start` are separate, so a supervisor can own the API process (that is how the live
preview here runs) while the script still reports it correctly. Content seeding prefers the newest
`backups/*.dump`, falls back to `data/afnews.db` via `cmd/migrate-data`, and never touches a
database that already holds data.

## Installing the Android app

[**Download the APK**](https://github.com/ali-shortcuts/afghanistan-news/releases/latest) from
Releases, copy it to the phone, tap it, and allow installation from unknown sources when asked.

| | |
|---|---|
| Package | `com.afghanistan.news` |
| Version | 1.0.0 (versionCode 1) |
| Size | ~12 MB, one APK for both ABIs |
| Architectures | `arm64-v8a` (64-bit) **and** `armeabi-v7a` (32-bit) |
| Android | 5.0 (API 21) through 16 (API 36) |
| Signature | v1 + v2 schemes, so it installs on old devices *and* passes modern verification |
| Permissions | INTERNET, ACCESS_NETWORK_STATE, POST_NOTIFICATIONS, RECEIVE_BOOT_COMPLETED |

**First run: enter your server address.** The app ships with a placeholder API address
(`https://api.afghanistan.news/`), because the address of the news API is a deployment decision,
not something that can be baked into a public build. Open **بیشتر → تنظیمات → سرور خبر**, type the
address of your running backend, tap **تست اتصال** (test connection) and then **ذخیره و بارگیری
دوباره** (save and reload). Accepted forms:

```
news.example.com              → https://news.example.com/
https://news.example.com/api  → https://news.example.com/api/
10.0.2.2:8080                 → http://10.0.2.2:8080/     (Android emulator → host machine)
192.168.1.5:8080              → http://192.168.1.5:8080/ (phone on the same Wi-Fi as your server)
```

Addresses without a scheme are treated as `https`, except loopback/private/LAN names, which are
treated as `http` — those cannot have a certificate the phone can trust. Cleartext HTTP is
permitted for exactly that reason (`res/xml/network_security_config.xml` explains the trade-off).
Changing the address also evicts the HTTP cache, so articles from one backend are never shown as
if they came from another.

### Signing

Release builds are signed with `android/afnews-release.jks`, whose credentials live in
`android/keystore.properties`. **Both are gitignored — back them up.** Without that keystore you
cannot ship an update to an app that was installed from the signed APK; Android refuses to replace
it. To build on another machine, set `ANDROID_KEYSTORE_FILE`, `ANDROID_KEYSTORE_PASSWORD`,
`ANDROID_KEY_ALIAS` and `ANDROID_KEY_PASSWORD` instead of the properties file. With no keystore at
all the build still works and falls back to the debug key, printing which one it used.

### Building the APK yourself

```bash
cd android
./gradlew :app:assembleDebug                 # debug APK, both ABIs
./gradlew :app:assembleRelease               # signed release APK (runs R8)
./gradlew :app:assembleRelease -PminifyRelease=false   # signed, unshrunk (low-memory machine)
```

R8 needs roughly 1.5 GB of heap. The 2 GB container used to develop this project cannot run it, so
the APK attached to the current release is **signed but unshrunk**; `.github/workflows/release-apk.yml`
builds the minified APK on a GitHub runner (7 GB) whenever a `v*` tag is pushed.

## Running right now (this workspace)

| Surface | URL |
|---|---|
| Landing / service index | `/` |
| Public API | `/v1/*` — `home`, `articles`, `categories`, `provinces`, `sources`, `search`, `config`, `feed-pack/version`, `notifications` |
| Mobile client preview | `/app/` |
| Editorial console | `/admin/` (seed login `admin` / `afnews-admin`, development only) |
| Metrics · Health | `/metrics` · `/health/live`, `/health/ready` |

## Verified end-to-end

`./scripts/acceptance.sh` — **48/48 checks passing**:

* public API: 11 endpoints + home shape + detail + 404 contract + search
* push: device registration and revocation
* admin API: login, `/me`, dashboard, feeds, articles, imports, pushes, audit, reference, waves
* console contracts: every feed field the UI renders, offset paging **with `total`** and moderation
  `status`, tri-state `enabled` filter, flat feed detail with `healthEvents` + `recentArticles`,
  OPML dry-run that writes nothing, push preview payload
* observability: per-health feed gauges and article/feed totals exposed on `/metrics`
* web surfaces: `/`, `/admin/*`, `/app/*` all served
* Android: static reference checks (`android/tools/check_sources.py`) + module layout

Backend unit/integration tests: `cd backend && go test ./...` → **69 test cases, all packages
green** — and the same suite runs against PostgreSQL with `TEST_DB_DRIVER=postgres` (each test
gets its own schema). Dialect parity is asserted, not assumed.

Android: `cd android && ./gradlew :app:assembleDebug :app:testDebugUnitTest :app:lintDebug` →
APK produced, 6 unit tests green, `lintDebug` fails the build on any `NewApi` violation. Two
artifact-level gates run before the build: `tools/check_deps_api21.py` (every dependency's
declared `minSdk`) and `tools/check_sources.py` (every internal reference).

Ingestion is live on PostgreSQL: 106 feeds in the registry (105 enabled by Wave 1), 2 400+
articles stored, health events per feed, pushes recorded by the dry-run sender.

## Phase status

| Phase | Scope | State | Evidence |
|---|---|---|---|
| **A** | Version pins to the newest API-21-compatible releases, real Gradle wrapper, compileSdk/targetSdk 36, closing every documented deviation | **done** | debug APK builds for `arm64-v8a` + `armeabi-v7a`; `minSdkVersion:'21'`, `targetSdkVersion:'36'`; 6 unit tests; lint clean; `docs/android/compatibility.md` |
| **B** | PostgreSQL instead of SQLite, API and worker as separate processes, Docker Compose, data migration, backup/restore | **done** | 11 034 rows migrated and verified; API parity across 8 endpoints; suite green on both dialects; backup + restore drill; `docs/deployment.md` |
| **C** | Real FCM, story/cluster pages, AFN rates + fuel from a dedicated source, full offline download, advanced search and filters | next | — |

### Phase B in one screen

```bash
sudo scripts/bootstrap_postgres.sh                 # install/start PG, create role + database
scripts/migrate_to_postgres.sh data/afnews.db      # dry run → copy → verify → API parity
scripts/run_production_stack.sh start              # api (no embedded worker) + standalone worker
scripts/backup.sh && scripts/restore.sh "$(ls -t backups/*.dump | head -1)" --into drill --create
docker compose up -d --build                       # the same shape, containerised
```

Two PostgreSQL-only bugs were found and fixed while doing this — both invisible on SQLite, which
is the point of the phase:

* **Sequence drift**: the copy inserts explicit primary keys, and in PostgreSQL that does not
  advance a `BIGSERIAL` sequence, so the next feed-pack import died with
  `duplicate key value violates unique constraint "feed_pack_imports_pkey"`. `cmd/migrate-data`
  resyncs after copying, and `db.SyncSequences` runs on every boot so restores and bulk loads
  self-heal too (`sequences verified serial_columns=4` in the log), covered by two
  PostgreSQL-only regression tests.
* **Uninferable parameter type**: `CASE WHEN $7 IS NULL` gives PostgreSQL nothing to infer, so
  `MarkFeedSuccess` failed with `could not determine data type of parameter $7`. A dialect-aware
  `tsCast` helper now casts it (and stays out of the SQLite path, which has no `::` syntax).

## How ingestion works

```
OPML feed pack ─► registry (sources + feeds) ─► scheduler (leases, poll tiers)
                                                    │
                          SSRF-guarded conditional GET (ETag / If-Modified-Since)
                                                    ▼
                 parser (RSS 2.0 · RSS 1.0/RDF · Atom) ─► normalizer ─► HTML sanitizer
                                                    ▼
   dedup (GUID → canonical URL → content hash → title ≤48 h → story cluster) ─► classifier
                                                    ▼
                         articles ─► breaking evaluator ─► push queue (dedup + throttle)
```

* **Poll tiers** — BREAKING 3 min · HIGH 5 min · NORMAL 12 min · SLOW 30 min · OPPORTUNITY 60 min,
  with exponential backoff and jitter, per-domain concurrency limits and a circuit breaker.
* **Rollout waves** — wave 1 = the 99 direct/validated/official feeds; then topic aggregators,
  then selected searches, then the long tail. Switchable from the console without touching history.
* **Curated digests** — the front page drops non-editorial instrument data (a seismograph log
  keeps its own topical list and source page) and caps any single source at one card per section.
* **Deterministic breaking rule** — a score *and* a qualifying signal (local relevance, primary
  safety-critical topic, independent corroboration, or explicit severity), with a per-feed cap,
  so a high-volume wire can never turn its backlog into breaking news.
* **Attribution** — every article carries its publisher, source type (`VALIDATED_DIRECT`,
  `OFFICIAL_REALTIME`, `AGGREGATOR_SEARCH`, …) and a human-readable transparency label.
* **Never destructive** — OPML imports reconcile: new feeds are inserted, import-owned metadata is
  updated, absent feeds are flagged `needs_review`, and runtime fields (`etag`, `last_success_at`,
  failures, health, leases) are never overwritten.

## Quick start

```bash
cp .env.example .env                      # DATABASE_URL, ADMIN_SESSION_KEY, feed-pack wave
make backend-build backend-test           # compile + full Go test suite
make backend-run                          # API with the embedded worker (dev)
make acceptance                           # 48-check end-to-end verification
make preview                              # print the preview URLs
```

Production shape — separate API, worker and database processes:

```bash
docker compose up --build                 # postgres:17 + api + worker (+ the web bundles)
```

Android (needs JDK 17 + Android SDK 35, i.e. CI or a workstation):

```bash
cd android && ./gradlew :app:assembleDebug :app:testDebugUnitTest :app:lintDebug
```

## Product and safety posture

* **No mandatory login.** v1 has no accounts; preferences and bookmarks live on-device.
* **Honest delivery reporting** — push events are recorded as `DRY_RUN` (not `SENT`) whenever no
  FCM credential is configured.
* **No blind scraping, no republishing.** Declared feeds only, honest user agent, conditional
  GETs, summaries rendered with their source and the original one tap away (Chrome Custom Tabs).
* **SSRF-guarded fetching** — private/loopback/link-local/metadata addresses are refused, with a
  dial-time and redirect-time re-check; repeated blocks quarantine the feed.
* **Currency and fuel rates** would come from a dedicated provider only — never parsed from headlines.

## Documentation

| File | Contents |
|---|---|
| `docs/architecture/overview.md` | module map, data flow, ranking and cursor contracts |
| `docs/operations/runbook.md` | processes, rollout waves, imports, incident checklist, metrics |
| `docs/security/ssrf-and-content.md` | SSRF layers, content sanitisation, admin auth, data protection |
| `docs/product/reading-contract.md` | reader surfaces, non-negotiables, editorial workflow |
| `docs/android/compatibility.md` | the API-21 / ARM32+ARM64 gate and every dependency ceiling |
| `docs/verification.md` | how each surface is verified, and what this workspace cannot run |
| `contracts/openapi.yaml` | public API contract the Android client compiles against |
| `contracts/admin-api.md` | admin endpoints, role matrix, import semantics |
