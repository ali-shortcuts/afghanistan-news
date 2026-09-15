package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/afnews/backend/internal/model"
)

// articleColumns is the canonical article projection used by every read path.
const articleColumns = `a.id, a.source_id, COALESCE(a.feed_id,''), COALESCE(a.external_guid,''),
	a.canonical_url, a.original_url, a.normalized_url, a.title, a.normalized_title,
	COALESCE(a.summary,''), COALESCE(a.feed_content,''), COALESCE(a.image_url,''), COALESCE(a.author,''),
	a.published_at, a.updated_at, a.discovered_at, COALESCE(a.language,''), a.is_breaking,
	COALESCE(a.cluster_id,''), COALESCE(a.content_hash,''), a.status, a.created_at`

func scanArticle(sc interface{ Scan(...any) error }) (*model.Article, error) {
	var a model.Article
	var published, updated NullTimeOf
	var discovered, created NullTimeOf
	var breaking any
	var status string
	if err := sc.Scan(&a.ID, &a.SourceID, &a.FeedID, &a.ExternalGUID,
		&a.CanonicalURL, &a.OriginalURL, &a.NormalizedURL, &a.Title, &a.NormalizedTitle,
		&a.Summary, &a.FeedContent, &a.ImageURL, &a.Author,
		&published, &updated, &discovered, &a.Language, &breaking,
		&a.ClusterID, &a.ContentHash, &status, &created); err != nil {
		return nil, err
	}
	a.PublishedAt = published.Ptr()
	a.UpdatedAt = updated.Ptr()
	a.DiscoveredAt = discovered.Time
	a.CreatedAt = created.Time
	a.IsBreaking = scanBool(breaking)
	a.Status = model.ArticleStatus(status)
	return &a, nil
}

// CategoryAssignment and ProvinceAssignment alias the domain types so callers can use
// either package.
type CategoryAssignment = model.CategoryAssignment
type ProvinceAssignment = model.ProvinceAssignment

// ArticleInput is the fully normalized article ready for persistence (§119).
type ArticleInput struct {
	ID               string
	SourceID         string
	FeedID           string
	ExternalGUID     string
	CanonicalURL     string
	OriginalURL      string
	NormalizedURL    string
	Title            string
	NormalizedTitle  string
	Summary          string
	FeedContent      string
	ImageURL         string
	Author           string
	PublishedAt      *time.Time
	UpdatedAt        *time.Time
	DiscoveredAt     time.Time
	Language         string
	ContentHash      string
	Categories       []CategoryAssignment
	Provinces        []ProvinceAssignment
	Opportunity      *model.Opportunity
	TitleFingerprint string
}

// InsertOutcome reports what happened to a candidate article.
type InsertOutcome int

const (
	OutcomeInserted InsertOutcome = iota
	OutcomeDuplicate
)

// InsertResult carries the outcome plus the identity of an existing article.
type InsertResult struct {
	Outcome    InsertOutcome
	ArticleID  string
	DedupLevel string
}

// FindDuplicateArticle implements deduplication levels 1-4 (§159).
//
//	level 1: feed + guid
//	level 2: canonical / normalized URL
//	level 3: content hash
//	level 4: normalized title fingerprint inside a publish window
func (s *Store) FindDuplicateArticle(ctx context.Context, feedID, guid, normalizedURL, canonicalURL, contentHash, fingerprint string, since time.Time) (string, string, error) {
	if guid != "" && feedID != "" {
		var id string
		err := s.queryRow(ctx,
			`SELECT id FROM articles WHERE feed_id = $1 AND external_guid = $2 LIMIT 1`, feedID, guid).Scan(&id)
		if err == nil {
			return id, "GUID", nil
		}
		if err != sql.ErrNoRows {
			return "", "", err
		}
	}

	for _, u := range []string{normalizedURL, canonicalURL} {
		if u == "" {
			continue
		}
		var id string
		err := s.queryRow(ctx, `SELECT id FROM articles WHERE normalized_url = $1 LIMIT 1`, u).Scan(&id)
		if err == nil {
			return id, "URL", nil
		}
		if err != sql.ErrNoRows {
			return "", "", err
		}
	}

	if contentHash != "" {
		var id string
		err := s.queryRow(ctx, `SELECT id FROM articles WHERE content_hash = $1 LIMIT 1`, contentHash).Scan(&id)
		if err == nil {
			return id, "CONTENT_HASH", nil
		}
		if err != sql.ErrNoRows {
			return "", "", err
		}
	}

	if fingerprint != "" {
		var id string
		err := s.queryRow(ctx,
			`SELECT id FROM articles WHERE normalized_title = $1 AND discovered_at >= $2 LIMIT 1`,
			fingerprint, s.timeVal(since)).Scan(&id)
		if err == nil {
			return id, "TITLE_WINDOW", nil
		}
		if err != sql.ErrNoRows {
			return "", "", err
		}
	}
	return "", "", nil
}

// InsertArticle persists a normalized article with its classifications (§146).
// It returns OutcomeDuplicate (instead of failing) when another worker inserted the
// same story first, which keeps concurrent ingestion idempotent.
func (s *Store) InsertArticle(ctx context.Context, in ArticleInput) (InsertResult, error) {
	// Never persist an identifier-less article: the public API exposes articles/{id} and
	// an empty id makes the story impossible to open in any client. Callers that do not
	// pass an id (seeds, backfills) get the same stable derivation the ingest pipeline uses.
	if in.ID == "" {
		in.ID = StableID("art", in.CanonicalURL+in.FeedID)
	}
	existing, level, err := s.FindDuplicateArticle(ctx, in.FeedID, in.ExternalGUID, in.NormalizedURL,
		in.CanonicalURL, in.ContentHash, in.TitleFingerprint, in.DiscoveredAt.Add(-48*time.Hour))
	if err != nil {
		return InsertResult{}, err
	}
	if existing != "" {
		return InsertResult{Outcome: OutcomeDuplicate, ArticleID: existing, DedupLevel: level}, nil
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return InsertResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	_, err = s.txExec(ctx, tx,
		`INSERT INTO articles (id, source_id, feed_id, external_guid, canonical_url, original_url, normalized_url,
		   title, normalized_title, summary, feed_content, image_url, author, published_at, updated_at,
		   discovered_at, language, is_breaking, cluster_id, content_hash, status, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$16)`,
		in.ID, in.SourceID, nullString(in.FeedID), nullString(in.ExternalGUID), in.CanonicalURL, in.OriginalURL,
		in.NormalizedURL, in.Title, in.NormalizedTitle, nullString(in.Summary), nullString(in.FeedContent),
		nullString(in.ImageURL), nullString(in.Author), s.DB.TimePtrVal(in.PublishedAt), s.DB.TimePtrVal(in.UpdatedAt),
		s.timeVal(in.DiscoveredAt), in.Language, s.DB.BoolVal(false), nil, nullString(in.ContentHash),
		string(model.ArticleActive))
	if err != nil {
		if isUniqueViolation(err) {
			return InsertResult{Outcome: OutcomeDuplicate, DedupLevel: "RACE"}, nil
		}
		return InsertResult{}, fmt.Errorf("insert article: %w", err)
	}

	for _, c := range in.Categories {
		if _, err := s.txExec(ctx, tx,
			`INSERT INTO article_categories (article_id, category_id, confidence, origin) VALUES ($1,$2,$3,$4)
			 ON CONFLICT (article_id, category_id) DO UPDATE SET confidence = $3, origin = $4`,
			in.ID, c.ID, c.Confidence, string(c.Origin)); err != nil {
			return InsertResult{}, err
		}
	}
	for _, p := range in.Provinces {
		if _, err := s.txExec(ctx, tx,
			`INSERT INTO article_provinces (article_id, province_id, confidence, origin) VALUES ($1,$2,$3,$4)
			 ON CONFLICT (article_id, province_id) DO UPDATE SET confidence = $3, origin = $4`,
			in.ID, p.ID, p.Confidence, string(p.Origin)); err != nil {
			return InsertResult{}, err
		}
	}
	if in.Opportunity != nil {
		o := in.Opportunity
		if _, err := s.txExec(ctx, tx,
			`INSERT INTO article_opportunities (article_id, organization, location, deadline, employment_type, opportunity_type, reference_number)
			 VALUES ($1,$2,$3,$4,$5,$6,$7)
			 ON CONFLICT (article_id) DO UPDATE SET organization = $2, location = $3, deadline = $4,
			   employment_type = $5, opportunity_type = $6, reference_number = $7`,
			in.ID, nullString(o.Organization), nullString(o.Location), s.DB.TimePtrVal(o.Deadline),
			nullString(o.EmploymentType), nullString(o.OpportunityType), nullString(o.ReferenceNumber)); err != nil {
			return InsertResult{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return InsertResult{}, err
	}
	return InsertResult{Outcome: OutcomeInserted, ArticleID: in.ID}, nil
}

// ArticleByID returns the full article record including feed-provided content.
func (s *Store) ArticleByID(ctx context.Context, id string) (*model.Article, error) {
	row := s.queryRow(ctx, `SELECT `+articleColumns+` FROM articles a WHERE a.id = $1`, id)
	return scanArticle(row)
}

// ArticleCardByID returns a hydrated card for an article.
func (s *Store) ArticleCardByID(ctx context.Context, id string, lang string) (*model.ArticleCard, error) {
	art, err := s.ArticleByID(ctx, id)
	if err != nil {
		return nil, err
	}
	cards, err := s.HydrateCards(ctx, []*model.Article{art}, lang)
	if err != nil {
		return nil, err
	}
	if len(cards) == 0 {
		return nil, sql.ErrNoRows
	}
	return &cards[0], nil
}

// HydrateCards attaches sources, categories, provinces, clusters and opportunity
// metadata to article rows using batched lookups (no per-row queries) (§353).
func (s *Store) HydrateCards(ctx context.Context, arts []*model.Article, lang string) ([]model.ArticleCard, error) {
	if len(arts) == 0 {
		return []model.ArticleCard{}, nil
	}
	ids := make([]any, 0, len(arts))
	sourceIDs := map[string]struct{}{}
	clusterIDs := map[string]struct{}{}
	for _, a := range arts {
		ids = append(ids, a.ID)
		sourceIDs[a.SourceID] = struct{}{}
		if a.ClusterID != "" {
			clusterIDs[a.ClusterID] = struct{}{}
		}
	}
	ph := inClause(1, len(ids))

	// sources
	sources := map[string]model.SourceRef{}
	srcIDs := make([]any, 0, len(sourceIDs))
	for id := range sourceIDs {
		srcIDs = append(srcIDs, id)
	}
	if len(srcIDs) > 0 {
		rows, err := s.query(ctx, `SELECT id, name, source_type FROM sources WHERE id IN (`+inClause(1, len(srcIDs))+`)`, srcIDs...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id, name, st string
			if err := rows.Scan(&id, &name, &st); err != nil {
				rows.Close()
				return nil, err
			}
			t := model.SourceType(st)
			sources[id] = model.SourceRef{ID: id, Name: name, Type: t, Label: t.Label()}
		}
		rows.Close()
	}

	// categories (highest confidence wins as primary)
	type catInfo struct {
		ref        model.CategoryRef
		confidence float64
	}
	cats := map[string]catInfo{}
	rows, err := s.query(ctx,
		`SELECT ac.article_id, c.id, COALESCE(c.name_en,''), COALESCE(c.name_fa,''), COALESCE(c.name_ps,''), COALESCE(ac.confidence,0)
		 FROM article_categories ac JOIN categories c ON c.id = ac.category_id
		 WHERE ac.article_id IN (`+ph+`)`, ids...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var articleID, id, en, fa, ps string
		var conf float64
		if err := rows.Scan(&articleID, &id, &en, &fa, &ps, &conf); err != nil {
			rows.Close()
			return nil, err
		}
		prev, ok := cats[articleID]
		if ok && prev.confidence >= conf {
			continue
		}
		cats[articleID] = catInfo{
			ref:        model.CategoryRef{ID: id, DisplayNames: map[string]string{"en": en, "fa": fa, "ps": ps}},
			confidence: conf,
		}
	}
	rows.Close()

	// provinces
	provinces := map[string]model.ProvinceRef{}
	rows, err = s.query(ctx,
		`SELECT ap.article_id, p.id, COALESCE(p.name_en,''), COALESCE(p.name_fa,''), COALESCE(p.name_ps,'')
		 FROM article_provinces ap JOIN provinces p ON p.id = ap.province_id
		 WHERE ap.article_id IN (`+ph+`)`, ids...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var articleID, id, en, fa, ps string
		if err := rows.Scan(&articleID, &id, &en, &fa, &ps); err != nil {
			rows.Close()
			return nil, err
		}
		provinces[articleID] = model.ProvinceRef{ID: id, DisplayNames: map[string]string{"en": en, "fa": fa, "ps": ps}}
	}
	rows.Close()

	// clusters
	clusters := map[string]int{}
	if len(clusterIDs) > 0 {
		clIDs := make([]any, 0, len(clusterIDs))
		for id := range clusterIDs {
			clIDs = append(clIDs, id)
		}
		rows, err = s.query(ctx, `SELECT id, article_count FROM article_clusters WHERE id IN (`+inClause(1, len(clIDs))+`)`, clIDs...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			var n int
			if err := rows.Scan(&id, &n); err != nil {
				rows.Close()
				return nil, err
			}
			clusters[id] = n
		}
		rows.Close()
	}

	// opportunities
	opps := map[string]*model.Opportunity{}
	rows, err = s.query(ctx,
		`SELECT article_id, COALESCE(organization,''), COALESCE(location,''), deadline, COALESCE(employment_type,''),
		        COALESCE(opportunity_type,''), COALESCE(reference_number,'')
		 FROM article_opportunities WHERE article_id IN (`+ph+`)`, ids...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var articleID, org, loc, emp, typ, ref string
		var deadline NullTimeOf
		if err := rows.Scan(&articleID, &org, &loc, &deadline, &emp, &typ, &ref); err != nil {
			rows.Close()
			return nil, err
		}
		o := &model.Opportunity{Organization: org, Location: loc, Deadline: deadline.Ptr(),
			EmploymentType: emp, OpportunityType: typ, ReferenceNumber: ref}
		if o.Deadline != nil && o.Deadline.Before(time.Now().UTC()) {
			o.Expired = true
		}
		opps[articleID] = o
	}
	rows.Close()

	out := make([]model.ArticleCard, 0, len(arts))
	for _, a := range arts {
		card := model.ArticleCard{
			ID:           a.ID,
			Title:        a.Title,
			Summary:      a.Summary,
			ImageURL:     a.ImageURL,
			OriginalURL:  a.OriginalURL,
			PublishedAt:  a.PublishedAt,
			DiscoveredAt: a.DiscoveredAt,
			Language:     a.Language,
			IsBreaking:   a.IsBreaking,
			Source:       sources[a.SourceID],
			SourceType:   sources[a.SourceID].Type,
			Status:       a.Status,
		}
		if ci, ok := cats[a.ID]; ok {
			ref := ci.ref
			card.Category = &ref
		}
		if p, ok := provinces[a.ID]; ok {
			ref := p
			card.Province = &ref
		}
		if a.ClusterID != "" {
			if n, ok := clusters[a.ClusterID]; ok && n >= 2 {
				card.Cluster = &model.ClusterRef{ID: a.ClusterID, CoverageCount: n}
			}
		}
		if o, ok := opps[a.ID]; ok {
			card.Opportunity = o
		}
		out = append(out, card)
	}
	return out, nil
}

// CountArticles is used by the province/category counters.
func (s *Store) CountArticles(ctx context.Context, q model.ArticleQuery) (int, error) {
	where, args := s.articleWhere(q)
	return s.CountRow(ctx, `SELECT COUNT(*) FROM articles a LEFT JOIN feeds f ON f.id = a.feed_id WHERE `+where, args...)
}

// SetArticleBreaking toggles the breaking flag (§139).
func (s *Store) SetArticleBreaking(ctx context.Context, articleID string, breaking bool, score float64) error {
	_, err := s.exec(ctx, `UPDATE articles SET is_breaking = $2, breaking_score = $3 WHERE id = $1`,
		articleID, breaking, score)
	return err
}

// SetArticleStatus hides/restores an article (§138).
func (s *Store) SetArticleStatus(ctx context.Context, articleID string, status model.ArticleStatus) error {
	_, err := s.exec(ctx, `UPDATE articles SET status = $2 WHERE id = $1`, articleID, string(status))
	return err
}

// ReclassifyArticle replaces editor-owned category/province assignments (§138).
func (s *Store) ReclassifyArticle(ctx context.Context, articleID, categoryID, provinceID string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if categoryID != "" {
		if _, err := s.txExec(ctx, tx,
			`DELETE FROM article_categories WHERE article_id = $1 AND origin <> $2`, articleID, string(model.OriginEditor)); err != nil {
			return err
		}
		if _, err := s.txExec(ctx, tx,
			`INSERT INTO article_categories (article_id, category_id, confidence, origin) VALUES ($1,$2,$3,$4)
			 ON CONFLICT (article_id, category_id) DO UPDATE SET confidence = $3, origin = $4`,
			articleID, categoryID, 1.0, string(model.OriginEditor)); err != nil {
			return err
		}
	}
	if provinceID != "" {
		if _, err := s.txExec(ctx, tx,
			`DELETE FROM article_provinces WHERE article_id = $1 AND origin <> $2`, articleID, string(model.OriginEditor)); err != nil {
			return err
		}
		if _, err := s.txExec(ctx, tx,
			`INSERT INTO article_provinces (article_id, province_id, confidence, origin) VALUES ($1,$2,$3,$4)
			 ON CONFLICT (article_id, province_id) DO UPDATE SET confidence = $3, origin = $4`,
			articleID, provinceID, 1.0, string(model.OriginEditor)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ClusterCoverage returns other articles reported to be about the same story (§160, §298).
func (s *Store) ClusterCoverage(ctx context.Context, clusterID string, excludeID string, limit int, lang string) ([]model.ArticleCard, error) {
	rows, err := s.query(ctx,
		`SELECT `+articleColumns+` FROM articles a
		 WHERE a.cluster_id = $1 AND a.id <> $2 AND a.status = 'ACTIVE'
		 ORDER BY a.published_at DESC LIMIT $3`, clusterID, excludeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	arts := []*model.Article{}
	for rows.Next() {
		a, err := scanArticle(rows)
		if err != nil {
			return nil, err
		}
		arts = append(arts, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return s.HydrateCards(ctx, arts, lang)
}

// AssignCluster creates or reuses a story cluster and links the article (§159 level 5).
func (s *Store) AssignCluster(ctx context.Context, articleID, clusterID, topicKey string, publishedAt time.Time) error {
	now := s.timeVal(publishedAt)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var exists int
	if err := s.txQueryRow(ctx, tx, `SELECT COUNT(*) FROM article_clusters WHERE id = $1`, clusterID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		if _, err := s.txExec(ctx, tx,
			`INSERT INTO article_clusters (id, representative_article_id, normalized_topic_key, first_seen_at, last_seen_at, article_count)
			 VALUES ($1,$2,$3,$4,$4,$5)`,
			clusterID, articleID, topicKey, now, 1); err != nil {
			return err
		}
	}
	if _, err := s.txExec(ctx, tx,
		`UPDATE article_clusters SET last_seen_at = $2, article_count = (
		     SELECT COUNT(*) FROM articles WHERE cluster_id = $1 AND status = 'ACTIVE'
		 ) WHERE id = $1`, clusterID, now); err != nil {
		return err
	}
	if _, err := s.txExec(ctx, tx, `UPDATE articles SET cluster_id = $2 WHERE id = $1`, articleID, clusterID); err != nil {
		return err
	}
	return tx.Commit()
}

// FindClusterCandidate looks for an existing cluster covering the same story in a time
// window, using the normalized topic key (§159, §164).
func (s *Store) FindClusterCandidate(ctx context.Context, topicKey string, since time.Time) (string, error) {
	var id string
	err := s.queryRow(ctx,
		`SELECT id FROM article_clusters WHERE normalized_topic_key = $1 AND last_seen_at >= $2
		 ORDER BY last_seen_at DESC LIMIT 1`, topicKey, s.timeVal(since)).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return id, err
}

// DistinctSourceCountForTopic counts independent publishers on a topic inside a window,
// which is one deterministic breaking-news signal (§164).
func (s *Store) DistinctSourceCountForTopic(ctx context.Context, topicKey string, since time.Time) (int, error) {
	return s.CountRow(ctx,
		`SELECT COUNT(DISTINCT a.source_id) FROM articles a
		 JOIN article_clusters c ON c.id = a.cluster_id
		 WHERE c.normalized_topic_key = $1 AND a.discovered_at >= $2 AND a.status = 'ACTIVE'`,
		topicKey, s.timeVal(since))
}

// RecentBreakingCandidates returns high-signal recent articles awaiting evaluation.
func (s *Store) RecentBreakingCandidates(ctx context.Context, since time.Time, limit int) ([]*model.Article, error) {
	rows, err := s.query(ctx,
		`SELECT `+articleColumns+` FROM articles a
		 WHERE a.discovered_at >= $1 AND a.status = 'ACTIVE'
		 ORDER BY a.discovered_at DESC LIMIT $2`, s.timeVal(since), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*model.Article{}
	for rows.Next() {
		a, err := scanArticle(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// FeedArticleStats returns how many articles a feed produced in a window.
func (s *Store) FeedArticleStats(ctx context.Context, feedID string, since time.Time) (int, error) {
	return s.CountRow(ctx, `SELECT COUNT(*) FROM articles WHERE feed_id = $1 AND discovered_at >= $2`,
		feedID, s.timeVal(since))
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate key")
}
