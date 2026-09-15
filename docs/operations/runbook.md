# Operations runbook

## Processes

| Process | Command | Notes |
|---|---|---|
| API | `go run ./cmd/api` | `WORKER_ENABLED=true` embeds the worker (dev only) |
| Worker | `go run ./cmd/worker` | one or more replicas; feeds are claimed with leases |
| Database | PostgreSQL 17 | `DB_DRIVER=postgres`; SQLite exists for laptop demos only |

Health and metrics: `/health/live` (process), `/health/ready` (database + migrations), `/metrics`.

## Rollout waves

| Wave | Contents | When |
|---|---|---|
| 1 — CORE | validated direct + direct + official feeds (the 99-feed set) | default for production |
| 2 — DISCOVERY TOPICS | + aggregator topic feeds | after feed health is stable for a week |
| 3 — SELECTED SEARCH | + high-priority aggregator searches | after dedup ratios are understood |
| 4 — LONG TAIL | the full 570-feed catalog | only with adaptive polling enabled |

Activate with `POST /admin/api/activation {"wave": n}` or the console's *Rollout waves* page.
Activation changes only the polling flag: history, health and articles are untouched.

## Feed-pack imports

```bash
./scripts/import_feedpack.sh resources/feedpacks/afghanistan-global-news-master-v0.2.opml   # dry run
COMMIT=1 ./scripts/import_feedpack.sh resources/feedpacks/...                                # commit
```

Dry-run is always the default. Commit inserts new feeds, updates import-owned metadata and
flags absent feeds as `needs_review`. Feeds are never deleted and runtime fields (`etag`,
`last_modified`, `last_success_at`, failures, health, leases) are never overwritten.

## Diagnosing a sick feed

1. Console → *Feed registry* → filter by health state, or `GET /admin/api/feeds?health=QUARANTINED`.
2. `POST /admin/api/feeds/{id}/test` runs fetch + parse without writing articles and returns the
   diagnostics (HTTP status, duration, discovered items, parse warnings, first URL).
3. `GET /admin/api/feeds/{id}/history` shows the health timeline.
4. Common causes: `HTTP_403` (publisher blocks datacenter ranges — leave disabled, do not retry
   harder), `PARSER_ERROR` (the URL is not a feed), `TIMEOUT` (slow origin — the tier's backoff
   already raised), `QUARANTINED` (repeated SSRF/DNS blocks).

## Push operations

`PUSH_DRY_RUN=true` (default) records events with status `DRY_RUN` and delivers nothing — safe
for staging. To deliver, set `PUSH_DRY_RUN=false`, provide `FCM_PROJECT_ID` and
`FCM_CREDENTIALS_FILE`. Sends require `confirm: true`, duplicates return `409` with the existing
event, and per-topic throttles (6/hour, 40/day by default) suppress bursts.

## Backups and retention

* `pg_dump` nightly; the schema is small (article text is trimmed at ingest).
* Articles, health events and fetch runs are prunable; `feed_pack_imports` and `admin_audit_log`
  are the compliance trail and should be kept long-term.

## Metrics that matter

| Metric | Meaning | Alert |
|---|---|---|
| `feed_fetch_failure_total` rising | origin or DNS problems | > 10% of fetches over 15 min |
| `articles_duplicate_total` vs `articles_inserted_total` | discovery feeds duplicating direct feeds | dup ratio > 80% on a wave |
| `feeds_by_health_total{status="QUARANTINED"}` | SSRF/DNS blocks | any sustained increase |
| `push_events_total{status="FAILED"}` | FCM problems | any in an hour |
| `/health/ready` failing | database/migrations | immediate |

## Incident checklist

1. `/health/ready` → database reachable? migrations applied?
2. `/metrics` → fetch failures, run duration, push failures.
3. Console → *Dashboard* → top failing domains, recent imports, recent pushes.
4. If an import went wrong: dry-run the previous pack, commit it back (it reconciles), and check
   `admin_audit_log` for the `FEEDPACK_COMMITTED` entry.
5. Never hand-edit the database to "fix" a feed: use `PATCH /admin/api/feeds/{id}` so the change
   is audited.
