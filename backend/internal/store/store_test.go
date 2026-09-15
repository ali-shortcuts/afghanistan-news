package store

import (
	"context"
	"testing"
	"time"

	"github.com/afnews/backend/internal/db"
	"github.com/afnews/backend/internal/model"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	ctx := context.Background()
	// SQLite by default; TEST_DB_DRIVER=postgres runs the very same tests against production.
	database := db.TestDatabase(t)
	st := New(database)
	if err := st.SeedReference(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return st
}

func seedSourceAndFeed(t *testing.T, st *Store) model.Feed {
	t.Helper()
	ctx := context.Background()
	if err := st.UpsertSource(ctx, model.Source{
		ID: "src_test", Name: "Test Publisher", WebsiteURL: "https://test.example",
		SourceType: model.SourceDirectPublisher, DefaultLanguage: "fa", TrustWeight: 4,
		Enabled: true, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert source: %v", err)
	}
	feed := model.Feed{
		ID: "feed_test", SourceID: "src_test", Title: "Test Feed", XMLURL: "https://test.example/feed",
		NormalizedXMLURL: "https://test.example/feed", HTMLURL: "https://test.example/",
		Language: "fa", Scope: "afghanistan", CategoryKey: "afghanistan-direct-news",
		SourceType: model.SourceDirectPublisher, Priority: 4, PollTier: model.TierHigh,
		Enabled: true, HealthStatus: model.HealthUnknown, HealthScore: 100, FeedPackVersion: "v0.2",
	}
	if _, _, err := st.UpsertFeedFromImport(ctx, feed, "v0.2"); err != nil {
		t.Fatalf("upsert feed: %v", err)
	}
	return feed
}

func insertArticle(t *testing.T, st *Store, title, url, guid string) InsertResult {
	t.Helper()
	res, err := st.InsertArticle(context.Background(), ArticleInput{
		ID:              StableID("art", url),
		SourceID:        "src_test",
		FeedID:          "feed_test",
		ExternalGUID:    guid,
		CanonicalURL:    url,
		OriginalURL:     url,
		NormalizedURL:   url,
		Title:           title,
		NormalizedTitle: title,
		Summary:         "summary",
		DiscoveredAt:    time.Now().UTC(),
		Language:        "fa",
		Categories:      []model.CategoryAssignment{{ID: "economy", Confidence: 0.9, Origin: model.OriginRule}},
		Provinces:       []model.ProvinceAssignment{{ID: "kabul", Confidence: 0.8, Origin: model.OriginRule}},
		ContentHash:     "hash-" + url,
	})
	if err != nil {
		t.Fatalf("insert article: %v", err)
	}
	return res
}

func TestInsertArticleAndDeduplicate(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)
	seedSourceAndFeed(t, st)

	first := insertArticle(t, st, "کابل کې نوی پروژه پیل شوه", "https://test.example/a", "guid-1")
	if first.Outcome != OutcomeInserted {
		t.Fatalf("first insert outcome = %v", first.Outcome)
	}

	// Level 2: same URL
	dup := insertArticle(t, st, "different title", "https://test.example/a", "guid-2")
	if dup.Outcome != OutcomeDuplicate || dup.DedupLevel != "URL" {
		t.Fatalf("url dedupe failed: %+v", dup)
	}

	// Level 1: same feed + guid
	dupGUID := insertArticle(t, st, "another title", "https://test.example/b", "guid-1")
	if dupGUID.Outcome != OutcomeDuplicate || dupGUID.DedupLevel != "GUID" {
		t.Fatalf("guid dedupe failed: %+v", dupGUID)
	}

	n, err := st.CountArticles(ctx, model.ArticleQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("article count = %d, want 1", n)
	}
}

func TestArticleQueryFiltersAndCursor(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)
	seedSourceAndFeed(t, st)

	base := time.Now().UTC().Add(-2 * time.Hour)
	for i := 0; i < 12; i++ {
		pub := base.Add(time.Duration(i) * 5 * time.Minute)
		res, err := st.InsertArticle(ctx, ArticleInput{
			ID: StableID("art", "https://test.example/n"+itoa(i)), SourceID: "src_test", FeedID: "feed_test",
			CanonicalURL: "https://test.example/n" + itoa(i), OriginalURL: "https://test.example/n" + itoa(i),
			NormalizedURL: "https://test.example/n" + itoa(i), Title: "خبر شماره " + itoa(i),
			NormalizedTitle: "خبر شماره " + itoa(i), Summary: "خلاصه", PublishedAt: &pub,
			DiscoveredAt: pub, Language: "fa",
			Categories: []model.CategoryAssignment{{ID: "economy", Confidence: 0.9, Origin: model.OriginRule}},
		})
		if err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
		if res.Outcome != OutcomeInserted {
			t.Fatalf("insert %d deduped unexpectedly", i)
		}
	}

	arts, next, err := st.QueryArticles(ctx, model.ArticleQuery{Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 5 {
		t.Fatalf("page size = %d, want 5", len(arts))
	}
	if next == "" {
		t.Fatal("expected a next cursor")
	}
	cursor, err := DecodeCursor(next)
	if err != nil || cursor == nil {
		t.Fatalf("cursor decode failed: %v", err)
	}
	arts2, _, err := st.QueryArticles(ctx, model.ArticleQuery{Limit: 5, Cursor: next})
	if err != nil {
		t.Fatal(err)
	}
	if len(arts2) != 5 {
		t.Fatalf("second page size = %d, want 5", len(arts2))
	}
	if arts[0].ID == arts2[0].ID {
		t.Fatal("cursor pagination returned the same first item")
	}

	filtered, _, err := st.QueryArticles(ctx, model.ArticleQuery{Category: "economy", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 12 {
		t.Fatalf("category filter returned %d, want 12", len(filtered))
	}
	none, _, err := st.QueryArticles(ctx, model.ArticleQuery{Category: "cricket", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Fatalf("unmatched category returned %d rows", len(none))
	}
}

func TestFeedClaimingAndHealth(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)
	feed := seedSourceAndFeed(t, st)

	claimed, err := st.ClaimDueFeeds(ctx, "worker-1", 10, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claimed = %d, want 1", len(claimed))
	}
	// A second worker must not double-claim while the lease is active.
	again, err := st.ClaimDueFeeds(ctx, "worker-2", 10, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Fatalf("lease was not respected: %d feeds claimed twice", len(again))
	}

	newest := time.Now().UTC().Add(-time.Minute)
	if err := st.MarkFeedSuccess(ctx, feed.ID, 200, "etag-1", "Mon, 14 Sep 2026 06:00:00 GMT", &newest,
		12, 220, 100, model.HealthHealthy, "FETCH_OK", ""); err != nil {
		t.Fatal(err)
	}
	if err := st.ReleaseFeed(ctx, feed.ID, time.Now().UTC().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	updated, err := st.FeedByID(ctx, feed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.HealthStatus != model.HealthHealthy || updated.ETag != "etag-1" {
		t.Fatalf("health update lost: %+v", updated)
	}

	// Backoff and failure counting.
	if err := st.MarkFeedFailure(ctx, feed.ID, 503, "HTTP_ERROR", "HTTP_503", "upstream unavailable",
		3, 64, model.HealthDegraded, time.Now().UTC().Add(30*time.Minute), 900); err != nil {
		t.Fatal(err)
	}
	after, err := st.FeedByID(ctx, feed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.ConsecutiveFailures != 3 || after.HealthStatus != model.HealthDegraded {
		t.Fatalf("failure state wrong: %+v", after)
	}
	events, err := st.FeedHealthEvents(ctx, feed.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("health events = %d, want 2", len(events))
	}
}

func TestDisabledFeedIsNotClaimed(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)
	feed := seedSourceAndFeed(t, st)
	if err := st.SetFeedEnabled(ctx, feed.ID, false); err != nil {
		t.Fatal(err)
	}
	claimed, err := st.ClaimDueFeeds(ctx, "worker-1", 10, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 0 {
		t.Fatalf("disabled feed was claimed: %d", len(claimed))
	}
}

func TestHomePayloadSections(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)
	seedSourceAndFeed(t, st)

	insert := func(title, slug string, cat string, breaking bool) {
		pub := time.Now().UTC().Add(-10 * time.Minute)
		_, err := st.InsertArticle(ctx, ArticleInput{
			ID: StableID("art", slug), SourceID: "src_test", FeedID: "feed_test",
			CanonicalURL: slug, OriginalURL: slug, NormalizedURL: slug, Title: title,
			NormalizedTitle: title, Summary: "s", PublishedAt: &pub, DiscoveredAt: pub, Language: "fa",
			Categories: []model.CategoryAssignment{{ID: cat, Confidence: 0.9, Origin: model.OriginRule}},
			Provinces:  []model.ProvinceAssignment{{ID: "kabul", Confidence: 0.8, Origin: model.OriginRule}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if breaking {
			if err := st.SetArticleBreaking(ctx, StableID("art", slug), true, 0.9); err != nil {
				t.Fatal(err)
			}
		}
	}
	insert("خبر فوری کابل", "https://test.example/break", "security", true)
	insert("اقتصاد افغانستان", "https://test.example/econ", "economy", false)
	insert("وظیفه جدید در کابل", "https://test.example/job", "jobs", false)

	payload, err := st.HomePayload(ctx, "fa", []string{"kabul"}, []string{"technology"})
	if err != nil {
		t.Fatal(err)
	}
	if len(payload.Breaking) != 1 {
		t.Errorf("breaking section = %d, want 1", len(payload.Breaking))
	}
	if len(payload.LatestAfghanistan) != 3 {
		t.Errorf("latest afghanistan = %d, want 3", len(payload.LatestAfghanistan))
	}
	if len(payload.Economy) != 1 {
		t.Errorf("economy section = %d, want 1", len(payload.Economy))
	}
	if len(payload.Jobs) != 1 {
		t.Errorf("jobs section = %d, want 1", len(payload.Jobs))
	}
	if len(payload.FollowedProvince) != 1 {
		t.Errorf("followed province preview = %d, want 1", len(payload.FollowedProvince))
	}
	if payload.Breaking[0].Province == nil || payload.Breaking[0].Province.ID != "kabul" {
		t.Errorf("hydrated province missing on breaking card")
	}
}

func TestApplySourceDiversity(t *testing.T) {
	cards := []model.ArticleCard{
		{ID: "1", Source: model.SourceRef{ID: "A"}},
		{ID: "2", Source: model.SourceRef{ID: "A"}},
		{ID: "3", Source: model.SourceRef{ID: "A"}},
		{ID: "4", Source: model.SourceRef{ID: "B"}},
	}
	out := ApplySourceDiversity(cards, 2)
	if len(out) != 4 {
		t.Fatalf("diversity must not drop items: %d", len(out))
	}
	if out[2].Source.ID != "B" {
		t.Fatalf("third card should come from another source, got %s", out[2].Source.ID)
	}
	if out[3].Source.ID != "A" {
		t.Fatalf("deferred card was lost, last = %s", out[3].Source.ID)
	}
}

func TestNextPollDelayBackoff(t *testing.T) {
	st := testStore(t)
	base := st.NextPollDelay(model.TierNormal, 0, 5, false)
	if base <= 0 {
		t.Fatal("delay must be positive")
	}
	withFailures := st.NextPollDelay(model.TierNormal, 4, 0, false)
	if withFailures <= base {
		t.Fatalf("backoff did not grow: %s vs %s", withFailures, base)
	}
	// The cap must hold whatever the jitter draw is, not just this one.
	for i := 0; i < 500; i++ {
		capped := st.NextPollDelay(model.TierSlow, 20, 0, true)
		if capped > maxPollDelay {
			t.Fatalf("backoff exceeded the cap: %s > %s", capped, maxPollDelay)
		}
		if capped < minPollDelay {
			t.Fatalf("backoff fell below the floor: %s < %s", capped, minPollDelay)
		}
	}
	// A healthy, quiet feed is allowed to stretch out, but never past the same ceiling.
	for i := 0; i < 200; i++ {
		if d := st.NextPollDelay(model.TierSlow, 0, 0, true); d > maxPollDelay {
			t.Fatalf("adaptive interval exceeded the cap: %s", d)
		}
	}
}

func TestHealthScore(t *testing.T) {
	if HealthScore(0, true, false) != 100 {
		t.Error("healthy feed should score 100")
	}
	if HealthScore(3, true, false) >= 100 {
		t.Error("failures must reduce the score")
	}
	if HealthScore(1, false, true) >= HealthScore(1, true, false) {
		t.Error("parser failure and staleness must both cost points")
	}
}

// Regression: a caller that omits ArticleInput.ID (a seeder, a backfill, an import) must not
// be able to persist a row that no client can open. The public API addresses stories as
// /v1/articles/{id}; a stored empty id surfaces as `"id": ""` in every feed and list payload,
// which is exactly the failure the acceptance suite's "article detail" check caught.
func TestInsertArticleMintsMissingID(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)
	seedSourceAndFeed(t, st)

	res, err := st.InsertArticle(ctx, ArticleInput{
		// ID deliberately omitted — the store is responsible for it.
		SourceID:        "src_test",
		FeedID:          "feed_test",
		ExternalGUID:    "id-less-guid",
		CanonicalURL:    "https://test.example/no-id",
		OriginalURL:     "https://test.example/no-id",
		NormalizedURL:   "https://test.example/no-id",
		Title:           "خبر پرته له پېژندګلو",
		NormalizedTitle: "خبر پرته له پېژندګلو",
		Summary:         "summary",
		DiscoveredAt:    time.Now().UTC(),
		Language:        "ps",
		ContentHash:     "hash-no-id",
	})
	if err != nil {
		t.Fatalf("insert without id: %v", err)
	}
	if res.Outcome != OutcomeInserted {
		t.Fatalf("outcome = %v, want inserted", res.Outcome)
	}
	if res.ArticleID == "" {
		t.Fatal("store returned an empty article id — the story would be unopenable in every client")
	}

	// The minted id must be the one that addresses the stored row.
	article, err := st.ArticleByID(ctx, res.ArticleID)
	if err != nil {
		t.Fatalf("fetch by minted id %q: %v", res.ArticleID, err)
	}
	if article == nil || article.ID != res.ArticleID {
		t.Fatalf("article by minted id = %+v", article)
	}

	// And re-inserting the same story must still dedup rather than mint a second row.
	again, err := st.InsertArticle(ctx, ArticleInput{
		SourceID: "src_test", FeedID: "feed_test", ExternalGUID: "id-less-guid",
		CanonicalURL: "https://test.example/no-id", NormalizedURL: "https://test.example/no-id",
		Title: "خبر پرته له پېژندګلو", NormalizedTitle: "خبر پرته له پېژندګلو",
		DiscoveredAt: time.Now().UTC(), Language: "ps", ContentHash: "hash-no-id",
	})
	if err != nil {
		t.Fatalf("re-insert: %v", err)
	}
	if again.Outcome != OutcomeDuplicate || again.ArticleID != res.ArticleID {
		t.Fatalf("re-insert = %+v, want duplicate of %s", again, res.ArticleID)
	}
}
