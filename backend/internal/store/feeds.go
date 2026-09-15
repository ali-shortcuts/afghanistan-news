package store

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/afnews/backend/internal/model"
)

// feedColumns is the canonical feed projection.
const feedColumns = `f.id, f.source_id, f.xml_url, f.normalized_xml_url, COALESCE(f.html_url,''),
	f.title, COALESCE(f.language,''), COALESCE(f.scope,''), COALESCE(f.category_key,''), f.source_type,
	f.priority, f.poll_tier, f.enabled, f.health_status, COALESCE(f.etag,''), COALESCE(f.last_modified,''),
	f.last_checked_at, f.last_success_at, f.newest_item_at, f.next_poll_at, f.consecutive_failures,
	f.health_score, COALESCE(f.feed_pack_version,''), f.needs_review, COALESCE(f.lease_owner,''),
	f.lease_until, f.created_at, f.updated_at, COALESCE(s.name,'')`

func scanFeed(sc interface{ Scan(...any) error }) (*model.Feed, error) {
	var f model.Feed
	var enabled, needsReview any
	var lastChecked, lastSuccess, newest, nextPoll, leaseUntil, created, updated NullTimeOf
	var srcType, pollTier, health string
	err := sc.Scan(&f.ID, &f.SourceID, &f.XMLURL, &f.NormalizedXMLURL, &f.HTMLURL,
		&f.Title, &f.Language, &f.Scope, &f.CategoryKey, &srcType,
		&f.Priority, &pollTier, &enabled, &health, &f.ETag, &f.LastModified,
		&lastChecked, &lastSuccess, &newest, &nextPoll, &f.ConsecutiveFailures,
		&f.HealthScore, &f.FeedPackVersion, &needsReview, &f.LeaseOwner,
		&leaseUntil, &created, &updated, &f.SourceName)
	if err != nil {
		return nil, err
	}
	f.SourceType = model.SourceType(srcType)
	f.PollTier = model.PollTier(pollTier)
	f.HealthStatus = model.HealthStatus(health)
	f.Enabled = scanBool(enabled)
	f.NeedsReview = scanBool(needsReview)
	f.LastCheckedAt = lastChecked.Ptr()
	f.LastSuccessAt = lastSuccess.Ptr()
	f.NewestItemAt = newest.Ptr()
	f.NextPollAt = nextPoll.Ptr()
	f.LeaseUntil = leaseUntil.Ptr()
	f.CreatedAt = created.Time
	f.UpdatedAt = updated.Time
	return &f, nil
}

// UpsertSource creates or updates a source row.
func (s *Store) UpsertSource(ctx context.Context, src model.Source) error {
	now := time.Now().UTC()
	_, err := s.exec(ctx,
		`INSERT INTO sources (id, name, website_url, source_type, default_language, trust_weight, enabled, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$8)
		 ON CONFLICT (id) DO UPDATE SET name = $2, website_url = COALESCE(NULLIF($3,''), sources.website_url),
		   source_type = $4, default_language = COALESCE(NULLIF($5,''), sources.default_language),
		   trust_weight = $6, updated_at = $8`,
		src.ID, src.Name, src.WebsiteURL, string(src.SourceType), src.DefaultLanguage,
		src.TrustWeight, src.Enabled, s.timeVal(now))
	return err
}

// UpsertFeedFromImport inserts or updates import-owned feed metadata only. Runtime
// fields (etag, health history, failure counters, leases) are never overwritten (§267).
func (s *Store) UpsertFeedFromImport(ctx context.Context, f model.Feed, version string) (inserted bool, updated bool, err error) {
	now := s.timeVal(time.Now().UTC())
	var exists int
	if err := s.queryRow(ctx, `SELECT COUNT(*) FROM feeds WHERE normalized_xml_url = $1`, f.NormalizedXMLURL).Scan(&exists); err != nil {
		return false, false, err
	}
	if exists == 0 {
		_, err = s.exec(ctx,
			`INSERT INTO feeds (id, source_id, title, xml_url, normalized_xml_url, html_url, language, scope,
			   category_key, source_type, priority, poll_tier, enabled, health_status, health_score,
			   next_poll_at, feed_pack_version, created_at, updated_at)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,'UNKNOWN',100,$14,$15,$16,$16)`,
			f.ID, f.SourceID, f.Title, f.XMLURL, f.NormalizedXMLURL, f.HTMLURL, f.Language, f.Scope,
			f.CategoryKey, string(f.SourceType), f.Priority, string(f.PollTier), f.Enabled,
			now, version, now)
		if err != nil {
			return false, false, err
		}
		return true, false, nil
	}
	_, err = s.exec(ctx,
		`UPDATE feeds SET source_id = $2, title = $3, xml_url = $4, html_url = $5, language = $6, scope = $7,
		   category_key = $8, source_type = $9, priority = $10, poll_tier = $11, enabled = $12,
		   feed_pack_version = $13, needs_review = $14, updated_at = $15
		 WHERE normalized_xml_url = $1`,
		f.NormalizedXMLURL, f.SourceID, f.Title, f.XMLURL, f.HTMLURL, f.Language, f.Scope,
		f.CategoryKey, string(f.SourceType), f.Priority, string(f.PollTier), f.Enabled,
		version, false, now)
	if err != nil {
		return false, false, err
	}
	return false, true, nil
}

// MarkFeedsMissingFromPack flags (never deletes) feeds absent from a new feed pack (§137).
func (s *Store) MarkFeedsMissingFromPack(ctx context.Context, version string, keepNormalizedURLs []string) (int, error) {
	// $1 needs_review, $2 updated_at, $3 pack version, $4..N URLs present in the new pack.
	args := []any{true, time.Now().UTC(), version}
	clause := ""
	if len(keepNormalizedURLs) > 0 {
		placeholders := make([]string, 0, len(keepNormalizedURLs))
		for _, u := range keepNormalizedURLs {
			args = append(args, u)
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
		}
		clause = " AND normalized_xml_url NOT IN (" + strings.Join(placeholders, ",") + ")"
	}
	res, err := s.exec(ctx,
		`UPDATE feeds SET needs_review = $1, updated_at = $2
		 WHERE feed_pack_version IS NOT NULL AND feed_pack_version <> $3`+clause,
		args...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// FeedByID loads a feed with source name.
func (s *Store) FeedByID(ctx context.Context, id string) (*model.Feed, error) {
	row := s.queryRow(ctx, `SELECT `+feedColumns+` FROM feeds f LEFT JOIN sources s ON s.id = f.source_id WHERE f.id = $1`, id)
	return scanFeed(row)
}

// ListFeeds returns feeds filtered for the admin registry (§134).
func (s *Store) ListFeeds(ctx context.Context, health, sourceType, language, category, domain, search string, enabled *bool, limit, offset int) ([]model.Feed, int, error) {
	where := []string{"1=1"}
	args := []any{}
	add := func(cond string, val any) {
		args = append(args, val)
		where = append(where, strings.ReplaceAll(cond, "?", "$"+fmt.Sprint(len(args))))
	}
	if health != "" {
		add("f.health_status = ?", health)
	}
	if sourceType != "" {
		add("f.source_type = ?", sourceType)
	}
	if language != "" {
		add("f.language = ?", language)
	}
	if category != "" {
		add("f.category_key = ?", category)
	}
	if domain != "" {
		add("f.xml_url LIKE ?", "%"+domain+"%")
	}
	if search != "" {
		args = append(args, "%"+strings.ToLower(search)+"%")
		n := len(args)
		where = append(where, fmt.Sprintf("(LOWER(f.title) LIKE $%d OR LOWER(f.xml_url) LIKE $%d OR LOWER(COALESCE(s.name,'')) LIKE $%d)", n, n, n))
	}
	if enabled != nil {
		args = append(args, *enabled)
		where = append(where, fmt.Sprintf("f.enabled = $%d", len(args)))
	}
	cond := strings.Join(where, " AND ")

	var total int
	if err := s.queryRow(ctx, `SELECT COUNT(*) FROM feeds f LEFT JOIN sources s ON s.id = f.source_id WHERE `+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, limit, offset)
	q := fmt.Sprintf(`SELECT %s FROM feeds f LEFT JOIN sources s ON s.id = f.source_id WHERE %s
		ORDER BY f.priority DESC, f.id LIMIT $%d OFFSET $%d`, feedColumns, cond, len(args)-1, len(args))
	rows, err := s.query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []model.Feed{}
	for rows.Next() {
		f, err := scanFeed(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *f)
	}
	return out, total, rows.Err()
}

// FeedByNormalizedURL finds a feed by its normalized URL.
func (s *Store) FeedByNormalizedURL(ctx context.Context, normalized string) (*model.Feed, error) {
	row := s.queryRow(ctx, `SELECT `+feedColumns+` FROM feeds f LEFT JOIN sources s ON s.id = f.source_id WHERE f.normalized_xml_url = $1`, normalized)
	return scanFeed(row)
}

// EnabledFeedCount returns how many feeds are eligible for polling.
func (s *Store) EnabledFeedCount(ctx context.Context) (int, error) {
	return s.CountRow(ctx, `SELECT COUNT(*) FROM feeds WHERE enabled = $1`, true)
}

// ClaimDueFeeds leases up to limit due feeds for this worker (§147, §275).
func (s *Store) ClaimDueFeeds(ctx context.Context, owner string, limit int, lease time.Duration) ([]model.Feed, error) {
	now := time.Now().UTC()
	leaseUntil := now.Add(lease)
	if s.DB.Dialect == "postgres" {
		rows, err := s.query(ctx,
			`UPDATE feeds SET lease_owner = $1, lease_until = $2, updated_at = $3
			 WHERE id IN (
			   SELECT id FROM feeds
			   WHERE enabled = $4
			     AND (next_poll_at IS NULL OR next_poll_at <= $3)
			     AND (lease_until IS NULL OR lease_until < $3)
			     AND health_status <> 'QUARANTINED'
			     AND needs_review = $5
			   ORDER BY priority DESC, next_poll_at ASC NULLS FIRST
			   LIMIT $6
			   FOR UPDATE SKIP LOCKED
			 )
			 RETURNING id, source_id, xml_url, normalized_xml_url, COALESCE(html_url,''), title,
			   COALESCE(language,''), COALESCE(scope,''), COALESCE(category_key,''), source_type,
			   priority, poll_tier, enabled, health_status, COALESCE(etag,''), COALESCE(last_modified,''),
			   last_checked_at, last_success_at, newest_item_at, next_poll_at, consecutive_failures,
			   health_score, COALESCE(feed_pack_version,''), needs_review, COALESCE(lease_owner,''),
			   lease_until, created_at, updated_at, ''`,
			owner, s.timeVal(leaseUntil), s.timeVal(now), true, false, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		return s.collectFeedRows(rows)
	}

	// SQLite: single-writer engine; the same UPDATE ... RETURNING is lease-safe.
	rows, err := s.query(ctx,
		`UPDATE feeds SET lease_owner = $1, lease_until = $2, updated_at = $3
		 WHERE id IN (
		   SELECT id FROM feeds
		   WHERE enabled = $4
		     AND (next_poll_at IS NULL OR next_poll_at <= $3)
		     AND (lease_until IS NULL OR lease_until < $3)
		     AND health_status <> 'QUARANTINED'
		   ORDER BY priority DESC, next_poll_at ASC
		   LIMIT $5
		 )
		 RETURNING id, source_id, xml_url, normalized_xml_url, COALESCE(html_url,''), title,
		   COALESCE(language,''), COALESCE(scope,''), COALESCE(category_key,''), source_type,
		   priority, poll_tier, enabled, health_status, COALESCE(etag,''), COALESCE(last_modified,''),
		   last_checked_at, last_success_at, newest_item_at, next_poll_at, consecutive_failures,
		   health_score, COALESCE(feed_pack_version,''), needs_review, COALESCE(lease_owner,''),
		   lease_until, created_at, updated_at, ''`,
		owner, s.timeVal(leaseUntil), s.timeVal(now), true, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.collectFeedRows(rows)
}

func (s *Store) collectFeedRows(rows *sql.Rows) ([]model.Feed, error) {
	out := []model.Feed{}
	for rows.Next() {
		f, err := scanFeed(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *f)
	}
	return out, rows.Err()
}

// ReleaseFeed clears the lease and schedules the next poll (§147).
func (s *Store) ReleaseFeed(ctx context.Context, feedID string, nextPoll time.Time) error {
	_, err := s.exec(ctx,
		`UPDATE feeds SET lease_owner = NULL, lease_until = NULL, next_poll_at = $2, last_checked_at = $3, updated_at = $3
		 WHERE id = $1`, feedID, s.timeVal(nextPoll), s.nowVal())
	return err
}

// MarkFeedSuccess records a successful (possibly 304) fetch and updates health.
func (s *Store) MarkFeedSuccess(ctx context.Context, feedID string, httpStatus int, etag, lastModified string,
	newestItem *time.Time, itemCount int, durationMS int, score int, status model.HealthStatus, eventType, message string) error {
	now := time.Now().UTC()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// $7 is cast explicitly: it is only ever tested for NULL here, which leaves PostgreSQL
	// unable to infer its type (see tsCast).
	newest := s.tsCast("$7")
	if _, err := s.txExec(ctx, tx,
		`UPDATE feeds SET health_status = $2, health_score = $3, consecutive_failures = 0,
		   etag = COALESCE(NULLIF($4,''), etag), last_modified = COALESCE(NULLIF($5,''), last_modified),
		   last_success_at = $6, last_checked_at = $6,
		   newest_item_at = CASE WHEN `+newest+` IS NULL THEN newest_item_at ELSE `+newest+` END,
		   updated_at = $6
		 WHERE id = $1`,
		feedID, string(status), score, etag, lastModified, s.timeVal(now), s.DB.TimePtrVal(newestItem)); err != nil {
		return err
	}
	if _, err := s.txExec(ctx, tx,
		`INSERT INTO feed_health_events (feed_id, checked_at, http_status, parse_ok, item_count, duration_ms, event_type, message)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		feedID, s.timeVal(now), httpStatus, s.DB.BoolVal(true), itemCount, durationMS, eventType, nullString(message)); err != nil {
		return err
	}
	return tx.Commit()
}

// MarkFeedFailure records a failed fetch/parse and applies backoff (§271).
func (s *Store) MarkFeedFailure(ctx context.Context, feedID string, httpStatus int, eventType, errorCode, message string,
	failures int, score int, status model.HealthStatus, nextPoll time.Time, durationMS int) error {
	now := time.Now().UTC()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := s.txExec(ctx, tx,
		`UPDATE feeds SET health_status = $2, health_score = $3, consecutive_failures = $4,
		   last_checked_at = $5, next_poll_at = $6, lease_owner = NULL, lease_until = NULL, updated_at = $5
		 WHERE id = $1`,
		feedID, string(status), score, failures, s.timeVal(now), s.timeVal(nextPoll)); err != nil {
		return err
	}
	var hs any
	if httpStatus > 0 {
		hs = httpStatus
	}
	if _, err := s.txExec(ctx, tx,
		`INSERT INTO feed_health_events (feed_id, checked_at, http_status, parse_ok, item_count, duration_ms, event_type, error_code, message)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		feedID, s.timeVal(now), hs, s.DB.BoolVal(false), 0, durationMS, eventType, nullString(errorCode), nullString(message)); err != nil {
		return err
	}
	return tx.Commit()
}

// RecordFetchRun stores one fetch attempt (§250).
func (s *Store) RecordFetchRun(ctx context.Context, feedID string, started, finished time.Time,
	result string, httpStatus int, bytes int64, seen, inserted, dups int, requestID string) error {
	var hs any
	if httpStatus > 0 {
		hs = httpStatus
	}
	_, err := s.exec(ctx,
		`INSERT INTO feed_fetch_runs (feed_id, started_at, finished_at, result, http_status, bytes_received,
		   items_seen, items_inserted, duplicates, request_id)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		feedID, s.timeVal(started), s.timeVal(finished), result, hs, bytes, seen, inserted, dups, nullString(requestID))
	return err
}

// FeedHealthEvents returns recent health events for a feed (§142, §135).
func (s *Store) FeedHealthEvents(ctx context.Context, feedID string, limit int) ([]model.FeedHealthEvent, error) {
	rows, err := s.query(ctx,
		`SELECT id, feed_id, checked_at, COALESCE(http_status,0), parse_ok, COALESCE(item_count,0),
		        COALESCE(duration_ms,0), event_type, COALESCE(error_code,''), COALESCE(message,'')
		 FROM feed_health_events WHERE feed_id = $1 ORDER BY checked_at DESC LIMIT $2`, feedID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.FeedHealthEvent{}
	for rows.Next() {
		var ev model.FeedHealthEvent
		var checked NullTimeOf
		var parseOK any
		if err := rows.Scan(&ev.ID, &ev.FeedID, &checked, &ev.HTTPStatus, &parseOK, &ev.ItemCount,
			&ev.DurationMS, &ev.EventType, &ev.ErrorCode, &ev.Message); err != nil {
			return nil, err
		}
		ev.CheckedAt = checked.Time
		ev.ParseOK = scanBool(parseOK)
		out = append(out, ev)
	}
	return out, rows.Err()
}

// SetFeedEnabled toggles a feed (§134).
func (s *Store) SetFeedEnabled(ctx context.Context, feedID string, enabled bool) error {
	nextPoll := time.Now().UTC()
	if !enabled {
		nextPoll = time.Now().UTC().Add(365 * 24 * time.Hour)
	}
	_, err := s.exec(ctx,
		`UPDATE feeds SET enabled = $2, health_status = $3, next_poll_at = $4, lease_owner = NULL, lease_until = NULL, updated_at = $5
		 WHERE id = $1`,
		feedID, enabled, string(orDisabled(enabled)), s.timeVal(nextPoll), s.nowVal())
	return err
}

func orDisabled(enabled bool) model.HealthStatus {
	if enabled {
		return model.HealthUnknown
	}
	return model.HealthDisabled
}

// UpdateFeedAdmin applies admin-owned metadata changes (§134, §141).
func (s *Store) UpdateFeedAdmin(ctx context.Context, feedID string, priority int, tier model.PollTier, categoryKey, language string, enabled bool) error {
	_, err := s.exec(ctx,
		`UPDATE feeds SET priority = $2, poll_tier = $3, category_key = COALESCE(NULLIF($4,''), category_key),
		   language = COALESCE(NULLIF($5,''), language), enabled = $6, updated_at = $7, needs_review = $8
		 WHERE id = $1`,
		feedID, priority, string(tier), categoryKey, language, enabled, s.nowVal(), false)
	return err
}

// ScheduleFeedNow forces a feed to be polled on the next scheduler tick.
func (s *Store) ScheduleFeedNow(ctx context.Context, feedID string) error {
	_, err := s.exec(ctx,
		`UPDATE feeds SET next_poll_at = $2, lease_owner = NULL, lease_until = NULL, needs_review = $3, updated_at = $2 WHERE id = $1`,
		feedID, s.nowVal(), false)
	return err
}

// TopFailingDomains lists domains with the most failures in a window (§133).
type FailingDomain struct {
	Domain   string `json:"domain"`
	Failures int    `json:"failures"`
}

// TopFailingDomains aggregates failures by feed URL host.
func (s *Store) TopFailingDomains(ctx context.Context, since time.Time, limit int) ([]FailingDomain, error) {
	rows, err := s.query(ctx,
		`SELECT f.xml_url, COUNT(*) AS n FROM feed_health_events e
		 JOIN feeds f ON f.id = e.feed_id
		 WHERE e.checked_at >= $1 AND e.event_type IN ('HTTP_ERROR','TIMEOUT','PARSER_ERROR','RATE_LIMITED')
		 GROUP BY f.xml_url ORDER BY n DESC LIMIT $2`, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	agg := map[string]int{}
	order := []string{}
	for rows.Next() {
		var url string
		var n int
		if err := rows.Scan(&url, &n); err != nil {
			return nil, err
		}
		host := hostOf(url)
		if _, ok := agg[host]; !ok {
			order = append(order, host)
		}
		agg[host] += n
	}
	out := make([]FailingDomain, 0, len(order))
	for _, h := range order {
		out = append(out, FailingDomain{Domain: h, Failures: agg[h]})
	}
	return out, rows.Err()
}

func hostOf(raw string) string {
	u := raw
	if i := strings.Index(u, "//"); i >= 0 {
		u = u[i+2:]
	}
	if i := strings.IndexAny(u, "/?#"); i >= 0 {
		u = u[:i]
	}
	return strings.ToLower(u)
}

// StableID builds a deterministic identifier from a natural key (§265).
func StableID(prefix, naturalKey string) string {
	h := sha1.Sum([]byte(strings.ToLower(strings.TrimSpace(naturalKey))))
	return prefix + "_" + hex.EncodeToString(h[:])[:16]
}

// healthScore computes a 0..100 operational score (§169).
func HealthScore(consecutiveFailures int, parseOK bool, stale bool) int {
	score := 100
	if !parseOK {
		score -= 35
	}
	score -= consecutiveFailures * 12
	if stale {
		score -= 20
	}
	if score < 0 {
		score = 0
	}
	return score
}

// NextPollDelay applies adaptive intervals and exponential backoff with jitter (§149, §151).
func (s *Store) NextPollDelay(tier model.PollTier, failures int, recentItems int, adaptive bool) time.Duration {
	base := tier.Interval()
	if adaptive {
		if recentItems == 0 && failures == 0 {
			base = base * 2
		}
		if recentItems >= 10 {
			base = base / 2
			if base < 2*time.Minute {
				base = 2 * time.Minute
			}
		}
	}
	if failures > 0 {
		mult := 1 << min(failures, 6) // 2,4,8,...64
		base = base * time.Duration(mult)
	}
	// jitter: +-20% to avoid thundering herds, applied *before* the ceiling. Clamping first
	// and jittering second let a "6 hour cap" hand out 7.2 hours, which is what a CI run
	// caught (7h8m): a cap that the code can exceed is not a cap.
	jittered := time.Duration(int64(float64(base) * (0.8 + 0.4*float64(time.Now().UnixNano()%1000)/1000.0)))
	if jittered > maxPollDelay {
		return maxPollDelay
	}
	if jittered < minPollDelay {
		return minPollDelay
	}
	return jittered
}

// Polling bounds (§149). A feed is never polled faster than [minPollDelay] and never
// scheduled further out than [maxPollDelay], whatever the jitter draw says.
const (
	minPollDelay = 2 * time.Minute
	maxPollDelay = 6 * time.Hour
)
