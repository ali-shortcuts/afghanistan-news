package store

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/afnews/backend/internal/model"
)

// Cursor is the stable keyset position for pagination: (published_at, id) (§43, §229).
type Cursor struct {
	PublishedAt time.Time
	ID          string
}

// Encode renders the cursor as an opaque token. Clients must never parse it (§225).
func (c Cursor) Encode() string {
	raw := fmt.Sprintf("%d|%s", c.PublishedAt.UTC().UnixNano(), c.ID)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// DecodeCursor parses an opaque cursor token.
func DecodeCursor(token string) (*Cursor, error) {
	if strings.TrimSpace(token) == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor encoding")
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid cursor payload")
	}
	var nanos int64
	if _, err := fmt.Sscanf(parts[0], "%d", &nanos); err != nil {
		return nil, fmt.Errorf("invalid cursor timestamp")
	}
	return &Cursor{PublishedAt: time.Unix(0, nanos).UTC(), ID: parts[1]}, nil
}

// nonEditorialCategories are topics that never drive a curated digest by themselves: a global
// instrument feed full of magnitude-1.0 readings has a topical list and a source page of its
// own, but "top stories" for Afghan readers must not be a tremor log (§63, §297).
const nonEditorialCategories = "'science','science-space-climate','science-climate-disasters-expanded','reference'"

// digestExclusion drops an article from curated digests when its *primary* (highest-confidence)
// category is non-editorial. Secondary tags are ignored, so a real disaster story that also
// carries a science tag from its feed folder still reaches the front page.
const digestExclusion = `NOT EXISTS (
		SELECT 1 FROM article_categories ac
		WHERE ac.article_id = a.id
		  AND ac.category_id IN (` + nonEditorialCategories + `)
		  AND ac.confidence >= (
		      SELECT COALESCE(MAX(ac2.confidence), 0) FROM article_categories ac2 WHERE ac2.article_id = a.id
		  )
	)`

// articleWhere builds the shared WHERE clause for article queries.
func (s *Store) articleWhere(q model.ArticleQuery) (string, []any) {
	where := []string{"1=1"}
	args := []any{}
	add := func(cond string, vals ...any) {
		for _, v := range vals {
			args = append(args, v)
		}
		placeholders := make([]string, 0, len(vals))
		for i := range vals {
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)-len(vals)+i+1))
		}
		n := len(placeholders)
		cond = strings.Replace(cond, "{IN}", "("+strings.Join(placeholders, ",")+")", 1)
		_ = n
		where = append(where, cond)
	}

	if !q.IncludeHidden {
		args = append(args, string(model.ArticleActive))
		where = append(where, fmt.Sprintf("a.status = $%d", len(args)))
	}
	if q.Scope == "afghanistan" {
		where = append(where, "(f.scope LIKE 'afghanistan%' OR EXISTS (SELECT 1 FROM article_categories ac WHERE ac.article_id = a.id AND ac.category_id IN ('afghanistan','provincial')))")
	} else if q.Scope == "world" {
		where = append(where, "(f.scope = 'global' OR EXISTS (SELECT 1 FROM article_categories ac WHERE ac.article_id = a.id AND ac.category_id IN ('world','regional')))")
	}
	if q.Category != "" {
		add("EXISTS (SELECT 1 FROM article_categories ac WHERE ac.article_id = a.id AND ac.category_id = {IN})", q.Category)
	}
	if q.Province != "" {
		add("EXISTS (SELECT 1 FROM article_provinces ap WHERE ap.article_id = a.id AND ap.province_id = {IN})", q.Province)
	}
	if q.Language != "" {
		add("a.language = {IN}", q.Language)
	}
	if q.Source != "" {
		add("a.source_id = {IN}", q.Source)
	}
	if q.Feed != "" {
		add("a.feed_id = {IN}", q.Feed)
	}
	if q.Cluster != "" {
		add("a.cluster_id = {IN}", q.Cluster)
	}
	if q.Breaking != nil {
		add("a.is_breaking = {IN}", *q.Breaking)
	}
	if q.Digest {
		where = append(where, digestExclusion)
	}
	if q.From != nil {
		args = append(args, s.timeVal(*q.From))
		where = append(where, fmt.Sprintf("COALESCE(a.published_at, a.discovered_at) >= $%d", len(args)))
	}
	if q.To != nil {
		args = append(args, s.timeVal(*q.To))
		where = append(where, fmt.Sprintf("COALESCE(a.published_at, a.discovered_at) <= $%d", len(args)))
	}
	return strings.Join(where, " AND "), args
}

// searchWhere adds the dialect-appropriate full-text predicate.
func (s *Store) searchWhere(q model.ArticleQuery, args []any) (string, []any, string) {
	if strings.TrimSpace(q.Query) == "" {
		return "", args, ""
	}
	term := strings.TrimSpace(q.Query)
	if s.DB.Dialect == "postgres" {
		args = append(args, term)
		cond := fmt.Sprintf(`to_tsvector('simple', a.title || ' ' || COALESCE(a.summary,'')) @@ plainto_tsquery('simple', $%d)`, len(args))
		rank := fmt.Sprintf(`ts_rank(to_tsvector('simple', a.title || ' ' || COALESCE(a.summary,'')), plainto_tsquery('simple', $%d))`, len(args))
		return cond, args, rank
	}
	// SQLite dialect: LIKE based Unicode-safe substring matching (no stemming).
	args = append(args, "%"+strings.ToLower(term)+"%")
	n := len(args)
	cond := fmt.Sprintf(`(LOWER(a.title) LIKE $%d OR LOWER(COALESCE(a.summary,'')) LIKE $%d)`, n, n)
	return cond, args, ""
}

// QueryArticles returns a page of articles for the read API (§230).
func (s *Store) QueryArticles(ctx context.Context, q model.ArticleQuery) ([]*model.Article, string, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 30
	}
	if limit > 100 {
		limit = 100
	}
	where, args := s.articleWhere(q)
	rankExpr := ""
	if cond, newArgs, rank := s.searchWhere(q, args); cond != "" {
		args = newArgs
		where += " AND " + cond
		rankExpr = rank
	}

	cur, err := DecodeCursor(q.Cursor)
	if err != nil {
		return nil, "", err
	}

	order := "COALESCE(a.published_at, a.discovered_at) DESC, a.id DESC"
	if q.Sort == "top" {
		order = s.scoreExpr(&args) + " DESC, COALESCE(a.published_at, a.discovered_at) DESC, a.id DESC"
		if rankExpr != "" {
			order = rankExpr + " DESC, " + order
		}
	}
	if cur != nil && q.Sort != "top" {
		args = append(args, s.timeVal(cur.PublishedAt), cur.ID)
		where += fmt.Sprintf(" AND (COALESCE(a.published_at, a.discovered_at) < $%d OR (COALESCE(a.published_at, a.discovered_at) = $%d AND a.id < $%d))",
			len(args)-1, len(args)-1, len(args))
	}

	args = append(args, limit+1)
	query := fmt.Sprintf(`SELECT %s FROM articles a LEFT JOIN feeds f ON f.id = a.feed_id
		WHERE %s ORDER BY %s LIMIT $%d`, articleColumns, where, order, len(args))
	if q.Offset > 0 && q.Cursor == "" {
		args = append(args, q.Offset)
		query += fmt.Sprintf(` OFFSET $%d`, len(args))
	}

	rows, err := s.query(ctx, query, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	out := []*model.Article{}
	for rows.Next() {
		a, err := scanArticle(rows)
		if err != nil {
			return nil, "", err
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}

	next := ""
	if len(out) > limit {
		last := out[limit-1]
		ts := last.DiscoveredAt
		if last.PublishedAt != nil {
			ts = *last.PublishedAt
		}
		next = Cursor{PublishedAt: ts, ID: last.ID}.Encode()
		out = out[:limit]
	}
	return out, next, nil
}

// scoreExpr is the deterministic ranking formula (§295). Thresholds are bound
// parameters so the expression is dialect-portable; the caller's argument slice is
// updated in place because Go slices cannot be appended to through a value parameter.
func (s *Store) scoreExpr(args *[]any) string {
	now := time.Now().UTC()
	thresholds := map[string]time.Time{
		"t15m": now.Add(-15 * time.Minute),
		"t1h":  now.Add(-time.Hour),
		"t6h":  now.Add(-6 * time.Hour),
		"t24h": now.Add(-24 * time.Hour),
		"t3d":  now.Add(-72 * time.Hour),
	}
	idx := map[string]int{}
	for _, key := range []string{"t15m", "t1h", "t6h", "t24h", "t3d"} {
		*args = append(*args, s.timeVal(thresholds[key]))
		idx[key] = len(*args)
	}
	return fmt.Sprintf(`(
	  CASE
	    WHEN COALESCE(a.published_at, a.discovered_at) >= $%d THEN 100
	    WHEN COALESCE(a.published_at, a.discovered_at) >= $%d THEN 80
	    WHEN COALESCE(a.published_at, a.discovered_at) >= $%d THEN 60
	    WHEN COALESCE(a.published_at, a.discovered_at) >= $%d THEN 40
	    WHEN COALESCE(a.published_at, a.discovered_at) >= $%d THEN 20
	    ELSE 5
	  END
	  + (SELECT COALESCE(s2.trust_weight,3) * 4 FROM sources s2 WHERE s2.id = a.source_id)
	  + CASE WHEN a.is_breaking = %s THEN 25 ELSE 0 END
	  - (CASE WHEN a.cluster_id IS NULL THEN 0 ELSE 6 END)
	)`, idx["t15m"], idx["t1h"], idx["t6h"], idx["t24h"], idx["t3d"], s.boolLiteral(true))
}

func (s *Store) boolLiteral(v bool) string {
	if s.DB.Dialect == "sqlite" {
		if v {
			return "1"
		}
		return "0"
	}
	if v {
		return "TRUE"
	}
	return "FALSE"
}

// ApplySourceDiversity enforces the deterministic "no more than N consecutive cards
// from one source" rule for Top/Home ranking. Latest ordering is never altered (§63, §297).
func ApplySourceDiversity(cards []model.ArticleCard, maxConsecutive int) []model.ArticleCard {
	if maxConsecutive <= 0 || len(cards) < 3 {
		return cards
	}
	out := make([]model.ArticleCard, 0, len(cards))
	pending := append([]model.ArticleCard{}, cards...)
	var lastSource string
	run := 0
	for len(pending) > 0 {
		picked := 0
		for i, c := range pending {
			if c.Source.ID == lastSource && run >= maxConsecutive {
				continue
			}
			picked = i
			break
		}
		c := pending[picked]
		pending = append(pending[:picked], pending[picked+1:]...)
		if c.Source.ID == lastSource {
			run++
		} else {
			run = 1
			lastSource = c.Source.ID
		}
		out = append(out, c)
	}
	return out
}

// HomePayload assembles the aggregated Home response with bounded section sizes (§213).
func (s *Store) HomePayload(ctx context.Context, lang string, followedProvinces []string, topics []string) (*model.HomePayload, error) {
	payload := &model.HomePayload{GeneratedAt: time.Now().UTC()}

	// section renders one home section. maxPerSource caps how many cards a single source may
	// contribute (curated digests use 1); when a cap is in force the query asks for a wider
	// candidate window so diversity can actually be honoured, then trims to the display size.
	section := func(q model.ArticleQuery, limit int, maxPerSource int) ([]model.ArticleCard, error) {
		if limit <= 0 {
			limit = 5
		}
		q.Limit = limit
		if maxPerSource > 0 {
			q.Limit = limit * 6
			if q.Limit > 60 {
				q.Limit = 60
			}
		}
		arts, _, err := s.QueryArticles(ctx, q)
		if err != nil {
			return nil, err
		}
		cards, err := s.HydrateCards(ctx, arts, lang)
		if err != nil {
			return nil, err
		}
		if maxPerSource > 0 {
			cards = ApplySourceDiversity(cards, maxPerSource)
			if len(cards) > limit {
				cards = cards[:limit]
			}
		}
		return cards, nil
	}

	breakingTrue := true
	var err error
	if payload.Breaking, err = section(model.ArticleQuery{Breaking: &breakingTrue, Sort: "latest", Digest: true}, 3, 1); err != nil {
		return nil, err
	}
	if payload.TopStories, err = section(model.ArticleQuery{Sort: "top", Digest: true}, 5, 1); err != nil {
		return nil, err
	}
	if payload.LatestAfghanistan, err = section(model.ArticleQuery{Scope: "afghanistan"}, 8, 2); err != nil {
		return nil, err
	}
	if payload.Economy, err = section(model.ArticleQuery{Category: "economy", Digest: true}, 5, 1); err != nil {
		return nil, err
	}
	if payload.Jobs, err = section(model.ArticleQuery{Category: "jobs", Digest: true}, 5, 1); err != nil {
		return nil, err
	}
	if payload.World, err = section(model.ArticleQuery{Scope: "world", Digest: true}, 5, 1); err != nil {
		return nil, err
	}

	// Followed provinces: one preview card per followed province (§8.4).
	for _, province := range followedProvinces {
		cards, err := section(model.ArticleQuery{Province: province}, 1, 0)
		if err != nil {
			return nil, err
		}
		payload.FollowedProvince = append(payload.FollowedProvince, cards...)
	}

	// Personal topic sections (§8.8, §8.9).
	for _, topic := range topics {
		if topic == "" {
			continue
		}
		cards, err := section(model.ArticleQuery{Category: topic, Digest: true}, 5, 1)
		if err != nil {
			return nil, err
		}
		if len(cards) == 0 {
			continue
		}
		payload.PersonalizedSections = append(payload.PersonalizedSections, model.HomeSection{
			Key: topic, Title: strings.Title(strings.ReplaceAll(topic, "-", " ")), Articles: cards,
		})
	}

	// Jobs must never show expired opportunities when a deadline is known (§54).
	filtered := payload.Jobs[:0]
	for _, c := range payload.Jobs {
		if c.Opportunity != nil && c.Opportunity.Expired {
			continue
		}
		filtered = append(filtered, c)
	}
	payload.Jobs = filtered

	return payload, nil
}

// SourceList returns publishers for the source directory (§234).
func (s *Store) SourceList(ctx context.Context, sourceType, language, category, search string, limit, offset int) ([]model.Source, int, error) {
	where := []string{"1=1"}
	args := []any{}
	add := func(cond string, val any) {
		args = append(args, val)
		where = append(where, strings.ReplaceAll(cond, "?", fmt.Sprintf("$%d", len(args))))
	}
	if sourceType != "" {
		add("s.source_type = ?", sourceType)
	}
	if language != "" {
		add("s.default_language = ?", language)
	}
	if search != "" {
		args = append(args, "%"+strings.ToLower(search)+"%")
		where = append(where, fmt.Sprintf("LOWER(s.name) LIKE $%d", len(args)))
	}
	if category != "" {
		args = append(args, category)
		where = append(where, fmt.Sprintf("EXISTS (SELECT 1 FROM feeds f WHERE f.source_id = s.id AND f.category_key = $%d)", len(args)))
	}
	cond := strings.Join(where, " AND ")
	total, err := s.CountRow(ctx, `SELECT COUNT(*) FROM sources s WHERE `+cond, args...)
	if err != nil {
		return nil, 0, err
	}
	args = append(args, limit, offset)
	rows, err := s.query(ctx, fmt.Sprintf(
		`SELECT s.id, s.name, COALESCE(s.website_url,''), s.source_type, COALESCE(s.default_language,''),
		        s.trust_weight, s.enabled, s.created_at, s.updated_at
		 FROM sources s WHERE %s ORDER BY s.name LIMIT $%d OFFSET $%d`, cond, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []model.Source{}
	for rows.Next() {
		var src model.Source
		var st string
		var enabled any
		var created, updated NullTimeOf
		if err := rows.Scan(&src.ID, &src.Name, &src.WebsiteURL, &st, &src.DefaultLanguage,
			&src.TrustWeight, &enabled, &created, &updated); err != nil {
			return nil, 0, err
		}
		src.SourceType = model.SourceType(st)
		src.Enabled = scanBool(enabled)
		src.CreatedAt = created.Time
		src.UpdatedAt = updated.Time
		out = append(out, src)
	}
	return out, total, rows.Err()
}

// SourceByID loads one source with its feed count.
func (s *Store) SourceByID(ctx context.Context, id string) (*model.Source, error) {
	var src model.Source
	var st string
	var enabled any
	var created, updated NullTimeOf
	err := s.queryRow(ctx,
		`SELECT id, name, COALESCE(website_url,''), source_type, COALESCE(default_language,''),
		        trust_weight, enabled, created_at, updated_at FROM sources WHERE id = $1`, id).
		Scan(&src.ID, &src.Name, &src.WebsiteURL, &st, &src.DefaultLanguage, &src.TrustWeight,
			&enabled, &created, &updated)
	if err != nil {
		return nil, err
	}
	src.SourceType = model.SourceType(st)
	src.Enabled = scanBool(enabled)
	src.CreatedAt = created.Time
	src.UpdatedAt = updated.Time
	return &src, nil
}
