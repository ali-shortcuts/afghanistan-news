# Architecture overview

## Shape

A **modular monolith** with two runnable entry points built from one module:

```
backend/
├── cmd/api       HTTP surface (public /v1, protected /admin/api, static /admin/ and /app/)
├── cmd/worker    standalone ingestion worker (the api can embed it with WORKER_ENABLED=true)
└── internal/
    ├── config        environment configuration + production guards
    ├── model         domain types, DTOs, enums, reference data (34 provinces, 30 categories)
    ├── db            dialect-aware database layer + embedded migrations
    ├── store         all SQL lives here (handlers contain none)
    ├── feed/opml     OPML 2.0 parsing, validation and identity derivation
    ├── feed/fetcher  SSRF-guarded conditional HTTP fetching
    ├── feed/parser   tolerant RSS 2.0 / RSS 1.0 (RDF) / Atom parsing
    ├── feed/normalize canonical URLs, title fingerprints, dates, language detection, sanitizer
    ├── feed/classify category + province classification and opportunity extraction
    ├── ingest        the pipeline: claim → fetch → parse → dedup → classify → store → schedule
    ├── push          FCM sender (HTTP v1) behind a sender interface + dry-run recorder
    ├── observability structured logs, request IDs, Prometheus metrics
    └── httpapi       routing, middleware, public handlers, admin handlers
```

Separating `api` and `worker` is deliberate: HTTP traffic scales on latency, ingestion scales on
politeness. Both share one schema and one scheduler contract (leases), so the embedded mode is
only a convenience for development.

## Data flow

1. **Registry** — the bundled OPML pack is bootstrap; the database is production authority.
   Imports reconcile (insert / update import-owned metadata / flag absent feeds) and never
   delete or overwrite runtime fields.
2. **Scheduling** — the worker claims due feeds with a short lease, so two processes never fetch
   the same feed. A feed's tier decides its interval; failures multiply the interval (cap 6 h).
3. **Fetching** — conditional GET with ETag/Last-Modified, SSRF guard on host, redirect and dial,
   per-domain concurrency and rate limits, response size cap, gzip/brotli, a circuit breaker and
   quarantine after repeated failures.
4. **Parsing & normalising** — tolerant XML parsing into one candidate shape; canonical URLs,
   fingerprints, sanitised HTML, language detection, conservative dates (future dates are
   rejected for publication time but allowed for application deadlines).
5. **Deduplication → classification → storage** — five dedup levels, then category/province
   assignment with confidence and provenance, then insert with `content_hash` and clustering.
6. **Breaking & push** — a scored evaluator (source priority, safety categories, recency, multi-
   source corroboration) flags breaking stories; pushes are deduplicated and throttled per
   topic, and the sender is swappable (dry-run by default).

## Contracts

* `contracts/openapi.yaml` — the public contract the Android client compiles against.
* `contracts/admin-api.md` — the protected surface, roles and permission matrix.
* Cursor pagination is opaque (`base64url(unixnano|id)`), so clients must not parse it.
* Ranking is deterministic: `latest` = `COALESCE(published_at, discovered_at) DESC, id DESC`;
  `top` = age buckets + trust weight + breaking bonus − cluster penalty, with a source-diversity
  cap of 2 for the Top and Home surfaces only.

## Compatibility

The backend targets Go 1.23+ and PostgreSQL 17 (SQLite is supported as a laptop/demo dialect —
the same migrations exist for both, and the store layer rebinds `$n` placeholders per dialect).

The Android client is held to a hard floor of **API 21 (Android 5.0)** with `armeabi-v7a` and
`arm64-v8a` ABIs and no native code; every version-sensitive library is pinned with a comment
explaining the ceiling. See `docs/android/compatibility.md`.
