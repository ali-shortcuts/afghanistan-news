// Package ingest implements the ingestion pipeline (§146):
//
//	scheduler -> fetcher -> parser -> normalizer -> dedupe -> classifier -> storage
//	          -> story clustering -> breaking evaluator -> push queue
//
// Each stage is independently testable and no stage wraps slow network work in a long
// database transaction (§274).
package ingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/afnews/backend/internal/feed/classify"
	"github.com/afnews/backend/internal/feed/fetcher"
	"github.com/afnews/backend/internal/feed/normalize"
	"github.com/afnews/backend/internal/feed/parser"
	"github.com/afnews/backend/internal/model"
	"github.com/afnews/backend/internal/observability"
	"github.com/afnews/backend/internal/push"
	"github.com/afnews/backend/internal/store"
)

// Options configures the worker.
type Options struct {
	Owner             string
	BatchSize         int
	Concurrency       int
	SchedulerTick     time.Duration
	MaxItemsPerRun    int
	AdaptivePolling   bool
	QuarantineAfter   int
	DomainConcurrency int
	RequestsPerMinute int
	UserAgent         string
	FetchTimeout      time.Duration
	MaxResponseBytes  int64
	AllowPrivateFetch bool
	PushEnabled       bool
	PushDryRun        bool
	PushMaxPerHour    int
	PushMaxPerDay     int
	// MaxBreakingPerRun caps how many items one feed run may flag as breaking. Without it a
	// high-volume wire (a seismograph/alert stream) can turn its entire backlog into
	// "breaking" and saturate the push queue (§38, §164).
	MaxBreakingPerRun  int
	SearchIndexRefresh bool
}

// Pipeline is the worker implementation.
type Pipeline struct {
	store    *store.Store
	fetcher  *fetcher.Fetcher
	classify *classify.Classifier
	pusher   push.Sender
	metrics  *observability.Metrics
	log      *slog.Logger
	opts     Options
}

// New builds a Pipeline.
func New(st *store.Store, pusher push.Sender, metrics *observability.Metrics, log *slog.Logger, opts Options) *Pipeline {
	if opts.BatchSize <= 0 {
		opts.BatchSize = 24
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 8
	}
	if opts.SchedulerTick <= 0 {
		opts.SchedulerTick = 5 * time.Second
	}
	if opts.MaxItemsPerRun <= 0 {
		opts.MaxItemsPerRun = 60
	}
	if opts.QuarantineAfter <= 0 {
		opts.QuarantineAfter = 12
	}
	f := fetcher.New(fetcher.Options{
		UserAgent:         opts.UserAgent,
		Timeout:           opts.FetchTimeout,
		MaxBytes:          opts.MaxResponseBytes,
		DomainConcurrency: opts.DomainConcurrency,
		RequestsPerMinute: opts.RequestsPerMinute,
		AllowPrivate:      opts.AllowPrivateFetch,
	})
	return &Pipeline{
		store:    st,
		fetcher:  f,
		classify: classify.New(),
		pusher:   pusher,
		metrics:  metrics,
		log:      log,
		opts:     opts,
	}
}

// Run polls the registry in a loop until the context is cancelled.
func (p *Pipeline) Run(ctx context.Context) error {
	p.log.Info("ingest worker started", "owner", p.opts.Owner, "batch", p.opts.BatchSize, "concurrency", p.opts.Concurrency)
	ticker := time.NewTicker(p.opts.SchedulerTick)
	defer ticker.Stop()

	for {
		claimed, err := p.RunOnce(ctx)
		if err != nil {
			p.log.Error("ingest cycle failed", "error", err)
		} else if claimed == 0 {
			// Idle: nothing due, keep the loop responsive without spinning.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(p.opts.SchedulerTick):
			}
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// RunOnce claims and processes one batch of due feeds. It returns the number of feeds
// claimed; zero means the scheduler has nothing due (§147).
func (p *Pipeline) RunOnce(ctx context.Context) (int, error) {
	feeds, err := p.store.ClaimDueFeeds(ctx, p.opts.Owner, p.opts.BatchSize, 3*time.Minute)
	if err != nil {
		return 0, err
	}
	if len(feeds) == 0 {
		return 0, nil
	}

	sem := make(chan struct{}, p.opts.Concurrency)
	var wg sync.WaitGroup
	for i := range feeds {
		feed := feeds[i]
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			// A panic in one feed must never take down the worker (§175).
			defer func() {
				if r := recover(); r != nil {
					p.log.Error("feed processing panicked", "feed", feed.ID, "panic", r)
					p.metrics.Inc("feed_panic_total")
				}
			}()
			p.ProcessFeed(ctx, feed)
		}()
	}
	wg.Wait()
	return len(feeds), nil
}

// ProcessFeed performs the full single-feed pipeline (§274).
func (p *Pipeline) ProcessFeed(ctx context.Context, feed model.Feed) {
	start := time.Now()
	feedCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	resp, err := p.fetcher.Fetch(feedCtx, feed)
	if errors.Is(err, fetcher.ErrNotModified) {
		// 304 Not Modified: no parsing, no writes beyond health/schedule (§151).
		p.metrics.Inc("feed_http_304_total")
		p.metrics.Observe("feed_fetch_duration_seconds", time.Since(start).Seconds())
		score := store.HealthScore(0, true, false)
		_ = p.store.MarkFeedSuccess(ctx, feed.ID, 304, feed.ETag, feed.LastModified, nil, 0,
			int(time.Since(start).Milliseconds()), score, model.HealthHealthy, "NOT_MODIFIED", "")
		_ = p.store.RecordFetchRun(ctx, feed.ID, start, time.Now(), "NOT_MODIFIED", 304, 0, 0, 0, 0, "")
		p.scheduleNext(ctx, feed, 0, 0, feed.ConsecutiveFailures)
		return
	}
	if err != nil {
		p.handleFetchError(ctx, feed, err, start)
		return
	}

	p.metrics.Inc("feed_fetch_total")
	p.metrics.Observe("feed_fetch_duration_seconds", time.Since(start).Seconds())

	parsed, perr := parser.Parse(strings.NewReader(string(resp.Body)))
	if perr != nil && (parsed == nil || len(parsed.Candidates) == 0) {
		p.metrics.Inc("feed_parse_errors_total")
		score := store.HealthScore(feed.ConsecutiveFailures+1, false, false)
		next := time.Now().UTC().Add(p.store.NextPollDelay(model.PollTier(feed.PollTier),
			feed.ConsecutiveFailures+1, 0, p.opts.AdaptivePolling))
		_ = p.store.MarkFeedFailure(ctx, feed.ID, resp.StatusCode, "PARSER_ERROR", "PARSER_ERROR",
			trimErr(perr), feed.ConsecutiveFailures+1, score, model.HealthParserError, next, int(time.Since(start).Milliseconds()))
		_ = p.store.RecordFetchRun(ctx, feed.ID, start, time.Now(), "PARSER_ERROR", resp.StatusCode, resp.Bytes, 0, 0, 0, "")
		p.log.Warn("feed parse failed", "feed", feed.ID, "url", feed.XMLURL, "error", perr)
		return
	}

	seen, inserted, dups := p.ingestCandidates(ctx, feed, parsed.Candidates, resp)
	p.metrics.Add("articles_inserted_total", float64(inserted))
	p.metrics.Add("articles_duplicate_total", float64(dups))

	var newest *time.Time
	for _, c := range parsed.Candidates {
		if c.PublishedAt != nil && (newest == nil || c.PublishedAt.After(*newest)) {
			t := *c.PublishedAt
			newest = &t
		}
	}

	stale := newest != nil && time.Since(*newest) > staleWindow(model.PollTier(feed.PollTier))
	status := model.HealthHealthy
	if len(parsed.Candidates) == 0 {
		status = model.HealthEmpty
	} else if stale {
		status = model.HealthStale
	}
	score := store.HealthScore(0, true, stale)
	_ = p.store.MarkFeedSuccess(ctx, feed.ID, resp.StatusCode, resp.ETag, resp.LastModified, newest,
		len(parsed.Candidates), int(time.Since(start).Milliseconds()), score, status, "FETCH_OK",
		strings.Join(parsed.Meta.Warnings, "; "))
	_ = p.store.RecordFetchRun(ctx, feed.ID, start, time.Now(), "OK", resp.StatusCode, resp.Bytes,
		len(parsed.Candidates), inserted, dups, "")

	p.scheduleNext(ctx, feed, len(parsed.Candidates), seen, 0)

	p.log.Info("feed processed",
		"feed_id", feed.ID, "domain", domainOf(feed.XMLURL), "status", resp.StatusCode,
		"items_seen", len(parsed.Candidates), "items_inserted", inserted, "duplicates", dups,
		"duration_ms", time.Since(start).Milliseconds())
}

// ingestCandidates normalizes, deduplicates, classifies and stores parsed items (§156-§164).
func (p *Pipeline) ingestCandidates(ctx context.Context, feed model.Feed, candidates []model.Candidate, resp *fetcher.Response) (seen, inserted, dups int) {
	limit := p.opts.MaxItemsPerRun
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	runItems := len(candidates)
	breakingFlagged := 0

	for _, cand := range candidates {
		seen++
		normalizedURL, canonicalURL, err := normalize.CanonicalizeURL(cand.Link)
		if err != nil {
			// Reject unusable/malicious URIs instead of storing them (§53).
			p.metrics.Inc("article_rejected_total")
			continue
		}
		title := strings.TrimSpace(normalize.StripHTML(cand.Title))
		if title == "" || len([]rune(title)) < 6 {
			p.metrics.Inc("article_rejected_total")
			continue
		}
		summary := normalize.Truncate(normalize.StripHTML(cand.Summary), 600)
		content := normalize.SanitizeHTML(cand.Content)
		language := normalize.DetectLanguage(feed.Language, title, summary)
		published := cand.PublishedAt

		normalizedTitle := normalize.NormalizedTitle(title)
		var fingerprint string
		if published != nil {
			fingerprint = normalize.TitleFingerprint(normalizedTitle, *published)
		} else {
			fingerprint = normalize.TitleFingerprint(normalizedTitle, time.Now().UTC())
		}
		contentHash := normalize.ContentHash(normalizedTitle, summary)

		imageURL := cand.ImageURL
		if imageURL == "" {
			imageURL = normalize.ExtractFirstImage(content)
		}

		cls := p.classify.Classify(feed, title, summary+" "+content, canonicalURL)
		opportunity := classify.ExtractOpportunity(feed, title, summary)

		id := store.StableID("art", canonicalURL+feed.ID)
		input := store.ArticleInput{
			ID:               id,
			SourceID:         feed.SourceID,
			FeedID:           feed.ID,
			ExternalGUID:     cand.ExternalGUID,
			CanonicalURL:     canonicalURL,
			OriginalURL:      cand.Link,
			NormalizedURL:    normalizedURL,
			Title:            title,
			NormalizedTitle:  normalizedTitle,
			Summary:          summary,
			FeedContent:      content,
			ImageURL:         imageURL,
			Author:           strings.TrimSpace(cand.Author),
			PublishedAt:      published,
			UpdatedAt:        cand.UpdatedAt,
			DiscoveredAt:     time.Now().UTC(),
			Language:         language,
			ContentHash:      contentHash,
			Categories:       cls.Categories,
			Provinces:        cls.Provinces,
			Opportunity:      opportunity,
			TitleFingerprint: fingerprint,
		}

		result, err := p.store.InsertArticle(ctx, input)
		if err != nil {
			p.log.Warn("article insert failed", "feed", feed.ID, "title", title, "error", err)
			continue
		}
		if result.Outcome == store.OutcomeDuplicate {
			dups++
			if err := p.attachToCluster(ctx, feed, input, result.ArticleID); err != nil {
				p.log.Debug("cluster attach skipped", "error", err)
			}
			continue
		}
		inserted++
		if err := p.attachToCluster(ctx, feed, input, result.ArticleID); err != nil {
			p.log.Debug("cluster assign skipped", "error", err)
		}
		if p.evaluateBreaking(ctx, feed, input, result.ArticleID, runItems, breakingFlagged) {
			breakingFlagged++
		}
	}
	return seen, inserted, dups
}

// attachToCluster groups independent reports of the same story (§159 level 5, §160).
func (p *Pipeline) attachToCluster(ctx context.Context, feed model.Feed, in store.ArticleInput, articleID string) error {
	if in.TitleFingerprint == "" {
		return nil
	}
	since := in.DiscoveredAt.Add(-36 * time.Hour)
	clusterID, err := p.store.FindClusterCandidate(ctx, in.TitleFingerprint, since)
	if err != nil {
		return err
	}
	if clusterID == "" {
		// Exact topic-key miss. Independent publishers reword the same story, so the
		// exact fingerprint alone would split one event into many clusters. Fall back
		// to token-overlap matching over recent clustered titles (§159 level 5 fallback).
		seeds, seedErr := p.store.RecentClusteredSeeds(ctx, since, nearDupScanLimit)
		if seedErr != nil {
			p.log.Debug("near-duplicate scan unavailable", "error", seedErr)
		} else if id := nearDuplicateCluster(in.NormalizedTitle, seeds); id != "" {
			clusterID = id
		}
	}
	if clusterID == "" {
		clusterID = store.StableID("cluster", in.TitleFingerprint)
	}
	published := in.DiscoveredAt
	if in.PublishedAt != nil {
		published = *in.PublishedAt
	}
	return p.store.AssignCluster(ctx, articleID, clusterID, in.TitleFingerprint, published)
}

// nearDupScanLimit bounds the near-duplicate seed scan: the newest 300 clustered
// articles inside the window cover the live news cycle while keeping the per-insert
// cost at a few hundred uint64 comparisons.
const nearDupScanLimit = 300

// nearDuplicateCluster picks the cluster of the first seed whose title describes
// the same story under the token-overlap gate. Pure function so the matching rule
// stays unit-testable without a database.
func nearDuplicateCluster(candNormalizedTitle string, seeds []store.ClusterSeed) string {
	if strings.TrimSpace(candNormalizedTitle) == "" {
		return ""
	}
	for _, seed := range seeds {
		if normalize.NearDuplicateTitle(candNormalizedTitle, normalize.NormalizedTitle(seed.Title)) {
			return seed.ClusterID
		}
	}
	return ""
}

// breakingSignals is the pure input of the breaking decision, which keeps the rule
// testable without a database (§38, §164).
type breakingSignals struct {
	OfficialRealtime bool
	Priority         int
	Scope            string
	SafetyCategory   string // security | disasters | breaking, when the classifier matched one
	RecentPublish    bool
	DistinctSources  int
	UrgencyMarker    bool
}

// breakingScore is the deterministic weight table (§164): it is additive and auditable, so an
// editor can always explain why a story was flagged.
func breakingScore(s breakingSignals) (float64, []string) {
	score := 0.0
	reasons := []string{}

	if s.OfficialRealtime {
		score += 0.45
		reasons = append(reasons, "official-realtime source")
	}
	if s.Priority >= 5 {
		score += 0.15
		reasons = append(reasons, "priority 5 source")
	} else if s.Priority == 4 {
		score += 0.08
	}
	if s.SafetyCategory != "" {
		score += 0.2
		reasons = append(reasons, "safety-critical category "+s.SafetyCategory)
	}
	if s.RecentPublish {
		score += 0.1
	}
	if s.DistinctSources >= 3 {
		score += 0.25
		reasons = append(reasons, fmt.Sprintf("%d independent sources in window", s.DistinctSources))
	}
	return score, reasons
}

// breakingThreshold is the score a story must reach to be flagged.
const breakingThreshold = 0.6

// bulkStreamItems is the run size above which a feed is treated as a stream rather than an
// event source: such feeds must be corroborated or carry an explicit severity marker.
const bulkStreamItems = 10

// decideBreaking is the whole rule, as a pure function:
//
//	flag = score >= 0.6  AND  a qualifying signal exists  AND  the run cap is not exhausted
//
// The qualifying signal is what stops "well-connected source" from being sufficient on its
// own: an official realtime feed on priority 5 already scores exactly 0.60, which would mark
// every item it publishes as breaking.
func decideBreaking(s breakingSignals, runItems, alreadyFlagged, maxPerRun int) (bool, float64, []string) {
	score, reasons := breakingScore(s)

	qualifying := ""
	switch {
	case strings.HasPrefix(s.Scope, "afghanistan"):
		qualifying = "local relevance"
	case s.SafetyCategory != "":
		qualifying = "safety-critical category"
	case s.DistinctSources >= 3:
		qualifying = "corroborated by independent sources"
	case s.UrgencyMarker:
		qualifying = "explicit urgency marker"
	case s.DistinctSources == 2:
		qualifying = "two independent sources"
	}
	if qualifying != "" {
		reasons = append(reasons, qualifying)
	}

	if maxPerRun <= 0 {
		maxPerRun = 2
	}
	switch {
	case score < breakingThreshold:
		return false, score, reasons
	case qualifying == "":
		reasons = append(reasons, "no qualifying signal (source ranking alone is not enough)")
		return false, score, reasons
	case alreadyFlagged >= maxPerRun:
		reasons = append(reasons, fmt.Sprintf("per-run breaking cap reached (%d)", maxPerRun))
		return false, score, reasons
	case runItems >= bulkStreamItems && s.DistinctSources < 3 && !s.UrgencyMarker && s.SafetyCategory == "":
		reasons = append(reasons, fmt.Sprintf("bulk stream (%d items this run) without corroboration or severity", runItems))
		return false, score, reasons
	}
	return true, score, reasons
}

// evaluateBreaking applies the deterministic breaking rule (§38, §164) and returns whether the
// article was flagged. A story never becomes breaking merely because it is new.
func (p *Pipeline) evaluateBreaking(ctx context.Context, feed model.Feed, in store.ArticleInput, articleID string, runItems, alreadyFlagged int) bool {
	sig := breakingSignals{
		OfficialRealtime: feed.SourceType == model.SourceOfficialRealtime,
		Priority:         feed.Priority,
		Scope:            feed.Scope,
		RecentPublish:    in.PublishedAt != nil && time.Since(*in.PublishedAt) < 45*time.Minute,
		UrgencyMarker:    hasUrgencyMarker(in.Title + " " + in.Summary),
	}
	sig.SafetyCategory = safetyCategoryOf(in.Categories)
	if in.TitleFingerprint != "" {
		if n, err := p.store.DistinctSourceCountForTopic(ctx, in.TitleFingerprint, in.DiscoveredAt.Add(-6*time.Hour)); err == nil {
			sig.DistinctSources = n
		}
	}

	flag, score, reasons := decideBreaking(sig, runItems, alreadyFlagged, p.opts.MaxBreakingPerRun)
	if !flag {
		p.log.Debug("breaking not flagged", "article", articleID, "score", score, "reasons", strings.Join(reasons, "; "))
		return false
	}
	if err := p.store.SetArticleBreaking(ctx, articleID, true, score); err != nil {
		p.log.Warn("breaking flag failed", "article", articleID, "error", err)
		return false
	}
	p.metrics.Inc("breaking_flagged_total")
	p.log.Info("breaking story flagged", "article", articleID, "score", score, "reasons", strings.Join(reasons, "; "))

	if p.opts.PushEnabled {
		p.enqueuePush(ctx, feed, in, articleID, score)
	}
	return true
}

// safetyCategoryOf returns the article's safety-critical topic, and only when that topic is the
// article's *primary* one. A feed-folder label is not evidence: the seismograph feeds arrive
// tagged disasters/climate/science from their folder, and with a secondary tag counting as a
// safety signal every magnitude-1.0 reading scored 0.80 and was flagged breaking.
func safetyCategoryOf(cats []model.CategoryAssignment) string {
	if len(cats) == 0 {
		return ""
	}
	best := cats[0]
	for _, c := range cats[1:] {
		if c.Confidence > best.Confidence || (c.Confidence == best.Confidence && c.ID < best.ID) {
			best = c
		}
	}
	switch best.ID {
	case "security", "disasters":
		return best.ID
	}
	return ""
}

// urgencyRe recognises explicit urgency or severity language. Numeric severity is included
// because a seismograph or alert stream states its severity in numbers, not in prose
// (a magnitude-1.0 tremor must never be breaking news).
var urgencyRe = regexp.MustCompile(`(?i)` +
	`\b(urgent|breaking|flash|emergency|evacuat\w+|death toll|killed|dead|explosion|blast|airstrike|air strike|earthquake|landslide|flash flood|displaced)\b` +
	`|\bM\s?[5-9](?:\.\d)?\b` +
	`|\b\d{1,4}\s+(?:deaths?|killed|injured|casualties|displaced)\b` +
	`|فوری|اضطراری|هشدار|انفجار|حمله|زمینلرزه|زلزله|سیل|رانش زمین|کشته|زخمی|تلفات|مرگ`)

func hasUrgencyMarker(text string) bool { return urgencyRe.MatchString(text) }

// enqueuePush applies notification deduplication and throttling before sending (§165, §166).
func (p *Pipeline) enqueuePush(ctx context.Context, feed model.Feed, in store.ArticleInput, articleID string, score float64) {
	topic := pushTopicFor(feed, in)
	dedupKey := in.TitleFingerprint
	if dedupKey == "" {
		dedupKey = articleID
	}
	dedupKey = topic + "|" + dedupKey

	exists, err := p.store.PushEventExists(ctx, dedupKey)
	if err != nil || exists {
		return
	}
	hourCount, _ := p.store.CountPushEventsSince(ctx, time.Now().UTC().Add(-time.Hour), "")
	if p.opts.PushMaxPerHour > 0 && hourCount >= p.opts.PushMaxPerHour {
		p.log.Info("push suppressed by hourly throttle", "topic", topic, "hour_count", hourCount)
		return
	}
	dayCount, _ := p.store.CountPushEventsSince(ctx, time.Now().UTC().Add(-24*time.Hour), "")
	if p.opts.PushMaxPerDay > 0 && dayCount >= p.opts.PushMaxPerDay {
		p.log.Info("push suppressed by daily throttle", "topic", topic, "day_count", dayCount)
		return
	}

	title := in.Title
	body := in.Summary
	if body == "" {
		body = feed.SourceName
	}
	event := model.PushEvent{
		ID:        store.StableID("push", dedupKey),
		ArticleID: articleID,
		Topic:     topic,
		Title:     title,
		Body:      normalize.Truncate(body, 180),
		DedupKey:  dedupKey,
		Status:    "QUEUED",
		CreatedAt: time.Now().UTC(),
		Actor:     "ingest-worker",
	}

	audience, err := p.store.PushAudienceSize(ctx, topic)
	if err != nil {
		audience = 0
	}
	event.Audience = audience

	sent, err := p.pusher.Send(ctx, push.Message{
		Topic:     topic,
		Title:     title,
		Body:      event.Body,
		ArticleID: articleID,
		Score:     score,
	})
	switch {
	case err != nil:
		event.Status = "FAILED"
		p.log.Warn("push send failed", "topic", topic, "error", err)
		p.metrics.Inc("push_failed_total")
	case p.opts.PushDryRun || sent.Dry:
		// The message is fully assembled and recorded, but nothing left the building (§39):
		// say so instead of reporting a delivery that never happened.
		event.Status = "DRY_RUN"
		p.metrics.Inc("push_dry_run_total")
	default:
		now := time.Now().UTC()
		event.Status = "SENT"
		event.SentAt = &now
		p.metrics.Inc("push_sent_total")
	}
	if err := p.store.RecordPushEvent(ctx, event); err != nil {
		p.log.Warn("push event record failed", "error", err)
	}
}

func (p *Pipeline) handleFetchError(ctx context.Context, feed model.Feed, err error, start time.Time) {
	failures := feed.ConsecutiveFailures + 1
	score := store.HealthScore(failures, true, false)
	status := model.HealthDegraded
	eventType := "HTTP_ERROR"
	code := "HTTP_ERROR"
	backoff := p.store.NextPollDelay(model.PollTier(feed.PollTier), failures, 0, p.opts.AdaptivePolling)

	var httpErr *fetcher.HTTPError
	switch {
	case errors.Is(err, fetcher.ErrBlocked):
		status, eventType, code = model.HealthQuarantined, "SECURITY_BLOCKED", "SSRF_BLOCKED"
		backoff = 24 * time.Hour
	case errors.Is(err, fetcher.ErrTimeout):
		status, eventType, code = model.HealthDegraded, "TIMEOUT", "TIMEOUT"
	case errors.Is(err, fetcher.ErrTooLarge):
		status, eventType, code = model.HealthUnstable, "RESPONSE_TOO_LARGE", "RESPONSE_GUARD"
	case errors.Is(err, fetcher.ErrBadType):
		status, eventType, code = model.HealthUnstable, "PARSER_ERROR", "HTML_RESPONSE"
	case errors.As(err, &httpErr):
		switch {
		case httpErr.StatusCode == 429:
			status, eventType, code = model.HealthRateLimited, "RATE_LIMITED", "HTTP_429"
			if httpErr.RetryAfter > 0 {
				backoff = httpErr.RetryAfter
			} else {
				backoff = time.Hour
			}
		case httpErr.StatusCode == 404 || httpErr.StatusCode == 410:
			status, eventType, code = model.HealthHTTPError, "HTTP_ERROR", fmt.Sprintf("HTTP_%d", httpErr.StatusCode)
			backoff = 24 * time.Hour // never hammer a permanently gone feed (§271)
		case httpErr.StatusCode == 401 || httpErr.StatusCode == 403:
			// A publisher refusing our crawler is a policy answer, not a security incident:
			// remember it as DISABLED with a long backoff instead of quarantining the feed
			// (quarantine is reserved for SSRF/DNS blocks, §271).
			status, eventType, code = model.HealthDisabled, "HTTP_ERROR", fmt.Sprintf("HTTP_%d", httpErr.StatusCode)
			backoff = 12 * time.Hour
		case httpErr.StatusCode >= 500:
			status, eventType, code = model.HealthDegraded, "HTTP_ERROR", fmt.Sprintf("HTTP_%d", httpErr.StatusCode)
		default:
			status, eventType = model.HealthDegraded, "HTTP_ERROR"
			code = fmt.Sprintf("HTTP_%d", httpErr.StatusCode)
		}
	default:
		if strings.Contains(err.Error(), "circuit open") {
			status, eventType, code = model.HealthDegraded, "CIRCUIT_OPEN", "CIRCUIT_OPEN"
		}
	}

	if failures >= p.opts.QuarantineAfter && status != model.HealthQuarantined {
		status = model.HealthUnstable
	}

	var httpStatus int
	if httpErr != nil {
		httpStatus = httpErr.StatusCode
	}
	_ = p.store.MarkFeedFailure(ctx, feed.ID, httpStatus, eventType, code, trimErr(err), failures, score,
		status, time.Now().UTC().Add(backoff), int(time.Since(start).Milliseconds()))
	_ = p.store.RecordFetchRun(ctx, feed.ID, start, time.Now(), "ERROR", httpStatus, 0, 0, 0, 0, "")
	p.metrics.Inc("feed_fetch_failure_total")
	p.log.Warn("feed fetch failed",
		"feed_id", feed.ID, "domain", domainOf(feed.XMLURL), "event", eventType, "code", code,
		"failures", failures, "backoff", backoff.String(), "error", trimErr(err))
}

// scheduleNext computes the next poll time from tier, adaptivity and failures (§148, §149).
func (p *Pipeline) scheduleNext(ctx context.Context, feed model.Feed, itemCount int, _ int, failures int) {
	delay := p.store.NextPollDelay(model.PollTier(feed.PollTier), failures, itemCount, p.opts.AdaptivePolling)
	if err := p.store.ReleaseFeed(ctx, feed.ID, time.Now().UTC().Add(delay)); err != nil {
		p.log.Warn("release feed failed", "feed", feed.ID, "error", err)
	}
}

func pushTopicFor(feed model.Feed, in store.ArticleInput) string {
	switch {
	case feed.Scope == "afghanistan" || feed.Scope == "afghanistan-province" || feed.Scope == "afghanistan-related":
		return "breaking-afghanistan"
	case feed.Scope == "global" || feed.Scope == "country":
		return "world-breaking"
	default:
		return "breaking-afghanistan"
	}
}

func staleWindow(tier model.PollTier) time.Duration {
	switch tier {
	case model.TierBreaking, model.TierHigh:
		return 24 * time.Hour
	case model.TierNormal:
		return 72 * time.Hour
	default:
		return 14 * 24 * time.Hour
	}
}

func domainOf(raw string) string {
	s := raw
	if i := strings.Index(s, "//"); i >= 0 {
		s = s[i+2:]
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	return s
}

func trimErr(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if len(msg) > 300 {
		msg = msg[:300]
	}
	return msg
}
