# Admin / Editorial API (internal)

Protected surface behind `/admin/api`. Session auth: HMAC-SHA256 signed token, 8 h TTL, sent
either as the `afnews_session` cookie or as `Authorization: Bearer <token>`. Roles are enforced
server-side against the permission matrix below; every mutation is written to `admin_audit_log`.

| Role | view | feed_edit | feed_disable | feed_test | import_dryrun | import_commit | breaking | push_send | users | audit |
|---|---|---|---|---|---|---|---|---|---|---|
| SUPER_ADMIN | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| EDITOR | ✓ |  |  | ✓ |  |  | ✓ | ✓ |  | ✓ |
| SOURCE_MANAGER | ✓ | ✓ | ✓ | ✓ | ✓ |  |  |  |  | ✓ |
| VIEWER | ✓ |  |  |  |  |  |  |  |  | ✓ |

## Endpoints

| Method | Path | Notes |
|---|---|---|
| POST | `/admin/api/login` | body `{username, password}`, sets cookie, returns `{token, role, expiresAt}` |
| POST | `/admin/api/logout` | clears the cookie |
| GET | `/admin/api/me` | current operator + expiry |
| GET | `/admin/api/dashboard` | stats, metrics, top failing domains, recent imports/pushes |
| GET | `/admin/api/feeds` | filters: `q, health, sourceType, enabled, limit, offset` |
| POST | `/admin/api/feeds` | register one feed manually |
| GET | `/admin/api/feeds/{id}` | feed detail incl. etag/lease/health |
| PATCH | `/admin/api/feeds/{id}` | `{priority, pollTier, categoryKey, language, enabled}` — audited |
| GET | `/admin/api/feeds/{id}/history` | recent health events |
| POST | `/admin/api/feeds/{id}/test` | single-feed diagnostics (fetch + parse, no writes) |
| POST | `/admin/api/feedpacks/import` | `?commit=true&path=<server path>`; dry-run is the default |
| GET | `/admin/api/feedpacks` | import history |
| GET | `/admin/api/articles` | moderation list (status, breaking, category, source filters) |
| GET | `/admin/api/articles/{id}` | article detail |
| PATCH | `/admin/api/articles/{id}` | `{isBreaking, status, categoryId, provinceId}` — audited |
| POST | `/admin/api/push/preview` | audience size + throttle headroom |
| POST | `/admin/api/push/send` | requires `confirm: true`; duplicates return `409` with the existing event |
| GET | `/admin/api/push/events` | recent push events with status |
| GET | `/admin/api/audit` | audit log (`limit`, `offset`, `entity`) |
| GET | `/admin/api/reference` | categories + provinces (for dropdowns) |
| GET | `/admin/api/activation` | the four rollout waves and how many feeds each would enable |
| POST | `/admin/api/activation` | `{wave: 1..4}` — enables/disables polling only |

## Import semantics (never destructive)

* Dry-run computes a diff and writes nothing.
* Commit inserts new feeds, updates import-owned metadata only (title, category key, type,
  priority, language, scope) and **never** touches `etag`, `last_modified`, `last_success_at`,
  `consecutive_failures`, `health_status`, `lease_*`.
* Feeds absent from the new pack become `needs_review = true`; they are never deleted.
* Outlines with a non-http(s) `xmlUrl` are recorded as invalid and skipped individually —
  one bad outline never fails the document.
