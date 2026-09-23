# Production deployment (Phase B)

Phase B replaces the SQLite demo with the shape the architecture document describes: PostgreSQL,
the public API and the ingestion worker as **separate processes**, one migration path off SQLite,
and a backup/restore procedure that has been rehearsed rather than described.

Everything below was executed against a real PostgreSQL 17.11 server while writing it; the
commands and their output are reproduced as they ran.

## Process model

```
                    ┌──────────────────────┐
   Android client ──┤  api   (cmd/api)     │  read-only on the public path
   Admin console  ──┤  :8080, WORKER_=false│  serves /v1, /admin, /app, /metrics
   Landing page   ──┘                      │
                    └──────────┬───────────┘
                               │            shared schema, one writer per table
                    ┌──────────▼───────────┐
                    │  PostgreSQL 17       │
                    └──────────▲───────────┘
                               │
                    ┌──────────┴───────────┐
                    │  worker (cmd/worker) │  feed polling, parsing, dedup, clustering,
                    │  no listening socket │  breaking detection, push fan-out
                    └──────────────────────┘
```

Why two processes and not one (§66, §113): a slow feed — a 30-second timeout on a publisher that
is throttling us — must not consume a request goroutine. The API process starts with
`WORKER_ENABLED=false` in every production-shaped run, which is asserted by
`scripts/run_production_stack.sh start` and by `docker-compose.yml`.

| Process | Binary | Listens | Responsibility |
|---|---|---|---|
| api | `cmd/api` | `:8080` | `/v1/*`, `/admin/*`, static bundles, `/health/*`, `/metrics` |
| worker | `cmd/worker` | nothing | ingestion pipeline, push generation and delivery |
| migrate-data | `cmd/migrate-data` | nothing | one-off SQLite → PostgreSQL copy |

## Running it

### Without Docker (development, CI, this workspace)

```bash
scripts/preview.sh all                      # cold machine → running stack, one command
# or, step by step:
sudo scripts/bootstrap_postgres.sh          # install/start PG, create role + database
scripts/migrate_to_postgres.sh data/afnews.db   # optional: bring existing content across
scripts/run_production_stack.sh start       # api + worker, both detached
scripts/run_production_stack.sh status
scripts/run_production_stack.sh logs 40 worker
scripts/run_production_stack.sh stop
```

`make stack`, `make stack-status`, `make stack-logs WHICH=worker`, `make stack-stop` wrap the same
script.

### With Docker Compose

```bash
cp .env.example .env      # set ADMIN_SESSION_KEY (32+ bytes); FCM_* only if pushing for real
docker compose up -d --build
curl -fsS localhost:8080/health/ready
docker compose --profile tools run --rm backup       # dump into ./backups
```

The worker reuses the same image with `entrypoint: ["/app/worker"]`. The worker container has no
published port and no HTTP health check: it is checked with `pgrep`. `ALLOW_PRIVATE_FETCH` stays
`false` in every environment; it exists only so tests can point the fetcher at a local stub.

## Environment variables that matter in production

| Variable | Default | Why |
|---|---|---|
| `DB_DRIVER` | `sqlite` | must be `postgres` |
| `DATABASE_URL` | — | `postgres://user:pass@host:5432/db?sslmode=disable` inside the compose network; use `require`/`verify-full` for a managed database |
| `WORKER_ENABLED` | `true` | **set `false` on the API process** |
| `HTTP_ADDR` | `0.0.0.0:8080` | note: not `PORT` |
| `ADMIN_SESSION_KEY` | dev value | refuse to boot in `ENV=production` without a real one |
| `FEED_PACK_PATH` | `resources/feedpacks/…` | relative to the process's working directory — set it absolutely when the process does not start from `backend/` |
| `FEED_ACTIVATION_WAVE` | `1` | rollout wave; see the feed-pack document |
| `PUSH_DRY_RUN` | `true` | leave `true` until real FCM credentials are mounted |
| `RATE_LIMIT_PER_MINUTE` | `240` | raise for a local acceptance run (`600`), keep tight in production |
| `MAX_ITEMS_PER_FEED_RUN` | `60` | per-feed ceiling per poll, bounds a hostile feed |
| `QUARANTINE_AFTER` | `12` | consecutive failures before a feed is quarantined |
| `BREAKING_MAX_PER_RUN` | `2` | per-feed cap on items that may be flagged breaking (§ anti-abuse) |

## Seeding a fresh environment (`cmd/seed-fixtures`)

An empty database is a legitimate deployment — but it makes three acceptance checks
(`article detail`, `feed row contract`, `moderation paging`) fail for a reason that has
nothing to do with the code, which is what a fresh CI runner used to look like. The
fixture seeder closes that gap: **offline, deterministic and idempotent** — no network,
safe to re-run, works in CI.

```bash
# against PostgreSQL
cd backend && go run ./cmd/seed-fixtures -driver postgres \
  -pack resources/feedpacks/afghanistan-global-news-master-v0.3.opml

# the whole "does this build work on an empty database?" question, one command
bash scripts/seed-and-verify.sh
```

What it loads, in order:

1. **reference data** — categories, provinces, the admin/editorial user;
2. **the bundled OPML pack** — 676 outlines reconciled into the feed registry;
3. **a rollout wave** — `-wave 1` (core) by default, `1..4` accepted;
4. **articles** — `backend/testdata/feeds/*.xml` decoded by the *real* parser and inserted
   through the *real* dedup path (these files deliberately repeat a story, so the dedup
   branch is exercised on every run), plus `-synth N` generated distinct stories spread
   over different feeds so the home feed, article pages and moderation queue have content;
5. **a push device** — `fixture-device-token`, so the push contract has something to answer.

Flags: `-driver sqlite|postgres`, `-dsn`, `-sqlite`, `-pack`, `-fixtures`, `-wave`, `-articles`, `-synth`.

> The seeder is not a production bootstrap: real content arrives from the ingestion worker.
> Use it for CI, demos and local rehearsal. Both `make seed` and `make seed-test` wrap it.

## Data migration: SQLite → PostgreSQL

```bash
DATABASE_URL=postgres://afnews:…@127.0.0.1:5432/afnews scripts/migrate_to_postgres.sh data/afnews.db
```

Four steps, each observable:

1. **Preflight** — source file exists, target reachable, migrations applied.
2. **Dry run** — per-table row counts, nothing written.
3. **Copy** — one transaction per table, in foreign-key order, type-aware:
   SQLite `0/1` becomes PostgreSQL `boolean`, text timestamps become `timestamptz`, empty strings
   become `NULL`, blobs become text.
4. **Verify** — row counts compared table by table, then an **API parity check**: both dialects are
   started side by side and nine endpoints are compared for identical payloads.

Real output from this repository:

```
  sources 43 · categories 30 · provinces 34 · article_clusters 2065 · feeds 106 · articles 2080
  article_categories 5956 · article_provinces 196 · article_opportunities 16
  feed_health_events 245 · feed_fetch_runs 245 · feed_pack_imports 4 · push_registrations 1
  push_events 1 · admin_users 1 · admin_audit_log 11
  TOTAL 11034 rows
sequences resynced: 4
verified: every table has the same row count in source and target

  ✓ /v1/home: identical payloads (87 identifiers)
  ✓ /v1/articles?limit=5: identical payloads (15 identifiers)
  ✓ /v1/sources: identical payloads (44 identifiers)
  ✓ /v1/provinces: identical payloads (34 identifiers)
  ✓ /v1/categories: identical payloads (30 identifiers)
  ✓ /v1/config: identical payloads
  ✓ /v1/search?q=afghanistan: identical payloads (94 identifiers)
  ✓ /v1/articles?limit=3&category=politics: identical payloads (9 identifiers)
✓ PostgreSQL serves the same public API as SQLite
```

### The sequence trap (found here, worth remembering)

The copy inserts **explicit** primary keys. In PostgreSQL, inserting an explicit value into a
`BIGSERIAL` column does **not** advance that column's sequence, so the first ordinary insert after
a migration — in practice the first feed-pack import — dies with:

```
ERROR: duplicate key value violates unique constraint "feed_pack_imports_pkey" (SQLSTATE 23505)
```

The same thing happens after a `pg_restore`, and after any bulk load. Two layers now handle it:

1. **`cmd/migrate-data`** resyncs every sequence at the end of a copy (`sequences resynced: 4`).
2. **Both processes verify sequences on boot** — `db.SyncSequences` runs right after migrations and
   logs `sequences verified serial_columns=4`. It sets each sequence to `GREATEST(MAX(id), 1)` and
   marks it called only when the table actually has rows. Restores and hand-loaded data therefore
   self-heal, and the invariant is re-established on every start rather than trusted.

Covered by two PostgreSQL-only regression tests in `internal/db/sequences_test.go`: one drifts a
sequence deliberately (four rows, sequence at 3), asserts the collision really happens, then
asserts the repair and that the next id is 5; the other pins the empty-table edge, where an
incorrectly-marked-called sequence would hand out 2 for the first row — which is how the `is_called`
bug in the first version of the helper was caught.

## Backup and restore

```bash
scripts/backup.sh                       # custom-format dump + SQL dump + manifest, verified
BACKUP_DIR=/srv/backups RETENTION_DAYS=30 scripts/backup.sh
scripts/restore.sh backups/afnews-…dump --into afnews_restore --create   # rehearsal
```

`backup.sh` writes three files per run and is safe to put in cron:

| File | Purpose |
|---|---|
| `afnews-<stamp>.dump` | `pg_dump -Fc` — the real backup, restorable selectively |
| `afnews-<stamp>.sql` | plain SQL — greppable during an incident |
| `afnews-<stamp>.manifest` | sha256, server version, row counts — the audit trail |

It then **lists the dump back** with `pg_restore --list`. A dump nobody has ever read back is not a
backup. Verified output: `21 tables of data`, 1.3 MB for 2 080 articles.

`restore.sh` restores into a scratch database by default (`--into`) with `--single-transaction
--clean --if-exists`, so a rehearsal cannot half-wreck a live database, and prints post-restore row
counts. Drill result: `articles 2080 · feeds 106 · sources 43 · push_events 1`.

Retention: dumps older than `RETENTION_DAYS` (default 14) are pruned by the script itself.

Recommended schedule:

```cron
15 3 * * *  cd /srv/afnews && scripts/backup.sh >> /var/log/afnews-backup.log 2>&1
30 4 * * 0  cd /srv/afnews && scripts/restore.sh "$(ls -t backups/*.dump | head -1)" --into drill --create
```

## Monitoring

* `GET /health/live` — process is up (used by orchestrators for restarts).
* `GET /health/ready` — database reachable; `{"database":"ok","status":"ready"}`.
* `GET /metrics` — Prometheus text format: HTTP latency by route, feed fetch outcomes
  (HEALTHY/STALE/DEGRADED/UNSTABLE/DISABLED/HTTP_ERROR/PARSER_ERROR/EMPTY/UNKNOWN), items
  ingested/duplicate/skipped, push events, dedup and cluster counters.
* `scripts/pg_status.sh` — version, schema version, content counts, articles per day, feed-health
  histogram, and `seq_scan`/`idx_scan` on the hot tables.

Alert on: `health_ready == 0` for 2 minutes; no `items_ingested` for 30 minutes while
`feeds_polled` continues; `feed_health{state="HTTP_ERROR"}` growth; `BREAKING_MAX_PER_RUN` reached
every run (signals a threshold that is too loose).

## Capacity notes measured here

* API process: ~13 MB RSS idle, ~19 MB under the acceptance suite. The worker floats around
  20–25 MB with 8 concurrent fetches.
* The whole stack — PostgreSQL + API + worker — runs comfortably in a 2 GB container: the Gradle
  build is the memory-hungry part of this repository, not the backend.
* Ingestion rate observed on this box: ~200–400 items/minute with 8 workers, dominated by
  publisher response time, not by CPU. The SQLite dialect sustains the same rate, so a laptop demo
  remains honest.

## Restoring onto a fresh host (checklist)

1. `sudo scripts/bootstrap_postgres.sh` — role and database exist.
2. `scripts/restore.sh backups/<newest>.dump --into afnews --create` — restores and prints counts.
3. `scripts/start_worker.sh` and the API (`scripts/run_production_stack.sh start`) — both verify
   sequences on boot, so a dump that was written while a sequence lagged cannot break the first
   admin action after the restore.
4. `scripts/acceptance.sh` — 48 checks; the OPML dry-run check is what caught the sequence drift in
   this environment.
